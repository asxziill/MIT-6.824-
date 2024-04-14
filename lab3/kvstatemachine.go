package kvraft

// 定义操作接口
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
