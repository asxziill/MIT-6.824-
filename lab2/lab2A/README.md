定义随机数
```GO
// 线程安全随机数
type lockedRand struct {
	mu   sync.Mutex
	rand *rand.Rand
}

// 获取随机数
func (r *lockedRand) Intn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Intn(n)
}

var globalRand = &lockedRand{
	rand: rand.New(rand.NewSource(time.Now().UnixNano())),
}
```

定义时间常数
```GO
// 定义时间
// 心跳时间应该小于超时时间
const (
	HeartbeatTimeout = 125
	ElectionTimeout  = 1000
)

// 返回时间
func StableHeartbeatTimeout() time.Duration {
	return time.Duration(HeartbeatTimeout) * time.Millisecond
}

func RandomizedElectionTimeout() time.Duration {
	return time.Duration(ElectionTimeout+globalRand.Intn(ElectionTimeout)) * time.Millisecond
}
```

根据论文定义RPC
```GO
// 增加日志 RPC
type AppendEntriesRequest struct {
	Term         int     //领导人的任期号
	LeaderId     int     //领导人的 Id，以便于跟随者重定向请求
	PrevLogIndex int     //新的日志条目紧随之前的索引值
	PrevLogTerm  int     //(上一条日志)prevLogIndex条目的任期号
	Entries      []Entry //准备存储的日志条目（表示心跳时为空；一次性发送多个是为了提高效率）
	LeaderCommit int     //leaderCommit 领导人已经提交的日志的索引值
}

func (request AppendEntriesRequest) String() string {
	return fmt.Sprintf("{Term:%v,LeaderId:%v,PrevLogIndex:%v,PrevLogTerm:%v,LeaderCommit:%v,Entries:%v}", request.Term, request.LeaderId, request.PrevLogIndex, request.PrevLogTerm, request.LeaderCommit, request.Entries)
}

// 返回
type AppendEntriesResponse struct {
	Term    int  //当前的任期号，用于领导人去更新自己
	Success bool //跟随者的日志条目匹配上 prevLogIndex(上一个日志索引) 和 prevLogTerm(上一个日志任期) 的日志时为真
}

func (response AppendEntriesResponse) String() string {
	return fmt.Sprintf("{Term:%v,Success:%v}", response.Term, response.Success)
}

// 请求投票 RPC
type RequestVoteRequest struct {
	Term         int //候选人的任期号
	CandidateId  int //请求选票的候选人的 Id
	LastLogIndex int //候选人的最后日志条目的索引值
	LastLogTerm  int //候选人最后日志条目的任期号
}

func (request RequestVoteRequest) String() string {
	return fmt.Sprintf("{Term:%v,CandidateId:%v,LastLogIndex:%v,LastLogTerm:%v}", request.Term, request.CandidateId, request.LastLogIndex, request.LastLogTerm)
}

// 返回值
type RequestVoteResponse struct {
	Term        int  //当前任期号，以便于候选人去更新自己的任期号
	VoteGranted bool //候选人赢得了此张选票时为真
}

func (response RequestVoteResponse) String() string {
	return fmt.Sprintf("{Term:%v,VoteGranted:%v}", response.Term, response.VoteGranted)
}
```

根据论文定义raft
```GO
// 增加日志 RPC
type AppendEntriesRequest struct {
	Term         int     //领导人的任期号
	LeaderId     int     //领导人的 Id，以便于跟随者重定向请求
	PrevLogIndex int     //新的日志条目紧随之前的索引值
	PrevLogTerm  int     //(上一条日志)prevLogIndex条目的任期号
	Entries      []Entry //准备存储的日志条目（表示心跳时为空；一次性发送多个是为了提高效率）
	LeaderCommit int     //leaderCommit 领导人已经提交的日志的索引值
}

func (request AppendEntriesRequest) String() string {
	return fmt.Sprintf("{Term:%v,LeaderId:%v,PrevLogIndex:%v,PrevLogTerm:%v,LeaderCommit:%v,Entries:%v}", request.Term, request.LeaderId, request.PrevLogIndex, request.PrevLogTerm, request.LeaderCommit, request.Entries)
}

// 返回
type AppendEntriesResponse struct {
	Term    int  //当前的任期号，用于领导人去更新自己
	Success bool //跟随者的日志条目匹配上 prevLogIndex(上一个日志索引) 和 prevLogTerm(上一个日志任期) 的日志时为真
}

func (response AppendEntriesResponse) String() string {
	return fmt.Sprintf("{Term:%v,Success:%v}", response.Term, response.Success)
}

// 请求投票 RPC
type RequestVoteRequest struct {
	Term         int //候选人的任期号
	CandidateId  int //请求选票的候选人的 Id
	LastLogIndex int //候选人的最后日志条目的索引值
	LastLogTerm  int //候选人最后日志条目的任期号
}

func (request RequestVoteRequest) String() string {
	return fmt.Sprintf("{Term:%v,CandidateId:%v,LastLogIndex:%v,LastLogTerm:%v}", request.Term, request.CandidateId, request.LastLogIndex, request.LastLogTerm)
}

// 返回值
type RequestVoteResponse struct {
	Term        int  //当前任期号，以便于候选人去更新自己的任期号
	VoteGranted bool //候选人赢得了此张选票时为真
}

func (response RequestVoteResponse) String() string {
	return fmt.Sprintf("{Term:%v,VoteGranted:%v}", response.Term, response.VoteGranted)
}
```

构造
日志相关的随便点
```GO
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		peers:          peers,
		persister:      persister,
		me:             me,
		dead:           0,
		currentTerm:    0,
		votedFor:       -1,
		logs:           make([]Entry, 1),
		commitIndex:    0,
		lastApplied:    0,
		nextIndex:      make([]int, len(peers)),
		matchIndex:     make([]int, len(peers)),
		heartbeatTimer: time.NewTimer(StableHeartbeatTimeout()),
		electionTimer:  time.NewTimer(RandomizedElectionTimeout()),
	}

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
```

实现心跳
```GO
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
```

广播
```GO
func (rf *Raft) BroadcastHeartbeat(isHeartBeat bool) {
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		if isHeartBeat {
			go rf.replicateOneRound(peer)
		} else {
			//TODO
		}
	}
}
```

选举函数
```GO
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
					if response.VoteGranted {
						grantedVotes += 1
						if grantedVotes > len(rf.peers)/2 {
							rf.ChangeState(StateLeader)
							rf.BroadcastHeartbeat(true)
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
```


