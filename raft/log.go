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

import pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"

// RaftLog manage the log entries, its struct look like:
//
//  snapshot/first.....applied....committed....stabled.....last
//  --------|------------------------------------------------|
//                            log entries
//
// for simplify the RaftLog implement should manage all log entries
// that not truncated
type RaftLog struct {
	// storage contains all stable entries since the last snapshot.
	storage Storage

	// committed is the highest log position that is known to be in
	// stable storage on a quorum of nodes.
	committed uint64

	// applied is the highest log position that the application has
	// been instructed to apply to its state machine.
	// Invariant: applied <= committed
	applied uint64

	// log entries with index <= stabled are persisted to storage.
	// It is used to record the logs that are not persisted by storage yet.
	// Everytime handling `Ready`, the unstabled logs will be included.
	stabled uint64

	// all entries that have not yet compact.
	entries []pb.Entry

	// the incoming unstable snapshot, if any.
	// (Used in 2C)
	pendingSnapshot *pb.Snapshot

	// Your Data Here (2A).
}

// newLog returns log using the given storage. It recovers the log
// to the state that it just commits and applies the latest snapshot.
func newLog(storage Storage) *RaftLog {
	// Your Code Here (2A).
	first, _ := storage.FirstIndex()
	last, _ := storage.LastIndex()
	ents, _ := storage.Entries(first, last+1)
	return &RaftLog{
		committed: 0,
		applied:   0,
		stabled:   last,
		entries:   ents,
		storage:   storage,
	}
}

// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
}

// unstableEntries return all the unstable entries
func (l *RaftLog) unstableEntries() []pb.Entry {
	// Your Code Here (2A).
	if len(l.entries) > 0 {
		offset := l.entries[0].GetIndex()
		return l.entries[l.stabled-offset+1:]
	}
	return nil
}

// nextEnts returns all the committed but not applied entries
func (l *RaftLog) nextEnts() (ents []pb.Entry) {
	// Your Code Here (2A).
	// 只有持久化之后才能提交， 目前的逻辑还没涉及到storage中的写操作
	ents, _ = l.GetEntries(l.applied+1, l.committed+1)
	return
}

// LastIndex return the last index of the log entries
func (l *RaftLog) LastIndex() uint64 {
	// Your Code Here (2A).
	if len(l.entries) > 0 {
		return l.entries[len(l.entries)-1].GetIndex()
	}
	lastI, _ := l.storage.LastIndex()
	return lastI
}

// storage
// 现在取索引时是将index作为数组下标来取的，但是index和数组下标不是对应的
func (l *RaftLog) GetEntryByIndex(i uint64) (*pb.Entry, error) {
	firstIndex, _ := l.storage.FirstIndex()

	if i > l.LastIndex() || i < firstIndex {
		return nil, ErrUnavailable
	}
	if len(l.entries) > 0 && i >= l.entries[0].GetIndex() {
		return &l.entries[i-l.entries[0].GetIndex()], nil
	} else {
		if ent, err := l.storage.Entries(i, i+1); err != nil {
			return nil, err
		} else {
			return &ent[0], nil
		}
	}
}

// GetEntries 获取指定区间内的所有entry
func (l *RaftLog) GetEntries(lo uint64, hi uint64) (ents []pb.Entry, err error) {
	if lo > hi || hi > l.LastIndex()+1 {
		return nil, ErrUnavailable
	}

	if len(l.entries) == 0 {
		ents, err = l.storage.Entries(lo, hi)
	} else {
		offset := l.entries[0].Index
		if hi < offset {
			ents, err = l.storage.Entries(lo, hi)
		} else if lo >= offset {
			ents, err = l.entries[lo-offset:hi-offset], nil
		} else {
			ents, err = l.storage.Entries(lo, offset)
			if err == nil {
				ents = append(ents, l.entries[0:hi-offset]...)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return ents, nil
}

// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	// Your Code Here (2A).
	// Term 分为两个部分，从RaftLog中取数据和从Storage中取数据
	var offset uint64
	if i == 0 {
		return 0, nil
	}
	if len(l.entries) > 0 && i >= l.entries[0].GetIndex() {
		offset = l.entries[0].GetIndex()
		if i > l.entries[len(l.entries)-1].GetIndex() {
			return None, ErrUnavailable
		} else {
			return l.entries[i-offset].GetTerm(), nil
		}
	}
	return l.storage.Term(i)
}

func (l *RaftLog) Append(ents []*pb.Entry) {
	// 插入数据, 注,不同的地方要进行修改
	// 之后不一样的数据全部丢弃
	if len(ents) == 0 {
		return
	}
	if len(l.entries) != 0 {
		offset := l.entries[0].Index

		if ents[0].Index <= offset {
			l.entries = []pb.Entry{}
		} else {
			l.entries = l.entries[:ents[0].Index-offset]
		}
	}
	for _, ent := range ents {
		l.entries = append(l.entries, *ent)
	}
}

func (l *RaftLog) CommitIndex(i uint64) {
	// 先持久化，再提交
	// 将i及i之前的所有entries 都提交
	// l.stabled = i
	l.committed = i
}
