// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"sync"
	"time"

	"github.com/lrstanley/x/sync/cache/policy/base"
	"github.com/lrstanley/x/sync/cache/policy/fifo"
	"github.com/lrstanley/x/sync/cache/policy/lfu"
	"github.com/lrstanley/x/sync/cache/policy/lru"
	"github.com/lrstanley/x/sync/cache/policy/mru"
)

// Interface is a common cache interface.
type Interface[K comparable, V any] interface {
	// Get looks up a keys value from the cache.
	Get(key K) (value V, ok bool)
	// Set sets a value to the cache with key, replacing any existing value.
	Set(key K, val V)
	// Keys returns the keys of the cache. The order depends on the policy used.
	Keys() []K
	// Delete deletes the entry with the provided key from the cache.
	Delete(key K)
	// Len returns the number of entries in the cache.
	Len() int
}

var ( // Ensure that all of our policies implement [Interface].
	_ Interface[struct{}, any] = (*base.Policy[struct{}, any])(nil)
	_ Interface[struct{}, any] = (*lru.Policy[struct{}, any])(nil)
	_ Interface[struct{}, any] = (*lfu.Policy[struct{}, any])(nil)
	_ Interface[struct{}, any] = (*fifo.Policy[struct{}, any])(nil)
	_ Interface[struct{}, any] = (*mru.Policy[struct{}, any])(nil)
)

// Cache is a concurrent-safe generic cache implementation.
type Cache[K comparable, V any] struct {
	janitorInterval time.Duration
	janitor         *janitor

	mu         sync.Mutex
	cache      Interface[K, *Entry[K, V]]
	expManager *expirationManager[K]
}

// New creates a new concurrent-safe [Cache]. The context will be used to stop
// the internal janitor (evicts expired entries) when the context is cancelled.
//
// There are several [Cache] replacement policies, see the With* options for more
// details.
func New[K comparable, V any](ctx context.Context, opts ...Option[K, V]) *Cache[K, V] {
	cache := &Cache[K, V]{
		janitorInterval: time.Minute,
		cache:           base.New[K, *Entry[K, V]](),
		janitor:         newJanitor(ctx),
		expManager:      newExpirationManager[K](),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(cache)
	}

	go cache.janitor.run(cache.janitorInterval, cache.DeleteExpired)
	return cache
}

// Get looks up a keys value from the cache.
func (c *Cache[K, V]) Get(key K) (zero V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache.Get(key)

	if !ok {
		return zero, ok
	}

	// Returns nil if the entry has been expired. Do not delete here and leave it
	// to an external process such as Janitor.
	if e.Expired() {
		return zero, false
	}

	return e.Value, true
}

// GetOrSet atomically gets a keys value from the cache, or if the key is not present,
// sets the given value. The loaded result is true if the value was loaded, false
// if the value was stored.
func (c *Cache[K, V]) GetOrSet(key K, val V, opts ...EntryOption[K, V]) (actual V, loaded bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache.Get(key)

	if !ok || e.Expired() {
		e = newEntry(key, val, opts...)
		if !e.Expiration.IsZero() {
			c.expManager.update(key, e.Expiration)
		}
		c.cache.Set(key, e)
		return val, false
	}

	return e.Value, true
}

// DeleteExpired deletes all expired entries from the cache.
func (c *Cache[K, V]) DeleteExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for c.expManager.len() > 0 {
		key := c.expManager.pop()
		e, ok := c.cache.Get(key)
		if !ok {
			continue
		}
		if e.Expired() {
			c.cache.Delete(key)
			continue
		}
		c.expManager.update(key, e.Expiration)
		break
	}
}

// Set sets a value in the cache with the specified key, replacing any existing
// value.
func (c *Cache[K, V]) Set(key K, val V, opts ...EntryOption[K, V]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := newEntry(key, val, opts...)
	if !e.Expiration.IsZero() { // Entry doesn't have expiration.
		c.expManager.update(key, e.Expiration)
	}
	c.cache.Set(key, e)
}

// Keys returns the keys of the cache. The order is based on the cache policy.
func (c *Cache[K, V]) Keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache.Keys()
}

// Delete deletes the entry with the specified key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Delete(key)
	c.expManager.remove(key)
}

// Len returns the number of entries currently in the cache.
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache.Len()
}

// Contains reports whether the specified key is within the cache.
func (c *Cache[K, V]) Contains(key K) bool {
	c.mu.Lock()
	_, ok := c.cache.Get(key)
	c.mu.Unlock()
	return ok
}

// Option is a configurable option for the [Cache].
type Option[K comparable, V any] func(*Cache[K, V])

// WithLRU will use the LRU (Least Recently Used) cache policy for the [Cache].
func WithLRU[K comparable, V any](opts ...lru.Option) Option[K, V] {
	return func(c *Cache[K, V]) {
		c.cache = lru.New[K, *Entry[K, V]](opts...)
	}
}

// WithLFU will use the LFU (Least Frequently Used) cache policy for the [Cache].
func WithLFU[K comparable, V any](opts ...lfu.Option) Option[K, V] {
	return func(c *Cache[K, V]) {
		c.cache = lfu.New[K, *Entry[K, V]](opts...)
	}
}

// WithFIFO will use the FIFO (First In First Out) cache policy for the [Cache].
func WithFIFO[K comparable, V any](opts ...fifo.Option) Option[K, V] {
	return func(c *Cache[K, V]) {
		c.cache = fifo.New[K, *Entry[K, V]](opts...)
	}
}

// WithMRU will use the MRU (Most Recently Used) cache policy for the [Cache].
func WithMRU[K comparable, V any](opts ...mru.Option) Option[K, V] {
	return func(c *Cache[K, V]) {
		c.cache = mru.New[K, *Entry[K, V]](opts...)
	}
}

// WithJanitorInterval is an option to specify how often the [Cache] should delete
// expired entries. This is no-op if is less than or equal to 0. Default is 1 minute.
func WithJanitorInterval[K comparable, V any](d time.Duration) Option[K, V] {
	if d <= 0 {
		return nil
	}
	return func(c *Cache[K, V]) {
		c.janitorInterval = d
	}
}
