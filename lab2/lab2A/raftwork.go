package raft

//实现raft工作细节的函数

// 发送日志请求
func (rf *Raft) replicateOneRound(peer int) {
	rf.mu.Lock()
	if rf.state != StateLeader {
		rf.mu.Unlock()
		return
	}

	request := rf.genAppendEntriesRequest()
	rf.mu.Unlock()
	response := new(AppendEntriesResponse)
	rf.sendAppendEntries(peer, request, response)
}

func (rf *Raft) genRequestVoteRequest() *RequestVoteRequest {
	return &RequestVoteRequest{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: len(rf.logs) - 1,
		LastLogTerm:  0,
	}
}

func (rf *Raft) genAppendEntriesRequest() *AppendEntriesRequest {
	return &AppendEntriesRequest{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      nil,
		LeaderCommit: rf.commitIndex,
	}
}
