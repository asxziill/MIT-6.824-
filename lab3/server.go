package kvraft

import (
	"6.824/labgob"
	"6.824/labrpc"
	"6.824/raft"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	dead int32 // set by Kill()

	maxRaftState int // 如果日志增长到这个大小，就进行快照
	lastApplied  int // 记录最后应用的日志索引，防止状态机回滚

	stateMachine   KVStateMachine                // KV状态机，用于存储键值对状态
	lastOperations map[int64]OperationContext    // 通过记录每个clientId对应的最后一个commandId和响应来确定日志是否重复
	notifyChans    map[int]chan *CommandResponse // 通知通道，由应用程序goroutine通知客户端goroutine响应
}

// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

// 对接受到的命令进行操作
// 检查类型(修改/读取)，以分类操作
// 检查操作的合法性 : 修改不能被覆盖
// 然后提交到底层的raft,然后等待应用
func (kv *KVServer) Command(request *CommandRequest, response *CommandResponse) {
	//defer DPrintf("{Node %v} processes CommandRequest %v with CommandResponse %v", kv.rf.Me(), request, response)
	//如果是修改操作，要检查是否检查重复
	kv.mu.Lock()
	if request.Work != GetType && kv.isDuplicateRequest(request.ClientId, request.CommandId) {
		lastResponse := kv.lastOperations[request.ClientId].LastResponse
		response.Value, response.Err = lastResponse.Value, lastResponse.Err
		kv.mu.Unlock()
		return
	}
	kv.mu.Unlock()
	// 不持有锁以提高吞吐量
	// 当KVServer持有锁进行快照时，底层的raft仍然可以提交raft日志
	index, _, isLeader := kv.rf.Start(Command{request})
	if !isLeader {
		response.Err = ErrWrongLeader
		return
	}

	kv.mu.Lock()
	ch := kv.getNotifyChan(index)
	kv.mu.Unlock()

	//用管道来记时
	select {
	case result := <-ch: //一旦接受信息，执行下面的语句
		response.Value, response.Err = result.Value, result.Err
	case <-time.After(ExecuteTimeout):
		response.Err = ErrTimeout
	}
	// 释放 notifyChan 以减少内存占用
	// 这里没有必要阻塞客户端请求
	go func() {
		kv.mu.Lock()
		kv.removeOutdatedNotifyChan(index)
		kv.mu.Unlock()
	}()
}

// 应用
// 收到应用命令的回复 或快照请求 （通过apply管道
// 处理错误
// 应用
func (kv *KVServer) applier() {
	for kv.killed() == false {
		select {
		case message := <-kv.applyCh:
			//DPrintf("{Node %v} tries to apply message %v", kv.rf.Me(), message)
			if message.CommandValid {
				kv.mu.Lock()

				//请求命令小于 上一次的请求索引 错误情况
				if message.CommandIndex <= kv.lastApplied {
					//DPrintf("{Node %v} discards outdated message %v because a newer snapshot which lastApplied is %v has been restored", kv.rf.Me(), message, kv.lastApplied)
					kv.mu.Unlock()
					continue
				}
				kv.lastApplied = message.CommandIndex

				var response *CommandResponse
				command := message.Command.(Command)

				// 如果命令是重复的，不应用它，而是直接获取最后的操作响应
				if command.Work != GetType && kv.isDuplicateRequest(command.ClientId, command.CommandId) {
					//DPrintf("{Node %v} doesn't apply duplicated message %v to stateMachine because maxAppliedCommandId is %v for client %v", kv.rf.Me(), message, kv.lastOperations[command.ClientId], command.ClientId)
					response = kv.lastOperations[command.ClientId].LastResponse
				} else {
					// 应用日志条目到状态机，并获取响应
					response = kv.applyLogToStateMachine(command)
					if command.Work != GetType {
						kv.lastOperations[command.ClientId] = OperationContext{command.CommandId, response}
					}
				}

				// 如果节点是领导者且当前任期与消息的任期相同，通知相关的通道命令申请成功
				if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
					ch := kv.getNotifyChan(message.CommandIndex)
					ch <- response
				}

				//应用成功后检查状态机大小来判断快照
				needSnapshot := kv.needSnapshot()
				if needSnapshot {
					kv.takeSnapshot(message.CommandIndex)
				}
				kv.mu.Unlock()
			} else if message.SnapshotValid {
				//如果是安装快照命令
				kv.mu.Lock()

				if kv.rf.CondInstallSnapshot(message.SnapshotTerm, message.SnapshotIndex, message.Snapshot) {
					//安装成功，使用此快照
					kv.restoreSnapshot(message.Snapshot)
					kv.lastApplied = message.SnapshotIndex
				}
				kv.mu.Unlock()
			} else {
				panic(fmt.Sprintf("unexpected Message %v", message))
			}
		}
	}
}

// `servers[]` 包含了一组服务器的端口，这些服务器将通过 Raft 协议合作，
// 形成一个容错的键/值存储服务。
// `me` 是当前服务器在 `servers[]` 中的索引。
// 键/值服务器应该通过底层的 Raft 实现来存储快照，
// 该实现应该调用 `persister.SaveStateAndSnapshot()` 来
// 原子性地保存 Raft 状态和快照。
// 当 Raft 保存的状态超过 `maxraftstate` 字节时，键/值服务器应该进行快照，
// 以允许 Raft 清理其日志。如果 `maxraftstate` 为 -1，
// 则不需要进行快照。
// `StartKVServer()` 必须快速返回，因此它应该启动 goroutines
// 来处理任何长时间运行的工作。
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// 注册一个服务
	labgob.Register(Command{})
	//新建管道(负责raft与状态机的通信
	applyCh := make(chan raft.ApplyMsg)

	kv := &KVServer{
		maxRaftState:   maxraftstate,
		applyCh:        applyCh,
		dead:           0,
		lastApplied:    0,
		rf:             raft.Make(servers, me, persister, applyCh), //底层启动raft
		stateMachine:   NewMemoryKV(),
		lastOperations: make(map[int64]OperationContext),
		notifyChans:    make(map[int]chan *CommandResponse),
	}
	//如果有的话根据快照恢复
	kv.restoreSnapshot(persister.ReadSnapshot())
	// 应用的协程
	go kv.applier()

	//DPrintf("{Node %v} has started", kv.rf.me)
	return kv
}
