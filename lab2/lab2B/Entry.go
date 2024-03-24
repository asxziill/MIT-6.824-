package raft

import "fmt"

//存日志操作

// 日志
type Entry struct {
	Index   int
	Term    int
	Command interface{}
}

func (entry Entry) String() string {
	return fmt.Sprintf("{Index:%v,Term:%v}", entry.Index, entry.Term)
}

// 得到当前服务器上一个日志
func (rf *Raft) getLastLog() Entry {
	return rf.logs[len(rf.logs)-1]
}

func (rf *Raft) getFirstLog() Entry {
	return rf.logs[0]
}

//以下默认参数是请求的上一个日志的信息

// 返回是否匹配日志
// 首先查看索引是否小于
func (rf *Raft) matchLog(term, index int) bool {
	return index <= rf.getLastLog().Index &&
		index-rf.getFirstLog().Index >= 0 &&
		rf.logs[index-rf.getFirstLog().Index].Term == term
}

// 判断日志是否是新的
// 任期大，或者同任期索引大
func (rf *Raft) isLogUpToDate(term, index int) bool {
	lastLog := rf.getLastLog()
	return term > lastLog.Term || (term == lastLog.Term && index >= lastLog.Index)
}

// shrinkEntriesArray 函数可以缩减 entries 切片的底层数组，
// 如果我们使用的空间不到它的一半。这个数字是相当任意的，
// 选取它是为了尝试平衡内存使用和分配次数。
// 通过一些专注的调整，这个数字可能会得到改进。
func shrinkEntriesArray(entries []Entry) []Entry {
	const lenMultiple = 2
	if len(entries)*lenMultiple < cap(entries) {
		newEntries := make([]Entry, len(entries))
		copy(newEntries, entries)
		return newEntries
	}
	return entries
}

// 增加日志
func (rf *Raft) appendNewEntry(command interface{}) Entry {
	lastLog := rf.getLastLog()
	//创建新日志
	newLog := Entry{lastLog.Index + 1, rf.currentTerm, command}
	//更新自己的信息
	rf.logs = append(rf.logs, newLog)
	rf.matchIndex[rf.me], rf.nextIndex[rf.me] = newLog.Index, newLog.Index+1
	return newLog
}

// 判断是否需要向特定节点复制日志。
func (rf *Raft) needReplicating(peer int) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 判断当前节点是否是领导者，并检查目标节点的matchIndex是否小于最新日志的索引。
	// 如果matchIndex小于最新日志索引，意味着该节点的日志落后于领导者，需要复制。
	return rf.state == StateLeader && rf.matchIndex[peer] < rf.getLastLog().Index
}

// 检测当前任期是否有任何日志条目。
func (rf *Raft) HasLogInCurrentTerm() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查最新的日志条目的任期是否与当前任期相同。
	return rf.getLastLog().Term == rf.currentTerm
}
