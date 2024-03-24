package raft

import "fmt"

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

	//处理冲突新加的信息
	ConflictIndex int
	ConflictTerm  int
}

func (response AppendEntriesResponse) String() string {
	return fmt.Sprintf("{Term:%v,Success:%v,ConflictIndex:%v,ConflictTerm:%v}", response.Term, response.Success, response.ConflictIndex, response.ConflictTerm)
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

// 生成请求，可以看成构造器
func (rf *Raft) genRequestVoteRequest() *RequestVoteRequest {
	//设置一下当前leader的信息
	lastLog := rf.getLastLog()
	return &RequestVoteRequest{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLog.Index,
		LastLogTerm:  lastLog.Term,
	}
}

// 创建prevLogIndex后面开始的日志 请求
func (rf *Raft) genAppendEntriesRequest(prevLogIndex int) *AppendEntriesRequest {
	firstIndex := rf.getFirstLog().Index
	entries := make([]Entry, len(rf.logs[prevLogIndex+1-firstIndex:]))
	copy(entries, rf.logs[prevLogIndex+1-firstIndex:])
	return &AppendEntriesRequest{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  rf.logs[prevLogIndex-firstIndex].Term,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
}

type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandTerm  int
	CommandIndex int

	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

func (msg ApplyMsg) String() string {
	if msg.CommandValid {
		return fmt.Sprintf("{Command:%v,CommandTerm:%v,CommandIndex:%v}", msg.Command, msg.CommandTerm, msg.CommandIndex)
	} else if msg.SnapshotValid {
		return fmt.Sprintf("{Snapshot:%v,SnapshotTerm:%v,SnapshotIndex:%v}", msg.Snapshot, msg.SnapshotTerm, msg.SnapshotIndex)
	} else {
		panic(fmt.Sprintf("unexpected ApplyMsg{CommandValid:%v,CommandTerm:%v,CommandIndex:%v,SnapshotValid:%v,SnapshotTerm:%v,SnapshotIndex:%v}", msg.CommandValid, msg.CommandTerm, msg.CommandIndex, msg.SnapshotValid, msg.SnapshotTerm, msg.SnapshotIndex))
	}
}
