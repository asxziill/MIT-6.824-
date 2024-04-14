package kvraft

import "6.824/labrpc"
import "crypto/rand"
import "math/big"

type Clerk struct {
	servers []*labrpc.ClientEnd // 存储与Raft节点通信的客户端端点的切片
	// You will have to modify this struct.
	leaderId  int64 // 当前已知的Raft集群领导者的ID
	clientId  int64 // 由nrand()生成的唯一客户端ID
	commandId int64 // (clientId, commandId) 唯一定义了一个操作
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

// 初始化
func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	return &Clerk{
		servers:   servers,
		leaderId:  0,
		clientId:  nrand(),
		commandId: 0,
	}
}

//获取一个键的当前值
//如果键不存在，则返回""
//对于所有其他错误，持续尝试.

// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
func (ck *Clerk) Get(key string) string {
	return ck.Command(&CommandRequest{Key: key, Work: GetType})
}

// putappend为什么不直接实现呢？
func (ck *Clerk) Put(key string, value string) {
	ck.Command(&CommandRequest{Key: key, Value: value, Work: PutType})
}
func (ck *Clerk) Append(key string, value string) {
	ck.Command(&CommandRequest{Key: key, Value: value, Work: AppendType})
}

// 封装RPC的调用
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
