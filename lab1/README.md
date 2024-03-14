主要记录了lab1修改的文件
具体任务
每个pg-*.txt文件都是一本电子书。我们的任务是统计出所有电子书中出现过的单词，以及它们的出现次数

非分布式实现：src/main/mrsequential.go

要求 实现MapReduced,利用MapReduced框架实现单词统计功能
主要再mr文件下修改

### Coordinator (协调者)
负责分发任务包括map,和reduce。所以在Coordinator中他包含着两个队列来管理任务的分配。还需记录阶段来区分map和reduce因为reduce要在map全部结束后才能进行

设置一个TaskMetaHolder 管理所有任务
taskMetaInfo 记录状态和利用地址来方向寻找任务

acceptMeta 加入任务构造ID到任务的映射

makeMapTasks 对文件路径数组生成map任务，放入队列里

生成reduce任务同理(这里根据设定好的reduce数量生成任务)

selectReduceName获取map产生的文件切片

**分发任务**
该操作视为加锁保证并发的安全。
简单的逻辑，先分发map任务，如果队列空且没有正在进行的任务说明结束可以进入下一阶段即reduce。reduce同理

转化阶段，如果map转reduce说明map的工作已经做完，可以据此生成reduce工作

通过循环检查进入任务是否都完成

如果map reduce 任务都完成则通过Done函数退出


### worker(工作)
