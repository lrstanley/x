// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"sync"
	"testing"
)

// BenchmarkGetHit measures hot-path lookup on a single key (no expiration).
func BenchmarkGetHit(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)
	c.Set(1, 42)

	b.ReportAllocs()

	for b.Loop() {
		_, _ = c.Get(1)
	}
}

// BenchmarkSetNoExpiration measures Set without TTL (no expiration heap work).
func BenchmarkSetNoExpiration(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)
	key := 0

	b.ReportAllocs()

	for b.Loop() {
		c.Set(key, key)
		key++
	}
}

// BenchmarkGetOrSetLoaded measures GetOrSet when the key already exists.
func BenchmarkGetOrSetLoaded(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)
	c.Set(1, 42)

	b.ReportAllocs()

	for b.Loop() {
		_, _ = c.GetOrSet(1, 99)
	}
}

// BenchmarkGetOrSetStore measures GetOrSet inserting new keys (allocates entries).
func BenchmarkGetOrSetStore(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		_, _ = c.GetOrSet(i, i)
	}
}

// BenchmarkParallelGetSet exercises the mutex under concurrent readers/writers.
func BenchmarkParallelGetSet(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			k := i % 256
			i++
			if k%2 == 0 {
				c.Set(k, k)
			} else {
				_, _ = c.Get(k)
			}
		}
	})
}

// BenchmarkContains measures Contains on a present key.
func BenchmarkContains(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)
	c.Set(1, 42)

	b.ReportAllocs()

	for b.Loop() {
		_ = c.Contains(1)
	}
}

// BenchmarkKeys measures Keys() which allocates and sorts (base policy).
func BenchmarkKeys(b *testing.B) {
	ctx := b.Context()

	c := New[int, int](ctx)
	for i := range 100 {
		c.Set(i, i)
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = c.Keys()
	}
}

// BenchmarkConcurrentMixed matches TestConcurrent workload shape for regression.
func BenchmarkConcurrentMixed(b *testing.B) {
	for range b.N {
		ctx, cancel := context.WithCancel(context.Background())
		c := New[int, int](ctx)
		var wg sync.WaitGroup
		for w := range 32 {
			wg.Add(1)
			go func(seed int) {
				defer wg.Done()
				for i := range 64 {
					k := (seed*64 + i) % 4096
					c.Set(k, k)
					_, _ = c.Get(k)
				}
			}(w)
		}
		wg.Wait()
		cancel()
	}
}
