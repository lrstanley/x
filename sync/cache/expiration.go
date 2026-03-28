// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"container/heap"
	"time"
)

// expirationManager is a manager for expiration of cache entries.
type expirationManager[K comparable] struct {
	queue   expirationQueue[K]
	mapping map[K]*expirationKey[K]
}

// newExpirationManager creates a new expiration manager.
func newExpirationManager[K comparable]() *expirationManager[K] {
	q := make(expirationQueue[K], 0)
	heap.Init(&q)
	return &expirationManager[K]{
		queue:   q,
		mapping: make(map[K]*expirationKey[K]),
	}
}

// update updates the expiration time for the given key.
func (m *expirationManager[K]) update(key K, expiration time.Time) {
	if e, ok := m.mapping[key]; ok {
		e.expiration = expiration
		heap.Fix(&m.queue, e.index)
	} else {
		v := &expirationKey[K]{
			key:        key,
			expiration: expiration,
		}
		heap.Push(&m.queue, v)
		m.mapping[key] = v
	}
}

// len returns the number of entries in the expiration manager.
func (m *expirationManager[K]) len() int {
	return m.queue.Len()
}

// pop pops the next entry from the expiration manager.
func (m *expirationManager[K]) pop() K {
	v := heap.Pop(&m.queue)
	key := v.(*expirationKey[K]).key //nolint:errcheck
	delete(m.mapping, key)
	return key
}

// remove removes the given key from the expiration manager.
func (m *expirationManager[K]) remove(key K) {
	if e, ok := m.mapping[key]; ok {
		heap.Remove(&m.queue, e.index)
		delete(m.mapping, key)
	}
}

// clear clears the expiration manager.
func (m *expirationManager[K]) clear() {
	clear(m.queue)
	m.queue = m.queue[:0]
	heap.Init(&m.queue)
	clear(m.mapping)
}

type expirationKey[K comparable] struct {
	key        K
	expiration time.Time
	index      int
}

// expirationQueue implements [heap.Interface] and holds [expirationKey].
type expirationQueue[K comparable] []*expirationKey[K]

var _ heap.Interface = (*expirationQueue[int])(nil)

func (q expirationQueue[K]) Len() int { return len(q) }

func (q expirationQueue[K]) Less(i, j int) bool {
	// We want Pop to give us the least based on expiration time, not the greater.
	return q[i].expiration.Before(q[j].expiration)
}

func (q expirationQueue[K]) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *expirationQueue[K]) Push(x any) {
	n := len(*q)
	e := x.(*expirationKey[K]) //nolint:errcheck
	e.index = n
	*q = append(*q, e)
}

func (q *expirationQueue[K]) Pop() any {
	old := *q
	n := len(old)
	e := old[n-1]
	e.index = -1 // For safety
	*q = old[0 : n-1]
	return e
}
