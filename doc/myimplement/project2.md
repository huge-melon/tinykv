# Project2

## 要点
1. `raft/raft.go` 实现其中的函数
2. `raft/rawnode.go`
3. `raft/log.go`
4. 了解这里边的接口定义`proto/proto/eraftpb.proto`
5. `2A`除了完成选举部分的逻辑，还要完成log部分的逻辑

## 函数说明
1. 创建一个raft节点，要注意config
```go
// newRaft return a raft peer with the given config
func newRaft(c *Config) *Raft
```

2. 使用`NewMemoryStorage()`创建config中的存储引擎
3. `raft.Step()`往前走一步
    `proto/pkg/eraftpb/eraftpb.pb.go`中定义了，不同的消息类型，需要根据不同的类型，进行处理

4. 所有需要向外发送的数据全部压入, raft.msgs中
5. raft.step() 处理接收到的数据，根据接收到不同的数据类型，进行相应的操作
6. 时钟说明：
```go
heartbeatTimeout // 发送心跳的时间间隔
heartbeatElapsed // 距离上一次发送心跳已经过去的时间

electionTimeout // 选举超时的时间间隔
electionElapsed // 距离上一次进行选举已经过去的时间
```
详细信息见`raft/raft.go`中`config`的定义
## raft基础
1. term在Raft算法中作为**逻辑时钟**
2. 服务器之间通信时会交换当term
   - 如果一个服务器的当前任期号比其他的小，该服务器会将自己的任期号更新为较大的那个值
   - 如果一个 candidate 或者 leader 发现自己的任期号过期了，它会立即回到 follower 状态
   - 如果一个节点接收到一个包含过期的任期号的请求，它会直接拒绝这个请求
3. 一个新建的raft节点初始状态应该是follower类型
### Leader 选举
1. 一个服务器节点只要能从 leader 或 candidate 处接收到有效的 RPC 就一直保持 follower 状态
2. Leader 周期性地向所有 follower 发送心跳 维持自己地位
3. 如果一个 follower 在一段选举超时时间内没有接收到任何消息，则term+1 变为candidate，开始选举
4. 选举过程：
   1. follower 先增加自己的当前任期号并且转换到 candidate 状态
   2. 投票给自己
   3. 并行地向集群中的其他服务器节点发送 RequestVote RPC
   4. **同一term，一个node只会投一次票，按先来先服务的原则**
5. 一个节点的选举结果
   1. 自己收到超半数vote，转为leader，向其他node发送心跳，确定自己的地位，其他节点收到后重置选举超时计时
   2. 其他节点成为leader，转为 follower
   3. 平局，等待超时重新选举
6. 投票的条件：
   1. 同一任期，一个node只会投一次票，先来先服务
   2. candidate 可能会收到另一个声称自己是 leader 的服务器节点发来的 AppendEntries RPC 。如果这个 leader 的任期号（包含在RPC中）不小于 candidate 当前的任期号，那么 candidate 会承认该 leader 的合法地位并回到 follower 状态。 如果 RPC 中的任期号比自己的小，那么 candidate 就会拒绝这次的 RPC 并且继续保持 candidate 状态。
   3. 拒接/投票 一个candidate发来的投票申请
      - **candidate 日志**比自己的新，则投票，否则拒绝
      - 注：这里是**要求日志一样新**，而不是Term
7. 竞争的情况：
   1. 等待投票期间，candidate 可能会收到另一个声称自己是 leader 的服务器节点发来的 AppendEntries RPC：
      - 如果 leader 的任期号（包含在RPC中）>= candidate 当前的任期号,candidate转为follower，并设置自己的leader及term
      - 否则，拒绝该RPC
   2. 票数相等：等待超时进行下一轮选举
      - 选举超时时间是从一个固定的区间（例如 150-300 毫秒）随机选择；实际情况下心跳间隔(0.5-20ms)，选举超时(10-500ms)
8. 当一个节点成为leader后：
   1. leader 必须有关于哪些日志条目被提交了的最新信息。Leader 完整性特性保证了 leader 一定拥有所有已经被提交的日志条目，但是在它任期开始的时候，它可能不知道哪些是已经被提交的。为了知道这些信息，它需要在它的任期里提交一个日志条目。Raft 通过让 leader 在任期开始的时候提交一个空的没有任何操作的日志条目到日志中来处理该问题
   2. leader 接收到一个 MessageType_MsgPropose请求
   3. 处理MessageType_MsgPropose，并调用sendAppend方法将其中的日志发送到其他节点，Message中一次可包含多条日志
   4. 在发送 AppendEntries RPC 的时候，leader 会将前一个日志条目的索引位置和任期号包含在里面。如果 follower 在它的日志中找不到包含相同索引位置和任期号的条目，那么他就会拒绝该新的日志条目。
   5. 要使得 follower 的日志跟自己一致，leader 必须找到两者达成一致的最大的日志条目（索引最大），删除 follower 日志中从那个点之后的所有日志条目，并且将自己从那个点之后的所有日志条目发送给 follower 。所有的这些操作都发生在对 AppendEntries RPCs 中一致性检查的回复中。Leader 针对每一个 follower 都维护了一个 nextIndex ，表示 leader 要发送给 follower 的下一个日志条目的索引。当选出一个新 leader 时，该 leader 将所有 nextIndex 的值都初始化为自己最后一个日志条目的 index 加1（图 7 中的 11）。如果 follower 的日志和 leader 的不一致，那么下一次 AppendEntries RPC 中的一致性检查就会失败。**在被 follower 拒绝之后，leaer 就会减小 nextIndex 值并重试** AppendEntries RPC 。最终 nextIndex 会在某个位置使得 leader 和 follower 的日志达成一致。此时，AppendEntries RPC 就会成功，将 follower 中跟 leader 冲突的日志条目全部删除然后追加 leader 中的日志条目（如果有需要追加的日志条目的话）。一旦 AppendEntries RPC 成功，follower 的日志就和 leader 一致，并且在该任期接下来的时间里保持一致。
9.  所有消息的索引，从1开始计数
10. 处理只读请求：
    首先，leader 必须有关于哪些日志条目被提交了的最新信息。Leader 完整性特性保证了 leader 一定拥有所有已经被提交的日志条目，但是在它任期开始的时候，它可能不知道哪些是已经被提交的。为了知道这些信息，它需要在它的任期里提交一个日志条目。Raft 通过让 leader 在任期开始的时候提交一个空的没有任何操作的日志条目到日志中来处理该问题。第二，leader 在处理只读请求之前必须检查自己是否已经被替代了（如果一个更新的 leader 被选举出来了，它的信息就是过时的了）。Raft 通过让 leader 在响应只读请求之前，先和集群中的过半节点交换一次心跳信息来处理该问题。另一种可选的方案，leader 可以依赖心跳机制来实现一种租约的形式，但是这种方法依赖 timing 来保证安全性（假设时间误差是有界的）。

#### Log
1. log只有在持久化之后，才可以提交
2. log只有在提交之后才可以应用
3. 新收到的log位于log.entries中
4. 进行持久化之后再更新stabled


#### 协议细节问题：
1. 如果一个node发起选举，由于遇到了一个任期号比自己还高的节点，那么其leader应该设置为谁, 目前将leader设置为term比其高的节点，但理论上应该设置为其leader id
   1. TestLeaderElectionOverwriteNewerLogs2AB
      1. 遗漏了初始化raftnode状态时，应该使用storage.InitialState()中已经持久化的参数
2. MessageType_MsgHeartbeatResponse 对心跳也要 Response, 并且要心跳要发送commit信息,如果follower没有跟上,需要启动重传机制
3. TestDuelingCandidates2AB  每次投票前,需要清空计票. 因此统计集群中节点的个数,需要使用r.Prs进行统计
4. voteRequest RPC: 如果leader 接收到一个比其大的节点发来的投票请求,则变为Follower ???? 论文中是通过日志的新旧来判断的吧. **因为任期只能增加不能减少**, 所以离线的节点疯狂超时的Term必须被同步到其他节点上.
   - 若m.Term > leader.Term: leader变为follower, 设置其Leader为none,只有当m.的日志也更新时,才接受,并设置leader为它.
5. TestProposal2AB: 只有leader才可以向外发送信息
6. TestHandleMessageType_MsgAppend2AB: 只有最新添加的数据的最后一条, 才能作为本地节点的候选提交index, 而不是直接用lastIndex()
7. TestAllServerStepdown2AB(): 变为候选者时,要清空自己的lead参数
   - handleRequestVote(): 中投票并不意味着, 那个节点变为自己的Leader
8. TestOldMessages2AB(): 对于term小于自身term，而自己又是leader的节点的appendEntry请求要忽略