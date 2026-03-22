// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package base

import (
	"slices"
	"time"
)

// Policy is a basic non-thread safe cache policy that has no priority for evicting entries.
type Policy[K comparable, V any] struct {
	entries map[K]kvEntry[V]
}

// kvEntry holds a value and insertion time for [Policy.Keys] ordering. Stored by value in
// the map to avoid an extra heap allocation per [Policy.Set].
type kvEntry[V any] struct {
	value     V
	createdAt time.Time
}

// New creates a basic non-thread safe [Policy] that has no priority for
// evicting entries.
func New[K comparable, V any]() *Policy[K, V] {
	return &Policy[K, V]{
		entries: make(map[K]kvEntry[V]),
	}
}

// Set adds an entry to the cache, replacing any existing entry with the same key.
func (c *Policy[K, V]) Set(key K, val V) {
	c.entries[key] = kvEntry[V]{
		value:     val,
		createdAt: time.Now(),
	}
}

// Get gets an entry from the cache, returning the entry or zero value and a bool
// indicating whether the entry was found.
func (c *Policy[K, V]) Get(k K) (val V, ok bool) {
	e, found := c.entries[k]
	if !found {
		return val, false
	}
	return e.value, true
}

// Keys returns the keys of the cache, ordered by creation time.
func (c *Policy[K, _]) Keys() []K {
	keys := make([]K, 0, len(c.entries))
	for key := range c.entries {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(i, j K) int {
		return c.entries[i].createdAt.Compare(c.entries[j].createdAt)
	})
	return keys
}

// Delete deletes the entry with provided key from the cache, if it exists.
func (c *Policy[K, V]) Delete(key K) {
	delete(c.entries, key)
}

// Len returns the number of entries currently stored in the cache.
func (c *Policy[K, V]) Len() int {
	return len(c.entries)
}
