// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package lru

import (
	"container/list"
)

const DefaultCapacity = 128

// Policy is a non-thread safe LRU (least recently used) cache policy. The
// least recently accessed entry is evicted when capacity is reached.
type Policy[K comparable, V any] struct {
	entries  map[K]*list.Element
	queue    *list.List // most recently used at front, least at back
	capacity int
}

type entry[K comparable, V any] struct {
	key K
	val V
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

// WithCapacity sets the maximum number of entries the cache holds before LRU
// eviction occurs. Must be 1 or greater.
func WithCapacity(capacity int) Option {
	return func(o *options) {
		o.capacity = max(1, capacity)
	}
}

// New creates a non-thread safe [Policy] using LRU eviction. The default
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
		entries:  make(map[K]*list.Element, o.capacity),
		queue:    list.New(),
		capacity: o.capacity,
	}
}

// Set adds an entry to the cache, replacing any existing entry with the same key.
func (c *Policy[K, V]) Set(key K, val V) {
	if e, ok := c.entries[key]; ok {
		c.queue.MoveToFront(e)
		ent := e.Value.(*entry[K, V]) //nolint:errcheck
		ent.val = val
		return
	}

	c.entries[key] = c.queue.PushFront(&entry[K, V]{
		key: key,
		val: val,
	})

	if c.queue.Len() > c.capacity {
		c.deleteOldest()
	}
}

// Get gets an entry from the cache, returning the entry or zero value and a bool
// indicating whether the entry was found.
func (c *Policy[K, V]) Get(k K) (val V, ok bool) {
	e, found := c.entries[k]
	if !found {
		return val, ok
	}
	c.queue.MoveToFront(e)
	return e.Value.(*entry[K, V]).val, true //nolint:errcheck
}

// Keys returns the keys of the cache, ordered from least recently used to most
// recently used.
func (c *Policy[K, V]) Keys() []K {
	keys := make([]K, 0, len(c.entries))
	for ent := c.queue.Back(); ent != nil; ent = ent.Prev() {
		keys = append(keys, ent.Value.(*entry[K, V]).key) //nolint:errcheck
	}
	return keys
}

// Len returns the number of entries currently stored in the cache.
func (c *Policy[K, V]) Len() int {
	return c.queue.Len()
}

// Delete deletes the entry with provided key from the cache, if it exists.
func (c *Policy[K, V]) Delete(key K) {
	if e, ok := c.entries[key]; ok {
		c.remove(e)
	}
}

// Clear removes all entries from the cache.
func (c *Policy[K, V]) Clear() {
	clear(c.entries)
	c.queue.Init()
}

func (c *Policy[K, V]) deleteOldest() {
	c.remove(c.queue.Back())
}

func (c *Policy[K, V]) remove(e *list.Element) {
	c.queue.Remove(e)
	delete(c.entries, e.Value.(*entry[K, V]).key) //nolint:errcheck
}
