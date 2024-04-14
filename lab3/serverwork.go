package kvraft

import (
	"6.824/labgob"
	"bytes"
)

// 因此，我们只需要确定一个clientId的最新commandId是否满足条件
// 判断客户端的最大申请id是否大于请求id
func (kv *KVServer) isDuplicateRequest(clientId int64, requestId int64) bool {
	operationContext, ok := kv.lastOperations[clientId]
	return ok && requestId <= operationContext.MaxAppliedCommandId
}

// 如果长度大于设定的值则需要快照
func (kv *KVServer) needSnapshot() bool {
	return kv.maxRaftState != -1 && kv.rf.GetRaftStateSize() >= kv.maxRaftState
}

// 对状态机拍摄快照
func (kv *KVServer) takeSnapshot(index int) {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(kv.stateMachine)
	e.Encode(kv.lastOperations)
	kv.rf.Snapshot(index, w.Bytes())
}

// 根据快照恢复
func (kv *KVServer) restoreSnapshot(snapshot []byte) {
	if snapshot == nil || len(snapshot) == 0 {
		return
	}
	r := bytes.NewBuffer(snapshot)
	d := labgob.NewDecoder(r)
	var stateMachine MemoryKV
	var lastOperations map[int64]OperationContext
	if d.Decode(&stateMachine) != nil ||
		d.Decode(&lastOperations) != nil {
		//DPrintf("{Node %v} restores snapshot failed", kv.rf.Me())
	}
	kv.stateMachine, kv.lastOperations = &stateMachine, lastOperations
}

// 根据索引返回管道(实现不存在初始化的代码
func (kv *KVServer) getNotifyChan(index int) chan *CommandResponse {
	if _, ok := kv.notifyChans[index]; !ok {
		kv.notifyChans[index] = make(chan *CommandResponse, 1)
	}
	return kv.notifyChans[index]
}

// 清除索引对应的管道
func (kv *KVServer) removeOutdatedNotifyChan(index int) {
	delete(kv.notifyChans, index)
}

// 将命令应用到状态机中
func (kv *KVServer) applyLogToStateMachine(command Command) *CommandResponse {
	var value string
	var err Err
	switch command.Work {
	case PutType:
		err = kv.stateMachine.Put(command.Key, command.Value)
	case AppendType:
		err = kv.stateMachine.Append(command.Key, command.Value)
	case GetType:
		value, err = kv.stateMachine.Get(command.Key)
	}
	return &CommandResponse{err, value}
}
