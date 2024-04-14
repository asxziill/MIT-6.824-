package raft

import (
	"fmt"
	"sort"
)

//实现raft工作细节的函数

// 服务器角色
type NodeState uint8

const (
	StateFollower NodeState = iota
	StateCandidate
	StateLeader
)

func (state NodeState) String() string {
	switch state {
	case StateFollower:
		return "Follower"
	case StateCandidate:
		return "Candidate"
	case StateLeader:
		return "Leader"
	}
	panic(fmt.Sprintf("unexpected NodeState %d", state))
}

// 改变服务器状态
func (rf *Raft) ChangeState(state NodeState) {
	if rf.state == state {
		return
	}

	//初始化
	//计时器重置
	rf.state = state
	switch state {
	case StateFollower:
		rf.heartbeatTimer.Stop()
		rf.electionTimer.Reset(RandomizedElectionTimeout())
	case StateCandidate:
	case StateLeader:
		//初始化日志
		lastLog := rf.getLastLog()
		//匹配日志重新修改
		for i := 0; i < len(rf.peers); i++ {
			rf.matchIndex[i], rf.nextIndex[i] = 0, lastLog.Index+1
		}

		rf.electionTimer.Stop()
		rf.heartbeatTimer.Reset(StableHeartbeatTimeout())
		rf.BroadcastHeartbeat(true)
	}
}

// 发送心跳 可以携带 日志请求
func (rf *Raft) replicateOneRound(peer int) {
	rf.mu.Lock()
	if rf.state != StateLeader {
		rf.mu.Unlock()
		return
	}

	//日志打包
	prevLogIndex := rf.nextIndex[peer] - 1

	//如果服务器落后 leader状态，则日志没有交集
	//直接发送快照
	if prevLogIndex < rf.getFirstLog().Index {
		request := rf.genInstallSnapshotRequest()
		rf.mu.Unlock()
		response := new(InstallSnapshotResponse)

		if rf.sendInstallSnapshot(peer, request, response) {
			rf.mu.Lock()
			rf.handleInstallSnapshotResponse(peer, request, response)
			rf.mu.Unlock()
		}
	} else {
		request := rf.genAppendEntriesRequest(prevLogIndex)
		rf.mu.Unlock()

		response := new(AppendEntriesResponse)

		//得到请求再加锁，防止死锁
		if rf.sendAppendEntries(peer, request, response) {
			rf.mu.Lock()
			rf.handleAppendEntriesResponse(peer, request, response)
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) handleAppendEntriesResponse(peer int, request *AppendEntriesRequest, response *AppendEntriesResponse) {
	if rf.state == StateLeader && rf.currentTerm == request.Term {
		// 如果 follower 成功地附加了日志条目
		if response.Success {
			// 更新对应 follower 的 matchIndex 为上一个日志条目的索引加上新增条目的数量
			rf.matchIndex[peer] = request.PrevLogIndex + len(request.Entries)
			rf.nextIndex[peer] = rf.matchIndex[peer] + 1
			// 尝试更新 commitIndex
			rf.advanceCommitIndexForLeader()
		} else {
			// 如果响应中的任期大于当前任期，转换为 follower 状态
			if response.Term > rf.currentTerm {
				rf.ChangeState(StateFollower)
				rf.currentTerm, rf.votedFor = response.Term, -1
				rf.persist()
			} else if response.Term == rf.currentTerm {
				// 如果响应中的任期等于当前任期，但追加日志失败，则根据不同情况更新index
				rf.nextIndex[peer] = response.ConflictIndex
				if response.ConflictTerm != -1 {
					// 如果存在冲突的任期，找到该任期的最后一个日志条目的索引
					firstIndex := rf.getFirstLog().Index
					for i := request.PrevLogIndex; i >= firstIndex; i-- {
						if rf.logs[i-firstIndex].Term == response.ConflictTerm {
							// 更新 nextIndex 为冲突任期的下一个位置
							rf.nextIndex[peer] = i + 1
							break
						}
					}
				}
			}
		}
	}

	//DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} after handling AppendEntriesResponse %v for AppendEntriesRequest %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), response, request)
}

// 追随者的提交操作
func (rf *Raft) advanceCommitIndexForFollower(leaderCommit int) {
	// 计算新的 commitIndex，它是 leaderCommit 和本节点最后一条日志的索引中较小的一个
	newCommitIndex := min(leaderCommit, rf.getLastLog().Index)
	if newCommitIndex > rf.commitIndex {
		//DPrintf("{Node %d} advance commitIndex from %d to %d with leaderCommit %d in term %d", rf.me, rf.commitIndex, newCommitIndex, leaderCommit, rf.currentTerm)
		rf.commitIndex = newCommitIndex
		rf.applyCond.Signal()
	}
}

// 更新leader的提交索引
func (rf *Raft) advanceCommitIndexForLeader() {
	n := len(rf.matchIndex)
	srt := make([]int, n)
	copy(srt, rf.matchIndex)

	//一半以上，就算排序后，第一半大的索引大小
	sort.Ints(srt)
	newCommitIndex := srt[n-(n/2+1)]
	if newCommitIndex > rf.commitIndex {
		// 如果大多数的提交 比 leader的提交大 检测位置是否对上leader的日志
		if rf.matchLog(rf.currentTerm, newCommitIndex) {
			//只能提交当前任期的日志
			//DPrintf("{Node %d} advance commitIndex from %d to %d with matchIndex %v in term %d", rf.me, rf.commitIndex, newCommitIndex, rf.matchIndex, rf.currentTerm)
			//匹配上 则更新自己的提交
			rf.commitIndex = newCommitIndex
			rf.applyCond.Signal()
		} else {
			//DPrintf("{Node %d} can not advance commitIndex from %d because the term of newCommitIndex %d is not equal to currentTerm %d", rf.me, rf.commitIndex, newCommitIndex, rf.currentTerm)
		}
	}
}

// 处理快照的响应
func (rf *Raft) handleInstallSnapshotResponse(peer int, request *InstallSnapshotRequest, response *InstallSnapshotResponse) {
	if rf.state == StateLeader && rf.currentTerm == request.Term {
		if response.Term > rf.currentTerm {
			//任期处理
			rf.ChangeState(StateFollower)
			rf.currentTerm, rf.votedFor = response.Term, -1
			rf.persist()
		} else {
			//更新快照信息
			rf.matchIndex[peer], rf.nextIndex[peer] = request.LastIncludedIndex, request.LastIncludedIndex+1
		}
	}
	//DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} after handling InstallSnapshotResponse %v for InstallSnapshotRequest %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), response, request)
}

func (rf *Raft) GetRaftStateSize() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}
