[可视化raft](http://thesecretlivesofdata.com/raft/)
首先根据论文完成节点的定义
[中文参考](https://blog.csdn.net/erlib/article/details/53671783?ops_request_misc=%257B%2522request%255Fid%2522%253A%2522165154838116782350980907%2522%252C%2522scm%2522%253A%252220140713.130102334..%2522%257D&request_id=165154838116782350980907&biz_id=0&utm_medium=distribute.pc_search_result.none-task-blog-2~all~sobaiduend~default-1-53671783.142%5Ev9%5Econtrol,157%5Ev4%5Econtrol&utm_term=raft%E8%AE%BA%E6%96%87%E7%BF%BB%E8%AF%91&spm=1018.2226.3001.4187)
以下先定义结构体

## 每个服务器节点
### 服务器上的持久状态
(在响应RPC之前在稳定存储上进行更新"。)
+ currentTerm 服务器最后一次知道的任期号（初始化为 0，持续递增）
+ votedFor 	在当前获得选票的候选人的 Id
+ log[] 日志条目集；每一个条目包含一个用户状态机执行的指令，和收到时的任期号

### 服务器上的容易失效状态
+ commitIndex 已知的最大的已经被提交的日志条目的索引值
+ 最后被应用到状态机的日志条目索引值（初始化为 0，持续递增）

### 领导者上的容易失效状态
(选举后重新初始化)
nextIndex[] 对于每一个服务器，需要发送给他的下一个日志条目的索引值（初始化为领导人最后索引值加一
matchIndex[] 对于每一个服务器，已经复制给他的日志的最高索引值

## 增加日志 RPC
(由领导人负责调用来复制日志指令；也会用作heartbeat)
### 参数
+ term 领导人的任期号
+ leaderId 领导人的 Id，以便于跟随者重定向请求
+ prevLogIndex 新的日志条目紧随之前的索引值
+ prevLogTerm (上一条日志)prevLogIndex条目的任期号
+ entries[] 准备存储的日志条目（表示心跳时为空；一次性发送多个是为了提高效率）
+ leaderCommit 领导人已经提交的日志的索引值

### 返回值
+ term 当前的任期号，用于领导人去更新自己
+ success 跟随者的日志条目匹配上 prevLogIndex(上一个日志索引) 和 prevLogTerm(上一个日志任期) 的日志时为真

### 接收实现
1. 如果 term < currentTerm  (RPC任期小于服务器当前任期) 就返回 false
2. 如果服务器日志在 prevLogIndex 位置处的日志条目的任期号和 prevLogTerm 不匹配，则返回 false
3. 如果已经已经存在的日志条目和新的产生冲突（相同偏移量但是任期号不同），删除这一条和之后所有的 
4. 附加任何在已有的日志中不存在的条目
5. 如果 leaderCommit > commitIndex，令 commitIndex 等于 leaderCommit 和 新日志条目索引值中较小的一个

## 请求投票 RPC
(由候选人负责调用用来征集选票)
### 参数
+ term 候选人的任期号
+ candidateId 请求选票的候选人的 Id
+ lastLogIndex 候选人的最后日志条目的索引值
+ lastLogTerm 候选人最后日志条目的任期号
  
### 返回值
+ term 当前任期号，以便于候选人去更新自己的任期号
+ voteGranted 候选人赢得了此张选票时为真

### 接收实现
1. 如果term < currentTerm返回 false
2. 如果 votedFor 为空或者就是 candidateId，并且候选人的日志也自己一样新，那么就投票给他

## 所有服务器需遵守的规则
### 所有服务器
1. 如果commitIndex > lastApplied，那么就 lastApplied 加一，并把log[lastApplied]应用到状态机中
2. 如果接收到的 RPC 请求中，任期号T > currentTerm，那么就令 currentTerm 等于 T，并切换状态为跟随者

### 跟随者
1. 响应来自候选人和领导者的请求
2. 如果在超过选举超时时间的情况之前都没有收到领导人的心跳，或者是候选人请求投票的，就自己变成候选人

### 候选人
1. 在转变成候选人后就立即开始选举过程
   + 自增当前的任期号（currentTerm）
   + 给自己投票
   + 重置选举超时计时器
   + 发送请求投票的 RPC 给其他所有服务器
2. 如果接收到大多数服务器的选票，那么就变成领导人
3. 如果接收到来自新的领导人的附加日志(心跳) RPC，转变成跟随者
4. 如果选举过程超时，再次发起一轮选举

### 领导人
1.  一旦成为领导人：发送空的附加日志 RPC（心跳）给其他所有的服务器；在一定的空余时间之后不停的重复发送，以阻止跟随者超时
2. 如果接收到来自客户端的请求：附加条目到本地日志中，在条目被应用到状态机后响应客户端
3. 如果对于一个跟随者，最后日志条目的索引值大于等于 nextIndex，那么：发送从 nextIndex 开始的所有日志条目
  + 如果成功：更新相应跟随者的 nextIndex 和 matchIndex
  + 如果因为日志不一致而失败，减少 nextIndex 重试
4. 如果存在一个满足N > commitIndex的 N，并且大多数的matchIndex[i] ≥ N成立，并且log[N].term == currentTerm成立，那么令 commitIndex 等于这个 N （5.3 和 5.4 节）

为了实现附加的
+ 选举时间
+ 心跳时间
+ 状态


## 模型：
### 选举
+ **开始**
  + 变成**追随者**
  
+ **追随者**
  + 超时 : 变成 **候选者**

+ **候选者**
 + 收到足够的选票 变成 **领导者**
 + 任期错误/收到别的领导者消息 变成 **追随者**
 + 超时，重新开始选举 **候选者**
  
+ **领导者**
  + 任期错误 变成 **追随者**
  
### 规则
只有最新日志和最新任期 才有可能成为领导者
实现：不满足拒绝投票

### 修改
+ 添加日志
+ 广播请求否则日志
+ 成功就开始提交
+ 提交成功就发出申请
