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
func newEntry[K comparable, V any](key K, val V, opts ...EntryOption) *Entry[K, V] {
	e := &entryOptions{}
	for _, optFunc := range opts {
		if optFunc == nil {
			continue
		}
		optFunc(e)
	}
	return &Entry[K, V]{
		Key:            key,
		Value:          val,
		Expiration:     e.expiration,
		ReferenceCount: max(1, e.referenceCount),
	}
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

type entryOptions struct {
	expiration     time.Time
	referenceCount int
}

// EntryOption is a configurable option for cache entries.
type EntryOption func(*entryOptions)

// WithExpiration is an option to set expiration time for the cachable entry. If
// the expiration is zero or negative value, it treats it as no expiration.
func WithExpiration(exp time.Duration) EntryOption {
	return func(e *entryOptions) {
		if exp <= 0 {
			e.expiration = time.Time{}
			return
		}
		e.expiration = time.Now().Add(exp)
	}
}

// WithReferenceCount is an option to set reference count for the cachable entry.
// This option is only applicable to cache policies that have a reference count (e.g.,
// LFU). referenceCount specifies the reference count value to set for the cachable
// entry. The default is 1.
func WithReferenceCount(referenceCount int) EntryOption {
	return func(e *entryOptions) {
		e.referenceCount = max(1, referenceCount)
	}
}
