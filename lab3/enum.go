package kvraft

import (
	"fmt"
	"log"
)

//存枚举相关

type WorkType uint8

const (
	PutType WorkType = iota
	AppendType
	GetType
)

func (work WorkType) String() string {
	switch work {
	case PutType:
		return "Put"
	case AppendType:
		return "Append"
	case GetType:
		return "Get"
	}
	panic(fmt.Sprintf("unexpected OperationOp %d", work))
}

type Err uint8

const (
	OK Err = iota
	ErrNoKey
	ErrWrongLeader
	ErrTimeout
)

func (err Err) String() string {
	switch err {
	case OK:
		return "OK"
	case ErrNoKey:
		return "ErrNoKey"
	case ErrWrongLeader:
		return "ErrWrongLeader"
	case ErrTimeout:
		return "ErrTimeout"
	}
	panic(fmt.Sprintf("unexpected Err %d", err))
}

// debug函数
const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}
