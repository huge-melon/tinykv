// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raft

import (
	"errors"
	"math/rand"
	"time"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// None is a placeholder node ID used when there is no leader.
const None uint64 = 0

// StateType represents the role of a node in a cluster.
type StateType uint64

const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

var stmap = [...]string{
	"StateFollower",
	"StateCandidate",
	"StateLeader",
}

func (st StateType) String() string {
	return stmap[uint64(st)]
}

// ErrProposalDropped is returned when the proposal is ignored by some cases,
// so that the proposer can be notified and fail fast.
var ErrProposalDropped = errors.New("raft proposal dropped")

// Config contains the parameters to start a raft.
type Config struct {
	// ID is the identity of the local raft. ID cannot be 0.
	ID uint64

	// peers contains the IDs of all nodes (including self) in the raft cluster. It
	// should only be set when starting a new raft cluster. Restarting raft from
	// previous configuration will panic if peers is set. peer is private and only
	// used for testing right now.
	peers []uint64

	// ElectionTick is the number of Node.Tick invocations that must pass between
	// elections. That is, if a follower does not receive any message from the
	// leader of current term before ElectionTick has elapsed, it will become
	// candidate and start an election. ElectionTick must be greater than
	// HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid
	// unnecessary leader switching.
	ElectionTick int
	// HeartbeatTick is the number of Node.Tick invocations that must pass between
	// heartbeats. That is, a leader sends heartbeat messages to maintain its
	// leadership every HeartbeatTick ticks.
	HeartbeatTick int

	// Storage is the storage for raft. raft generates entries and states to be
	// stored in storage. raft reads the persisted entries and states out of
	// Storage when it needs. raft reads out the previous state and configuration
	// out of storage when restarting.
	Storage Storage
	// Applied is the last applied index. It should only be set when restarting
	// raft. raft will not return entries to the application smaller or equal to
	// Applied. If Applied is unset when restarting, raft might return previous
	// applied entries. This is a very application dependent configuration.
	Applied uint64
}

func (c *Config) validate() error {
	if c.ID == None {
		return errors.New("cannot use none as id")
	}

	if c.HeartbeatTick <= 0 {
		return errors.New("heartbeat tick must be greater than 0")
	}

	if c.ElectionTick <= c.HeartbeatTick {
		return errors.New("election tick must be greater than heartbeat tick")
	}

	if c.Storage == nil {
		return errors.New("storage cannot be nil")
	}

	return nil
}

// Progress represents a follower’s progress in the view of the leader. Leader maintains
// progresses of all followers, and sends entries to the follower based on its progress.
type Progress struct {
	Match, Next uint64
}

type Raft struct {
	id uint64

	Term uint64

	// vote to which peer
	Vote uint64

	// the log
	RaftLog *RaftLog

	// log replication progress of each peers
	Prs map[uint64]*Progress

	// // commitCount counts commit node at nextIndex
	// commitCount uint32
	// this peer's role
	State StateType

	// votes records
	votes map[uint64]bool

	// msgs need to send
	msgs []pb.Message

	// votesCount counts affirmative votes
	votesCount uint32

	// the leader id
	Lead uint64

	// heartbeat interval, should send
	heartbeatTimeout int
	// baseline of election interval
	electionTimeout int
	// random election interval based on electionTimeout
	currentElectionTimeout int

	// number of ticks since it reached last heartbeatTimeout.
	// only leader keeps heartbeatElapsed.
	heartbeatElapsed int
	// Ticks since it reached last electionTimeout when it is leader or candidate.
	// Number of ticks since it reached last electionTimeout or received a
	// valid message from current leader when it is a follower.
	electionElapsed int

	// leadTransferee is id of the leader transfer target when its value is not zero.
	// Follow the procedure defined in section 3.10 of Raft phd thesis.
	// (https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf)
	// (Used in 3A leader transfer)
	leadTransferee uint64

	// Only one conf change may be pending (in the log, but not yet
	// applied) at a time. This is enforced via PendingConfIndex, which
	// is set to a value >= the log index of the latest pending
	// configuration change (if any). Config changes are only allowed to
	// be proposed if the leader's applied index is greater than this
	// value.
	// (Used in 3A conf change)
	PendingConfIndex uint64
}

// newRaft return a raft peer with the given config
func newRaft(c *Config) *Raft {
	if err := c.validate(); err != nil {
		panic(err.Error())
	}
	// Your Code Here (2A).
	votes := make(map[uint64]bool)
	prs := make(map[uint64]*Progress)
	for _, peer := range c.peers {
		votes[peer] = false
		prs[peer] = &Progress{}

	}
	rand.Seed(time.Now().UnixNano())
	// newRaft(newTestConfig(id, peers, election, heartbeat, storage))
	return &Raft{
		id:                     c.ID,
		electionTimeout:        c.ElectionTick,
		currentElectionTimeout: rand.Intn(c.ElectionTick) + c.ElectionTick,
		heartbeatTimeout:       c.HeartbeatTick,
		State:                  StateFollower,
		votes:                  votes,
		Prs:                    prs,
		// msgs:                   make([]pb.Message, 1),
		Term:    0, // 初始term为0
		RaftLog: newLog(c.Storage),
	}
}

// sendAppend sends an append RPC with new entries (if any) and the
// current commit index to the given peer. Returns true if a message was sent.
func (r *Raft) sendAppend(to uint64) bool {
	// Your Code Here (2A).
	r.Vote = None
	var prevLogTerm uint64
	var prevLogIndex uint64
	lastI := r.RaftLog.LastIndex()

	if r.Prs[to].Next == lastI+1 { // 发送commit空消息
		prevLogIndex = lastI
		prevLogTerm, _ = r.RaftLog.Term(lastI)

		message := pb.Message{
			MsgType: pb.MessageType_MsgAppend,
			From:    r.id,
			To:      to,
			Term:    r.Term,
			LogTerm: prevLogTerm,
			Index:   prevLogIndex,
			Commit:  r.RaftLog.committed,
		}
		r.msgs = append(r.msgs, message)
		return true
	}

	prevLogIndex = r.Prs[to].Next - 1
	prevLogTerm, _ = r.RaftLog.Term(prevLogIndex)
	// entry, _ := r.RaftLog.GetEntryByIndex(r.Prs[to].Next)
	// 发的时候不是一条一条的发送，而是从next->last均发送
	if entries, err := r.RaftLog.GetEntries(r.Prs[to].Next, lastI+1); err == nil {
		// Message 中的Index和LogTerm对应论文中的preLogIndex 和 prevLogTerm
		entsP := []*pb.Entry{}
		for i := 0; i < len(entries); i++ {
			entsP = append(entsP, &entries[i])
		}

		message := pb.Message{
			MsgType: pb.MessageType_MsgAppend,
			From:    r.id,
			To:      to,
			Term:    r.Term,
			LogTerm: prevLogTerm,
			Index:   prevLogIndex,
			Entries: entsP,
			Commit:  r.RaftLog.committed,
		}
		r.msgs = append(r.msgs, message)
		return true
	}
	return false
}

// sendHeartbeat sends a heartbeat RPC to the given peer.
func (r *Raft) sendHeartbeat(to uint64) {
	// Your Code Here (2A).
	r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgHeartbeat, To: to, From: r.id, Term: r.Term})
}

// sendRequestVote sends a RequestVote RPC to the given peer. MINE
func (r *Raft) sendRequestVote(to uint64) {
	logTerm, _ := r.RaftLog.Term(r.RaftLog.LastIndex())
	r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgRequestVote, To: to, From: r.id, Term: r.Term, Index: r.RaftLog.LastIndex(), LogTerm: logTerm})

}

// tick advances the internal logical clock by a single tick.
func (r *Raft) tick() {
	// Your Code Here (2A).
	r.electionElapsed++
	r.heartbeatElapsed++
	switch r.State {
	case StateLeader:
		if r.heartbeatElapsed == r.heartbeatTimeout {
			// leader以heartbeatTimeout为间隔，向其他node发送心跳
			r.electionElapsed = 0
			r.Step(pb.Message{MsgType: pb.MessageType_MsgBeat, From: r.id, To: r.id})

		}
	case StateCandidate:
		if r.electionElapsed == r.currentElectionTimeout {
			r.electionElapsed = 0
			r.Step(pb.Message{MsgType: pb.MessageType_MsgHup, From: r.id, To: r.id})
		}
	case StateFollower:
		if r.electionElapsed == r.currentElectionTimeout {
			r.electionElapsed = 0
			r.Step(pb.Message{MsgType: pb.MessageType_MsgHup, From: r.id, To: r.id})
		}
	}

}

// becomeFollower transform this peer's state to Follower
func (r *Raft) becomeFollower(term uint64, lead uint64) {
	// Your Code Here (2A).
	r.Term = term
	r.Lead = lead
	r.State = StateFollower
}

// becomeCandidate transform this peer's state to candidate
func (r *Raft) becomeCandidate() {
	// Your Code Here (2A).
	r.Term++
	// 在变为candidate的时候才增加Term
	r.State = StateCandidate
}

// becomeLeader transform this peer's state to leader
func (r *Raft) becomeLeader() {
	// Your Code Here (2A).
	// NOTE: Leader should propose a noop entry on its term
	r.State = StateLeader
	// something wrong at there
	for peer, _ := range r.Prs { // 将所有nextIndex 初始化为自己最后一个log的index加一

		r.Prs[peer].Match = 0                        // 初始状态下match为0
		r.Prs[peer].Next = r.RaftLog.LastIndex() + 1 // 初始状态下为leader最后一条日志+1

	}

	// 成为leader 后向本地节点添加一个空的entry
	preIndex := r.RaftLog.LastIndex()
	preTerm, _ := r.RaftLog.Term(preIndex)

	entry := pb.Entry{
		EntryType: pb.EntryType_EntryNormal,
		Term:      r.Term,
		Index:     preIndex + 1,
		Data:      nil,
	}
	message := pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		From:    r.id,
		To:      r.id,
		Term:    r.Term,
		LogTerm: preTerm,
		Index:   preIndex,
		Entries: []*pb.Entry{&entry},
	}
	r.Step(message) // 向本地发送一个空消息
}

// Step the entrance of handle message, see `MessageType`
// on `eraftpb.proto` for what msgs should be handled
func (r *Raft) Step(m pb.Message) error {
	// Your Code Here (2A).

	switch m.GetMsgType() {
	case pb.MessageType_MsgHeartbeat:
		r.handleHeartbeat(m) // 处理接收到的心跳

	case pb.MessageType_MsgHup:
		// 超时开始选举，不负责Term更新，Term更新应该放在这里，根据TestLeaderCycle2AA中的逻辑
		// 从测试代码的逻辑中，应该先超时后先发送信息，接到信息后再转换为condidate
		// 并且term的修改在tick()中完成
		if r.State == StateLeader { // 如果当前状态是leader，则不需要
			break
		}

		// random reset electionTimeout
		rand.Seed(time.Now().UnixNano())
		r.currentElectionTimeout = rand.Intn(r.electionTimeout) + r.electionTimeout
		r.becomeCandidate()

		r.votes[r.id] = true // vote itself
		r.Vote = r.id
		r.votesCount = 1
		if len(r.votes) < 2 {
			r.becomeLeader() // if there is only one node in cluster, Become Leader immediately.
		}
		for peer, _ := range r.votes {
			if peer != r.id {
				r.votes[peer] = false
				r.sendRequestVote(peer)
			}
		}
	case pb.MessageType_MsgBeat:
		if r.State == StateLeader {
			for peer, _ := range r.votes {
				if peer != r.id {
					r.sendHeartbeat(peer)
				}
			}
		}
	case pb.MessageType_MsgRequestVote: // 处理其他节点发来的投票请求
		r.handleRequestVote(m)
	case pb.MessageType_MsgRequestVoteResponse: // 处理其他节点返回的投票结果
		if r.votes[m.GetFrom()] = !m.Reject; !m.Reject {
			r.votesCount++
			if r.votesCount > uint32(len(r.Prs)/2) {
				r.becomeLeader()
			}
		}

	case pb.MessageType_MsgPropose: // 将本地消息添加到本地的队列中，并发送给其他节点
		for _, ents := range m.Entries { // 将消息添加到log的entries队列中
			ents.Term = r.Term
			ents.Index = r.RaftLog.LastIndex() + 1
			r.RaftLog.Append([]*pb.Entry{ents})
		}
		// TestLeaderStartReplication2AB 先不发送 这里是应该发送信息的，但是noop的
		if len(r.Prs) == 1 { // 如果只有一个节点，则直接提交
			r.RaftLog.CommitIndex(r.RaftLog.LastIndex())
		}
		for peer, _ := range r.Prs {
			if peer != r.id {
				r.sendAppend(peer) // 从next 开始发消息
			}
		}

	case pb.MessageType_MsgAppend:
		r.handleAppendEntries(m) // 处理接收到的entry信息

	case pb.MessageType_MsgAppendResponse: // 如果一个消息，通过的数量超过一半，则提交
		r.handleAppendEntriesResponse(m)
	}
	return nil
}

func (r *Raft) handleAppendEntriesResponse(m pb.Message) {
	// 处理每个node返回的请求，统计当前消息返回true的数量
	if m.Reject {
		if m.Term > r.Term {
			// Reply false if term < currentTerm
			r.Term = m.Term
			r.sendAppend(m.From)
		} else {
			// Reply false if log doesn’t contain an entry at prevLogIndex whose term matches prevLogTerm
			// If AppendEntries fails because of log inconsistency: decrement nextIndex and retry
			r.Prs[m.From].Next--
			r.sendAppend(m.From)
		}

	} else if m.Index != r.Prs[m.From].Match { // 忽略重复发送的消息
		r.Prs[m.From].Match = m.Index
		r.Prs[m.From].Next = r.Prs[m.From].Match + 1
		// if r.Prs[m.From].Next < r.RaftLog.LastIndex()+1 { // 如果是批量发送，那么这里的逻辑可能有问题
		// 	// 继续发送
		// 	r.sendAppend(m.From)
		// }
		firstTermIndex := r.RaftLog.LastIndex()
		for firstTermIndex-1 > 0 {
			curTerm, _ := r.RaftLog.Term(firstTermIndex - 1)
			if curTerm == r.Term {
				firstTermIndex--
			} else {
				break
			}

		}

		if m.Index >= firstTermIndex && m.Index > r.RaftLog.committed {
			count := 1
			for id, v := range r.Prs { // 这里的过程可以使用一个全局的变量来替代 **
				if id != r.id && v.Match >= m.Index {
					count++
				}
			}
			if count > len(r.Prs)/2 {
				r.RaftLog.CommitIndex(m.Index)
				for id, _ := range r.Prs {
					if id != r.id {
						r.sendAppend(id)
					}
				}
			}
		}
	}
}

// handleAppendEntries handle AppendEntries RPC request
func (r *Raft) handleAppendEntries(m pb.Message) {
	// Your Code Here (2A).

	// 接收到来自leader的日志请求
	if r.Term <= m.Term {
		r.becomeFollower(m.Term, m.From)

		// m.Entries是slice类型，所以直接将所有的entries都添加到storage中
		// Reply false if log doesn’t contain an entry at prevLogIndex whose term matches prevLogTerm
		// 有可能leader发生网络分区，leader包含了一系列未提交的日志，所以要写的日志应该由message中包含的index来决定
		localTerm, _ := r.RaftLog.Term(m.Index)
		if localTerm != m.LogTerm { // 没有对应的index 所对应的Term和消息中的不一样
			r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgAppendResponse, From: r.id, To: m.From, Term: r.Term, Reject: true})
			return
		}

		//If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
		for i := 0; i < len(m.Entries); i++ {
			localTerm, _ = r.RaftLog.Term(m.Entries[i].Index)
			if localTerm == m.Entries[i].Term {
				m.Entries = m.Entries[1:]
			} else {
				break
			}
		}
		if len(m.Entries) != 0 {
			if m.Entries[0].Index <= r.RaftLog.stabled { // 这里的等于号卡了半天
				r.RaftLog.stabled = m.Entries[0].Index - 1
			}
		}

		r.RaftLog.Append(m.Entries)
		if m.Commit > r.RaftLog.committed {
			if m.Commit >= r.RaftLog.LastIndex() {
				r.RaftLog.CommitIndex(r.RaftLog.LastIndex())
			} else {
				r.RaftLog.CommitIndex(m.Commit)
			}

		}
		r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgAppendResponse, From: r.id, To: m.From, Term: r.Term, Index: r.RaftLog.LastIndex(), Reject: false})
		// dataIndex := r.RaftLog.LastIndex()
		// dataTerm, _ := r.RaftLog.Term(dataIndex)

		// if entry.Term > dataTerm || (entry.Term == dataTerm && entry.Index >= dataIndex) {
		// 	r.becomeFollower(m.GetTerm(), m.GetFrom())
		// 	r.RaftLog.entries = append(r.RaftLog.entries, *entry)
		// 	r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgAppendResponse, From: r.id, To: m.From, Reject: false})
		// }

	} else { // Reply false if leader.term < currentTerm
		r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgAppendResponse, From: r.id, To: m.From, Term: r.Term, Reject: true})
		return
	}

}

// handleHeartbeat handle Heartbeat RPC request
func (r *Raft) handleHeartbeat(m pb.Message) {
	// Your Code Here (2A).
	// 接收到有效的heatbeat才会重置选举超时
	if m.Term >= r.Term { // 发送heartbeat的前提是，已经选举成为leader，
		r.becomeFollower(m.GetTerm(), m.GetFrom())
		r.electionElapsed = 0
	}
}

// handleRequestVote handle Vote RPC request MINE
func (r *Raft) handleRequestVote(m pb.Message) {
	var reject bool
	// var err error
	rLogTerm, _ := r.RaftLog.Term(r.RaftLog.LastIndex()) // 没有处理err **

	if m.LogTerm < rLogTerm { // 先比较任期号
		reject = true
	} else if m.LogTerm == rLogTerm && m.Index < r.RaftLog.LastIndex() { // 任期号相同，比较最后一个索引的大小
		reject = true
	} else if m.Term == r.Term && r.Vote != None && r.Vote != m.From { // 一个node每个Term只能投一次票
		reject = true
	}

	if !reject {
		r.Vote = m.From
		r.becomeFollower(m.Term, m.From)
	}
	r.msgs = append(r.msgs, pb.Message{From: r.id, To: m.From, Term: r.Term, MsgType: pb.MessageType_MsgRequestVoteResponse, Reject: reject})

}

// handleSnapshot handle Snapshot RPC request
func (r *Raft) handleSnapshot(m pb.Message) {
	// Your Code Here (2C).
}

// addNode add a new node to raft group
func (r *Raft) addNode(id uint64) {
	// Your Code Here (3A).
}

// removeNode remove a node from raft group
func (r *Raft) removeNode(id uint64) {
	// Your Code Here (3A).
}
