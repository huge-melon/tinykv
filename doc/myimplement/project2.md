# Project2

## 要点
1. `raft/raft.go` 实现其中的函数
2. `raft/rawnode.go`
3. `raft/log.go`

4. 了解这里边的接口定义`proto/proto/eraftpb.proto`

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
6. 竞争的情况：
   1. 等待投票期间，candidate 可能会收到另一个声称自己是 leader 的服务器节点发来的 AppendEntries RPC：
      - 如果 leader 的任期号（包含在RPC中）>= candidate 当前的任期号,candidate转为follower，并设置自己的leader及term
      - 否则，拒绝该RPC
   2. 票数相等：等待超时进行下一轮选举
      - 选举超时时间是从一个固定的区间（例如 150-300 毫秒）随机选择；实际情况下心跳间隔(0.5-20ms)，选举超时(10-500ms)



   ElectionTick is the number of Node.Tick invocations that must pass between elections. That is, if a follower does not receive any message from the leader of current term before ElectionTick has elapsed, it will become candidate and start an election. ElectionTick must be greater than HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid unnecessary leader switching.


	HeartbeatTick is the number of Node.Tick invocations that must pass between heartbeats. That is, a leader sends heartbeat messages to maintain its leadership every HeartbeatTick ticks.