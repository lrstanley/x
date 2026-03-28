// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package lfu

import (
	"container/heap"
)

const DefaultCapacity = 128

// Policy is a non-thread safe LFU (least frequently used) cache policy. The
// entry with the lowest access frequency is evicted when capacity is reached.
// When two entries share the same frequency, the older access (by internal
// timestamp) is evicted first.
type Policy[K comparable, V any] struct {
	entries  map[K]*entry[K, V]
	queue    *priorityQueue[K, V]
	capacity int
}

// Option configures a [Policy].
type Option func(*options)

type options struct {
	capacity int
}

func newOptions() *options {
	return &options{
		capacity: DefaultCapacity,
	}
}

// WithCapacity sets the maximum number of entries the cache holds before LFU
// eviction occurs. Must be 1 or greater.
func WithCapacity(capacity int) Option {
	return func(o *options) {
		o.capacity = max(1, capacity)
	}
}

// New creates a non-thread safe [Policy] using LFU eviction. The default
// capacity is 128 (see [DefaultCapacity]) unless [WithCapacity] is used.
func New[K comparable, V any](opts ...Option) *Policy[K, V] {
	o := newOptions()
	for _, optFunc := range opts {
		if optFunc == nil {
			continue
		}
		optFunc(o)
	}
	return &Policy[K, V]{
		entries:  make(map[K]*entry[K, V], o.capacity),
		queue:    newPriorityQueue[K, V](o.capacity),
		capacity: o.capacity,
	}
}

// Set adds an entry to the cache, replacing any existing entry with the same key.
//
// If value satisfies "interface{ GetReferenceCount() int }", the value of the
// GetReferenceCount() method is used to set the initial reference count.
func (c *Policy[K, V]) Set(key K, val V) {
	if e, ok := c.entries[key]; ok {
		c.queue.update(e, val)
		return
	}

	if len(c.entries) == c.capacity {
		if e := heap.Pop(c.queue); e != nil {
			delete(c.entries, e.(*entry[K, V]).key) //nolint:errcheck
		}
	}

	e := newEntry(key, val)
	heap.Push(c.queue, e)
	c.entries[key] = e
}

// Get gets an entry from the cache, returning the entry or zero value and a bool
// indicating whether the entry was found.
func (c *Policy[K, V]) Get(k K) (val V, ok bool) {
	e, found := c.entries[k]
	if !found {
		return val, ok
	}
	e.referenced()
	heap.Fix(c.queue, e.index)
	return e.val, true
}

// Keys returns the keys of the cache, in heap slice order (not insertion order).
func (c *Policy[K, V]) Keys() []K {
	keys := make([]K, 0, len(c.entries))
	for _, ent := range *c.queue {
		keys = append(keys, ent.key)
	}
	return keys
}

// Delete deletes the entry with provided key from the cache, if it exists.
func (c *Policy[K, V]) Delete(key K) {
	if e, ok := c.entries[key]; ok {
		heap.Remove(c.queue, e.index)
		delete(c.entries, key)
	}
}

// Len returns the number of entries currently stored in the cache.
func (c *Policy[K, V]) Len() int {
	return c.queue.Len()
}

// Clear removes all entries from the cache.
func (c *Policy[K, V]) Clear() {
	clear(c.entries)
	c.queue = newPriorityQueue[K, V](c.capacity)
}
