package kvraft

import (
	"fmt"
	"time"
)

//RPC自己定义

const ExecuteTimeout = 500 * time.Millisecond

type OperationContext struct {
	MaxAppliedCommandId int64
	LastResponse        *CommandResponse
}

type Command struct {
	*CommandRequest
}

// 所有请求视作command
type CommandRequest struct {
	Key       string   //键
	Value     string   //值
	Work      WorkType //类型
	ClientId  int64    //服务器id
	CommandId int64    //请求id
}

func (request CommandRequest) String() string {
	return fmt.Sprintf("{Key:%v,Value:%v,Op:%v,ClientId:%v,CommandId:%v}", request.Key, request.Value, request.Work, request.ClientId, request.CommandId)
}

type CommandResponse struct {
	Err   Err
	Value string
}

func (response CommandResponse) String() string {
	return fmt.Sprintf("{Err:%v,Value:%v}", response.Err, response.Value)
}
