# 日志 一致性
1. 如果在不同日志中的两个条目有着相同索引和任期号，则所存储的命令是相同的，这点是由 leader 来保证的；
2. 如果在不同日志中的两个条目有着相同索引和任期号，则它们之间所有条目完全一样，这点是由日志复制的规则来保证的；

即 如果在不同日志中的两个条目有着相同索引和任期号
则 存储的命令是相同的 且之前的条目完全一样


## 调用函数
给领导者 增加日志
然后发送心跳到集群 请求同步日志
```GO
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
```

## 添加日志
直接添加
```GO
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
```

## 广播
不断尝试直到日志添加
```GO
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
```

## 每次日志添加成功后尝试 提交
跟随者只需同步领导者的提交
领导者需要同步所有提交
```GO
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
			//DPrintf("{Node %d} advance commitIndex from %d to %d with matchIndex %v in term %d", rf.me, rf.commitIndex, newCommitIndex, rf.matchIndex, rf.currentTerm)
			//匹配上 则更新自己的提交
			rf.commitIndex = newCommitIndex
			rf.applyCond.Signal()
		} else {
			//DPrintf("{Node %d} can not advance commitIndex from %d because the term of newCommitIndex %d is not equal to currentTerm %d", rf.me, rf.commitIndex, newCommitIndex, rf.currentTerm)
		}
	}
}
```

## 每次成功的提交后 尝试 申请
```GO
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
```

## 日志规则
申请否则日志，处理回来的请求
先看任期
检测日志匹配(希望，日志请求 和 本地日志有交集，且任期相同(任期不同就算相同索引，当被不同任期覆盖的情况))

如果匹配 找到第一个不同的地方的复制即可(也可以直接复制，只是一个小优化)
然后尝试提交

不匹配，则返回原因
+ 任期不同，往上一个日志任期 找
+ 任期没有问题，说明可能日志缺失，直接同步跟随者的日志索引

```GO
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
```
```GO
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
```


