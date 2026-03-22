// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import "time"

// Entry is an entry is a wrapper for a value in the cache.
type Entry[K comparable, V any] struct {
	Key            K
	Value          V
	Expiration     time.Time
	ReferenceCount int
}

// newEntry creates a new entry with the specified options.
func newEntry[K comparable, V any](key K, val V, opts ...EntryOption[K, V]) *Entry[K, V] {
	e := &Entry[K, V]{
		Key:   key,
		Value: val,
	}
	for _, optFunc := range opts {
		if optFunc == nil {
			continue
		}
		optFunc(e)
	}
	e.ReferenceCount = max(1, e.ReferenceCount)
	return e
}

// Expired returns true if the entry has expired.
func (e *Entry[K, V]) Expired() bool {
	if e.Expiration.IsZero() { // Entry doesn't have expiration.
		return false
	}
	return time.Now().After(e.Expiration)
}

// GetReferenceCount returns reference count to be used when setting the cache
// entry for the first time.
func (e *Entry[K, V]) GetReferenceCount() int {
	return e.ReferenceCount
}

// EntryOption is a configurable option for cache entries.
type EntryOption[K comparable, V any] func(*Entry[K, V])

// WithExpiration is an option to set expiration time for the cachable entry. If
// the expiration is zero or negative value, it treats it as no expiration.
func WithExpiration[K comparable, V any](exp time.Duration) EntryOption[K, V] {
	return func(e *Entry[K, V]) {
		if exp <= 0 {
			e.Expiration = time.Time{}
			return
		}
		e.Expiration = time.Now().Add(exp)
	}
}

// WithReferenceCount is an option to set reference count for the cachable entry.
// This option is only applicable to cache policies that have a reference count (e.g.,
// LFU). referenceCount specifies the reference count value to set for the cachable
// entry. The default is 1.
func WithReferenceCount[K comparable, V any](referenceCount int) EntryOption[K, V] {
	return func(e *Entry[K, V]) {
		e.ReferenceCount = max(1, referenceCount)
	}
}
