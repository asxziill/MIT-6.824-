# 可持久化
currentTerm, voteFor 和 logs 这三个变量一旦发生变化就一定要在被其他协程感知到之前（释放锁之前，发送 rpc 之前）持久化

## 调用可持久化
服务器重启时调用读取持久化函数

# 快照
+ 任期
+ 领导者id
+ 上一个日志索引 (用来匹配)
+ 上一个日志任期
+ 数据

# 返回
+ 任期信息

如果领导者发现追随者日志落后，可以直接发送快照。要求服务器安装快照


服务器接受到安装快照请求，先判断是否合法，如果合法发送到状态机。等待状态机的应用
```GO
// 在处理 InstallSnapshotRequest 之前和回复 InstallSnapshotResponse 之后，
// 打印节点的状态信息。
// InstallSnapshot 用于处理来自其他 Raft 节点的快照安装请求。
func (rf *Raft) InstallSnapshot(request *InstallSnapshotRequest, response *InstallSnapshotResponse) {
	rf.mu.Lock()         // 锁定节点状态
	defer rf.mu.Unlock() // 确保在函数结束时释放锁
	// 在处理快照安装请求之前和回复之后，打印节点的状态信息
	//defer DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} before processing InstallSnapshotRequest %v and reply InstallSnapshotResponse %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), request, response)

	response.Term = rf.currentTerm

	if request.Term < rf.currentTerm {

		return
	}

	if request.Term > rf.currentTerm {
		rf.currentTerm, rf.votedFor = request.Term, -1
		rf.persist()
	}

	rf.ChangeState(StateFollower)
	rf.electionTimer.Reset(RandomizedElectionTimeout())
	//上面和处理服务器信息是一样的

	// 如果请求的快照的最后包含的索引小于或等于提交索引，则认为快照过时，直接返回
	if request.LastIncludedIndex <= rf.commitIndex {
		return
	}

	// 异步地向 applyCh 通道发送一个包含快照数据的 ApplyMsg，
	// 表明收到了一个有效的快照及其元数据
	go func() {
		rf.applyCh <- ApplyMsg{
			SnapshotValid: true,                      // 快照有效
			Snapshot:      request.Data,              // 快照数据
			SnapshotTerm:  request.LastIncludedTerm,  // 快照的最后包含的任期
			SnapshotIndex: request.LastIncludedIndex, // 快照的最后包含的索引
		}
	}()
}
```

状态机安装快照
```GO
// 一个服务器更新快照只要被要求
// 没有最近的信息才这么做
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {
	// Your code here (2D).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//DPrintf("{Node %v} service calls CondInstallSnapshot with lastIncludedTerm %v and lastIncludedIndex %v to check whether snapshot is still valid in term %v", rf.me, lastIncludedTerm, lastIncludedIndex, rf.currentTerm)

	// 如果快照过时了（即最后包含的索引小于或等于提交索引），则拒绝快照
	if lastIncludedIndex <= rf.commitIndex {
		//DPrintf("{Node %v} rejects the snapshot which lastIncludedIndex is %v because commitIndex %v is larger", rf.me, lastIncludedIndex, rf.commitIndex)
		return false
	}

	if lastIncludedIndex > rf.getLastLog().Index {
		//当前服务器落后快照数据
		// 重置日志为只包含一个条目的新数组
		rf.logs = make([]Entry, 1)
	} else {
		// 否则，将快照前的日志舍弃
		rf.logs = shrinkEntriesArray(rf.logs[lastIncludedIndex-rf.getFirstLog().Index:])
		// 清除第一个条目的命令（因为它现在是一个占位符）
		rf.logs[0].Command = nil
	}
	// 更新占位条目的任期和索引
	rf.logs[0].Term, rf.logs[0].Index = lastIncludedTerm, lastIncludedIndex
	// 更新 lastApplied 和 commitIndex 为最后包含的索引
	rf.lastApplied, rf.commitIndex = lastIncludedIndex, lastIncludedIndex

	// 保存状态和快照到持久化存储
	rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)
	// 打印接受快照后的状态信息
	//DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} after accepting the snapshot which lastIncludedTerm is %v, lastIncludedIndex is %v", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), lastIncludedTerm, lastIncludedIndex)
	return true
}
```

服务器自己因为日志太大调用的快照
```GO
// 该服务表示它已经创建了一个快照，包含了所有信息直到索引(包括)。
// 这意味着服务不再需要日志中直到（包括）那个索引的部分。
// Raft现在应该尽可能地裁剪其日志。
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	snapshotIndex := rf.getFirstLog().Index
	//如果索引大于
	if index <= snapshotIndex {
		//DPrintf("{Node %v} rejects replacing log with snapshotIndex %v as current snapshotIndex %v is larger in term %v", rf.me, index, snapshotIndex, rf.currentTerm)
		return
	}
	rf.logs = shrinkEntriesArray(rf.logs[index-snapshotIndex:])
	rf.logs[0].Command = nil
	rf.persister.SaveStateAndSnapshot(rf.encodeState(), snapshot)
	//DPrintf("{Node %v}'s state is {state %v,term %v,commitIndex %v,lastApplied %v,firstLog %v,lastLog %v} after replacing log with snapshotIndex %v as old snapshotIndex %v is smaller", rf.me, rf.state, rf.currentTerm, rf.commitIndex, rf.lastApplied, rf.getFirstLog(), rf.getLastLog(), index, snapshotIndex)

}
```