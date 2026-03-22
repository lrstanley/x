// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package lfu

import (
	"container/heap"
	"time"
)

type entry[K comparable, V any] struct {
	index          int
	key            K
	val            V
	referenceCount int
	referencedAt   time.Time
}

func newEntry[K comparable, V any](key K, val V) *entry[K, V] {
	refCount := 1
	if getter, ok := any(val).(interface{ GetReferenceCount() int }); ok {
		refCount = getter.GetReferenceCount()
	}
	return &entry[K, V]{
		index:          0,
		key:            key,
		val:            val,
		referenceCount: refCount,
		referencedAt:   time.Now(),
	}
}

func (e *entry[K, V]) referenced() {
	e.referenceCount++
	e.referencedAt = time.Now()
}

// priorityQueue is a min-heap ordered by reference count, then by referencedAt
// when counts tie. It implements [heap.Interface].
type priorityQueue[K comparable, V any] []*entry[K, V]

func newPriorityQueue[K comparable, V any](capacity int) *priorityQueue[K, V] {
	pq := make(priorityQueue[K, V], 0, capacity)
	return &pq
}

var _ heap.Interface = (*priorityQueue[struct{}, any])(nil)

func (q priorityQueue[K, V]) Len() int { return len(q) }

func (q priorityQueue[K, V]) Less(i, j int) bool {
	if q[i].referenceCount == q[j].referenceCount {
		return q[i].referencedAt.Before(q[j].referencedAt)
	}
	return q[i].referenceCount < q[j].referenceCount
}

func (q priorityQueue[K, V]) Swap(i, j int) {
	if len(q) < 2 {
		return
	}
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *priorityQueue[K, V]) Push(x any) {
	ent := x.(*entry[K, V]) //nolint:errcheck
	ent.index = len(*q)
	*q = append(*q, ent)
}

func (q *priorityQueue[K, V]) Pop() any {
	old := *q
	n := len(old)
	if n == 0 {
		return nil
	}
	ent := old[n-1]
	old[n-1] = nil // avoid memory leak
	ent.index = -1
	*q = old[0 : n-1]
	return ent
}

func (q *priorityQueue[K, V]) update(e *entry[K, V], val V) {
	e.val = val
	e.referenced()
	heap.Fix(q, e.index)
}
