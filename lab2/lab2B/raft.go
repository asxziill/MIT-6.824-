package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labrpc"
)

//当每个Raft节点意识到连续的日志条目被提交后
// 通过传递给Make()的applyCh，向同一服务器上的服务（或测试器）发送一个ApplyMsg。
// CommandValid为true，表明ApplyMsg包含一个新提交的日志条目。
//
// 在第2D部分，你会希望通过applyCh发送其他类型的消息（例如，快照），
// 但对于这些其他用途，将CommandValid设置为false。

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // 锁
	peers     []*labrpc.ClientEnd // 每个服务器的位置
	persister *Persister          // 用于保持该节点持久化状态的对象
	me        int                 //自己的下标
	dead      int32               // s自己是否崩溃

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	//服务器上的持久状态
	currentTerm int
	votedFor    int
	logs        []Entry

	//服务器上的容易失效状态
	commitIndex int
	lastApplied int

	//领导者上的容易失效状态
	nextIndex  []int
	matchIndex []int

	//时间
	electionTimer  *time.Timer //选举时间
	heartbeatTimer *time.Timer //心跳时间

	//节点角色
	state          NodeState
	applyCh        chan ApplyMsg
	applyCond      *sync.Cond
	replicatorCond []*sync.Cond
}

// 返回任期和是否为领导者
func (rf *Raft) GetState() (int, bool) {
	//加锁
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == StateLeader
}

// 将Raft的持久状态保存到稳定存储中，
// 在发生崩溃和重启后可以从中恢复这些状态。
// 有关应持久化哪些内容的描述，请参见论文的图2。
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// 一个服务器更新快照只要被要求
// 没有最近的信息才这么做
// have more recent info since it communicate the snapshot on applyCh.
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// 该服务表示它已经创建了一个快照，包含了所有信息直到并包括索引。
// 这意味着服务不再需要日志中直到（包括）那个索引的部分。
// Raft现在应该尽可能地裁剪其日志。
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

// example RequestVote RPC handler.
// 实现收到 请求投票 时的规则
func (rf *Raft) RequestVote(request *RequestVoteRequest, response *RequestVoteResponse) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//小于任期 或者 已经投票
	if request.Term < rf.currentTerm || (request.Term == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != request.CandidateId) {
		response.Term, response.VoteGranted = rf.currentTerm, false
		return
	}

	if request.Term > rf.currentTerm {
		rf.ChangeState(StateFollower)
		rf.currentTerm, rf.votedFor = request.Term, -1
	}

	//根据日志判断候选人的规则
	//只要最新日志才有资格成为候选人
	if !rf.isLogUpToDate(request.LastLogTerm, request.LastLogIndex) {
		response.Term, response.VoteGranted = rf.currentTerm, false
		return
	}

	rf.votedFor = request.CandidateId
	//重新设置选举时间
	rf.electionTimer.Reset(RandomizedElectionTimeout())
	response.Term, response.VoteGranted = rf.currentTerm, true
}

// 增加日志
func (rf *Raft) AppendEntries(request *AppendEntriesRequest, response *AppendEntriesResponse) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	//defer DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} before processing AppendEntriesRequest %v and reply AppendEntriesResponse %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), request, response)

	//任期检测
	if request.Term < rf.currentTerm {
		response.Term, response.Success = rf.currentTerm, false
		return
	}

	if request.Term > rf.currentTerm {
		rf.currentTerm, rf.votedFor = request.Term, -1
	}

	rf.ChangeState(StateFollower)
	rf.electionTimer.Reset(RandomizedElectionTimeout())

	//缺失了必要的日志条目,无法匹配
	if request.PrevLogIndex < rf.getFirstLog().Index {
		response.Term, response.Success = rf.currentTerm, false
		//DPrintf("{Node %v} receives unexpected AppendEntriesRequest %v from {Node %v} because prevLogIndex %v < firstLogIndex %v", rf.me, request, request.LeaderId, request.PrevLogIndex, rf.getFirstLog().Index)
		return
	}

	if !rf.matchLog(request.PrevLogTerm, request.PrevLogIndex) {
		//没有匹配上
		//设置失效信息
		response.Term, response.Success = rf.currentTerm, false

		lastIndex := rf.getLastLog().Index
		if lastIndex < request.PrevLogIndex {
			//缺日志 没有交集

			//没有任何冲突任期
			response.ConflictTerm = -1
			//领导者应该从 追随者日志的 末尾开始尝试附加日志条目
			response.ConflictIndex = lastIndex + 1
		} else {
			//否则就是任期出问题
			firstIndex := rf.getFirstLog().Index
			//记录冲突的任期
			response.ConflictTerm = rf.logs[request.PrevLogIndex-firstIndex].Term
			//继续跳到上一个任期
			index := request.PrevLogIndex - 1
			for index >= firstIndex && rf.logs[index-firstIndex].Term == response.ConflictTerm {
				index--
			}
			response.ConflictIndex = index
		}
		return
	}

	//匹配上了
	//这里也可以直接复制
	//直接找索引不同的复制，减少复制的量
	firstIndex := rf.getFirstLog().Index
	for index, entry := range request.Entries {
		//检测一致，到不一致或缺失直接全部加上
		if entry.Index-firstIndex >= len(rf.logs) || rf.logs[entry.Index-firstIndex].Term != entry.Term {
			rf.logs = shrinkEntriesArray(append(rf.logs[:entry.Index-firstIndex], request.Entries[index:]...))
			break
		}
	}

	//添加日志后检测更新
	rf.advanceCommitIndexForFollower(request.LeaderCommit)

	response.Term, response.Success = rf.currentTerm, true
}

// example code to send a RequestVote RPC to a server.
// server是目标服务器在rf.peers[]中的索引。
// 预期RPC的参数在args中，用RPC的回复填充*reply

// labrpc包模拟了一个有丢失的网络，在这个网络中，服务器可能无法到达，请求和回复也可能会丢失。

// all()发送一个请求并等待回复。如果在超时时间内收到回复true，否则false
// 因此Call()可能需要一段时间才能返回。
// false返回可能是由于服务器宕机，活着的服务器无法连接，请求丢失或回复丢失造成的。
func (rf *Raft) sendRequestVote(server int, request *RequestVoteRequest, response *RequestVoteResponse) bool {
	return rf.peers[server].Call("Raft.RequestVote", request, response)
}

func (rf *Raft) sendAppendEntries(server int, request *AppendEntriesRequest, response *AppendEntriesResponse) bool {
	return rf.peers[server].Call("Raft.AppendEntries", request, response)
}

// 用kill模拟服务器寄
//
// 检验用 killed来检测
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// 调用start来启动raft服务
// 开始发送命令
// 如果这个服务器不是领导者，返回 false
//
// 第一个返回值是如果命令被提交，它将出现的索引位置。
// 第二个返回值是当前的任期。
// 第三个返回值是如果这个服务器认为它是领导者，则为 true。
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != StateLeader {
		return -1, -1, false
	}

	newLog := rf.appendNewEntry(command)
	//DPrintf("{Node %v} receives a new command[%v] to replicate in term %v", rf.me, newLog, rf.currentTerm)

	//发送增加日志通知
	//这里也可以将日志放到下一个心跳去发送
	rf.BroadcastHeartbeat(false)
	return newLog.Index, newLog.Term, true
}

// 对指定追随者（peer）进行日志复制的协程
func (rf *Raft) replicator(peer int) {
	rf.replicatorCond[peer].L.Lock()
	defer rf.replicatorCond[peer].L.Unlock()

	for rf.killed() == false {
		//等待条件变量的信号,直到该 服务器需要日志复制
		for !rf.needReplicating(peer) {
			rf.replicatorCond[peer].Wait()
		}
		//尝试复制日志
		rf.replicateOneRound(peer)
	}
}

// 一个专门的应用器协程，保证每个日志条目将被精确地推送一次到applyCh中，
// 确保服务层应用日志条目和Raft层提交日志条目可以并行。
func (rf *Raft) applier() {
	for !rf.killed() {
		rf.mu.Lock()

		// 等待接受请求
		for rf.lastApplied >= rf.commitIndex {
			rf.applyCond.Wait()
		}

		//该请求被提交
		firstIndex, commitIndex, lastApplied := rf.getFirstLog().Index, rf.commitIndex, rf.lastApplied
		entries := make([]Entry, commitIndex-lastApplied)
		copy(entries, rf.logs[lastApplied+1-firstIndex:commitIndex+1-firstIndex])

		//将 请求和提交的 差距 放入请求
		rf.mu.Unlock()
		for _, entry := range entries {
			rf.applyCh <- ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandTerm:  entry.Term,
				CommandIndex: entry.Index,
			}
		}
		rf.mu.Lock()

		//DPrintf("{Node %v} applies entries %v-%v in term %v", rf.me, rf.lastApplied, commitIndex, rf.currentTerm)
		// 使用commitIndex而非rf.commitIndex是因为在Unlock()和Lock()期间rf.commitIndex可能会改变

		rf.lastApplied = max(rf.lastApplied, commitIndex)

		rf.mu.Unlock()
	}
}

// 心跳，没心跳就要开始新的选举
func (rf *Raft) ticker() {
	for rf.killed() == false {
		select {
		case <-rf.electionTimer.C:
			rf.mu.Lock()

			//如果超过选举时间没有收到心跳就要开始选举（跟随者2
			//实现候选人规则
			rf.StartElection()
			rf.electionTimer.Reset(RandomizedElectionTimeout())

			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()

			//领导人不断发送心跳
			if rf.state == StateLeader {
				rf.BroadcastHeartbeat(true)
				rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
			}

			rf.mu.Unlock()
		}
	}
}

// 广播心跳
func (rf *Raft) BroadcastHeartbeat(isHeartBeat bool) {
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		if isHeartBeat {
			go rf.replicateOneRound(peer)
		} else {
			//通知 开始尝试复制日志
			rf.replicatorCond[peer].Signal()
		}
	}
}

func (rf *Raft) StartElection() {
	rf.ChangeState(StateCandidate)
	rf.currentTerm += 1
	grantedVotes := 1
	rf.votedFor = rf.me

	request := rf.genRequestVoteRequest()
	//DPrintf("{Node %v} starts election with RequestVoteRequest %v", rf.me, request)

	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		//局部函数，主要用grantedVotes
		//也可在rf上加，来分离操作，不过会破坏raft定义就没加上去
		go func(peer int) {
			response := new(RequestVoteResponse)
			if rf.sendRequestVote(peer, request, response) {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				//DPrintf("{Node %v} receives RequestVoteResponse %v from {Node %v} after sending RequestVoteRequest %v in term %v", rf.me, response, peer, request, rf.currentTerm)

				if rf.currentTerm == request.Term && rf.state == StateCandidate {
					//处理 收到的投票
					if response.VoteGranted {
						grantedVotes += 1
						if grantedVotes > len(rf.peers)/2 {
							rf.ChangeState(StateLeader)
						}
					} else if response.Term > rf.currentTerm {
						rf.ChangeState(StateFollower)
						rf.currentTerm, rf.votedFor = response.Term, -1
					}
				}
			}
		}(peer)
	}
}

// 服务或测试者想要创建一个Raft服务器。
// 所有Raft服务器（包括这个服务器）的端口,都在peers[]数组中。
// 这个服务器的端口是peers[me]。所有服务器是peers[]数组
// 顺序是相同的。persister是这个服务器保存其持久状态的地方，
// 初始包含了最近的保存状态（如果有的话）。
// applyCh是一个通道，测试者或服务期望Raft通过它，发送ApplyMsg消息。
// Make()必须快速返回，所以它应该启动goroutines来执行任何长时间运行的工作。
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		peers:          peers,
		persister:      persister,
		me:             me,
		currentTerm:    0,
		votedFor:       -1,
		logs:           make([]Entry, 1),
		nextIndex:      make([]int, len(peers)),
		matchIndex:     make([]int, len(peers)),
		heartbeatTimer: time.NewTimer(StableHeartbeatTimeout()),
		electionTimer:  time.NewTimer(RandomizedElectionTimeout()),
		state:          StateFollower,
		applyCh:        applyCh,
		replicatorCond: make([]*sync.Cond, len(peers)),
	}
	rf.applyCond = sync.NewCond(&rf.mu)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	lastLog := rf.getLastLog()
	//初始化切片
	for i := 0; i < len(peers); i++ {
		rf.matchIndex[i], rf.nextIndex[i] = 0, lastLog.Index+1
		if i != rf.me {
			rf.replicatorCond[i] = sync.NewCond(&sync.Mutex{})
			// start replicator goroutine to replicate entries in batch
			go rf.replicator(i)
		}
	}

	// start ticker goroutine to start elections
	go rf.ticker()
	go rf.applier()

	return rf
}
