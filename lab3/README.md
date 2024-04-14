实验3
## 客户端请求RPC
由客户端调用以修改复制状态
### 参数
+ clinetId 客户端的请求
+ sequenceNum 用来消除重复项
+ command 对状态机的请求

### 返回值
+ status 如果状态机应用了就ok
+ response 如果成功，则保存状态机的输出
+ leaderHint 如果有，就是当前leader的地址

### 接收的实现
1. 如果不是leader返回not_leader
2. 提交到日志，然后复制，提交
3. 如果没有 clientId 的记录，或者如果客户端的 序列号(sequenceNum )的响应已经被丢弃，则回复 SESSION_EXPIRED。
4. 如果序列号已经存在，则回应ok
5. 按顺序应用请求
6. 为客户端保存带有序列号的状态机输出，并丢弃该客户端的任何先前响应。
7. 回应ok和状态机的输出

### 领导者的规则
+ 当成为领导者时，在日志中追加一个空操作（no-op）条目。
+ 如果选举超时期满而没有成功向大多数服务器发送一轮心跳，则转变为跟随者。

## 注册客户端RPC
由新客户端调用以开启新会话，用于消除重复请求。
### 没有参数
### 返回值
+ status 如果状态机注册成功返回ok
+ response 如果成功就返回状态机的输出
+ leaderHint 如果有的话就返回当前领导者的地址

### 接收的实现
1. 如果没有领导者则返回NOT_LEADER 否则返回地址
2. 将注册命令追加到日志中，复制并提交它
3. 按日志顺序应用命令，为新客户端分配会话
4. 回复OK并带有唯一的客户端标识符（可以使用此注册命令的日志索引）

## 客户端请求RPC
由客户端调用，用于查询复制的状态（只读命令）
### 参数
+ query 对状态机的只读请求 

### 返回值

### 接收的实现
1. 如果没有领导者则返回NOT_LEADER 否则返回地址
2. 等待直到最后一个提交的日志是来自于这个领导者的任期
3. 保存 commitIndex 为本地变量 readIndex（下文会用到）
4. 发送新一轮的心跳信号，并等待大多数服务器的回复
5. 等待状态机至少前进到 readIndex 日志条目
6. 处理查询
7. 用状态机的输出回复确认（OK）


# 代码实现
代码实现上没有按照上面的定义严格实现
### 客户端Client
主要负责生成和发送命令
```GO
func (ck *Clerk) Command(request *CommandRequest) string {
	//设置该命令的标识符
	request.ClientId, request.CommandId = ck.clientId, ck.commandId
	for {
		var response CommandResponse
		//超时/非leader就重试
		if !ck.servers[ck.leaderId].Call("KVServer.Command", request, &response) || response.Err == ErrWrongLeader || response.Err == ErrTimeout {
			ck.leaderId = (ck.leaderId + 1) % int64(len(ck.servers))
			continue
		}
		ck.commandId++
		return response.Value
	}
}
```

### 服务端
对请求的命令：
先对底层的raft添加日志
当日志提交则应用到状态机


处理请求函数：主要负责判断请求合法，应用请求是否超时
总的来说是封装了添加请求的操作
```GO
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
```

命令应用函数
主要负责将提交的命令应用到kv存储上
其次就是处理快照命令
主要是负责和底层的raft通信通过apply的管道
```GO
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
```

### kv存储
这里使用简单的map,但是和raft相结合
封装了基础的操作
```GO
type KVStateMachine interface {
	Get(key string) (string, Err)
	Put(key, value string) Err
	Append(key, value string) Err
}

// 定义存储的数据结构(接口的实现
type MemoryKV struct {
	KV map[string]string
}

// 初始化
func NewMemoryKV() *MemoryKV {
	return &MemoryKV{make(map[string]string)}
}

// 设置接口操作底层的map
// 根据key返回value
func (memoryKV *MemoryKV) Get(key string) (string, Err) {
	if value, ok := memoryKV.KV[key]; ok {
		return value, OK
	}
	return "", ErrNoKey
}

// 将key设置成value
func (memoryKV *MemoryKV) Put(key, value string) Err {
	memoryKV.KV[key] = value
	return OK
}

// 将key 的数据加上value
func (memoryKV *MemoryKV) Append(key, value string) Err {
	memoryKV.KV[key] += value
	return OK
}
```

