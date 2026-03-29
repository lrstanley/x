// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lrstanley/x/sync/cache/policy/fifo"
	"github.com/lrstanley/x/sync/cache/policy/lfu"
	"github.com/lrstanley/x/sync/cache/policy/lru"
	"github.com/lrstanley/x/sync/cache/policy/mru"
)

func TestDeletedCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		nc := New[string, int](ctx)
		key := "key"
		nc.Set(key, 1, WithExpiration(1*time.Second))
		time.Sleep(2 * time.Second)
		synctest.Wait()
		_, ok := nc.cache.Get(key)
		if !ok {
			t.Fatal("want true")
		}

		nc.DeleteExpired()

		_, ok = nc.cache.Get(key)
		if ok {
			t.Fatal("want false")
		}
	})
}

func TestGetOrSetUpdatesExpirationManager(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		c := New[string, int](ctx)
		_, loaded := c.GetOrSet("k", 1, WithExpiration(time.Millisecond))
		if loaded {
			t.Fatal("want store")
		}
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		c.DeleteExpired()
		if c.Len() != 0 {
			t.Fatalf("want empty cache after expiry, got len %d", c.Len())
		}
	})
}

func TestDeleteExpired(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			base := time.Now()
			c := New[string, int](ctx)

			c.Set("0", 0)
			c.Set("1", 10, WithExpiration(10*time.Millisecond))
			c.Set("2", 20, WithExpiration(20*time.Millisecond))
			c.Set("3", 30, WithExpiration(30*time.Millisecond))
			c.Set("4", 40, WithExpiration(40*time.Millisecond))
			c.Set("5", 50)

			maxEntries := c.Len()

			expEntries := 2

			for i := 0; i <= maxEntries; i++ {
				target := base.Add(time.Duration(i)*10*time.Millisecond + time.Millisecond)
				time.Sleep(time.Until(target))
				synctest.Wait()
				c.DeleteExpired()

				got := c.Len()
				want := max(maxEntries-i, expEntries)
				if want != got {
					t.Errorf("want %d entries but got %d", want, got)
				}
			}
		})
	})

	t.Run("with remove", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			base := time.Now()
			c := New[string, int](ctx)

			c.Set("0", 0)
			c.Set("1", 10, WithExpiration(10*time.Millisecond))
			c.Set("2", 20, WithExpiration(20*time.Millisecond))

			c.Delete("1")

			time.Sleep(time.Until(base.Add(30*time.Millisecond + time.Millisecond)))
			synctest.Wait()
			c.DeleteExpired()

			keys := c.Keys()
			want := 1
			if want != len(keys) {
				t.Errorf("want %d entries but got %d", want, len(keys))
			}
		})
	})

	t.Run("with update", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			base := time.Now()
			c := New[string, int](ctx)

			c.Set("0", 0)
			c.Set("1", 10, WithExpiration(10*time.Millisecond))
			c.Set("2", 20, WithExpiration(20*time.Millisecond))
			c.Set("1", 30, WithExpiration(30*time.Millisecond)) // update

			maxEntries := c.Len()

			time.Sleep(time.Until(base.Add(10*time.Millisecond + time.Millisecond)))
			synctest.Wait()
			c.DeleteExpired()

			got1 := c.Len()
			want1 := maxEntries
			if want1 != got1 {
				t.Errorf("want1 %d entries but got1 %d", want1, got1)
			}

			time.Sleep(time.Until(base.Add(30*time.Millisecond + time.Millisecond)))
			synctest.Wait()
			c.DeleteExpired()

			got2 := c.Len()
			want2 := 1
			if want2 != got2 {
				t.Errorf("want2 %d entries but got2 %d", want2, got2)
			}
		})
	})

	t.Run("expect not expired reset existing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			base := time.Now()
			c := New[string, int](ctx)

			c.Set("1", 10, WithExpiration(10*time.Millisecond))
			c.Set("2", 20, WithExpiration(20*time.Millisecond))
			c.Set("1", 30, WithExpiration(100*time.Millisecond)) // Do not expire key "1" because it is reset.

			time.Sleep(time.Until(base.Add(30*time.Millisecond + time.Millisecond)))
			synctest.Wait()
			c.DeleteExpired()

			got := c.Len()
			if want := 1; want != got {
				t.Errorf("want %d entries but got %d", want, got)
			}
		})
	})

	t.Run("expect not expired set zero expiration", func(t *testing.T) {
		c := New[string, int](t.Context())
		c.Set("1", 4, WithExpiration(0))  // These should not be expired.
		c.Set("2", 5, WithExpiration(-1)) // These should not be expired.
		c.Set("3", 6, WithExpiration(1*time.Hour))

		want := true
		_, ok := c.Get("1")
		if ok != want {
			t.Errorf("want %t but got %t", want, ok)
		}

		_, ok = c.Get("2")
		if ok != want {
			t.Errorf("want %t but got %t", want, ok)
		}
		_, ok = c.Get("3")
		if ok != want {
			t.Errorf("want %t but got %t", want, ok)
		}
	})
}

func TestDeleteExpiredConcurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		c := New[string, int](ctx)
		for i := range 50 {
			c.Set(strconv.Itoa(i), i, WithExpiration(time.Millisecond))
		}
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()

		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				for range 200 {
					c.DeleteExpired()
				}
			})
		}
		for range 20 {
			wg.Go(func() {
				for range 200 {
					c.Delete(strconv.Itoa(rand.IntN(50)))
				}
			})
		}
		wg.Wait()
	})
}

func TestClear(t *testing.T) {
	cases := []struct {
		name   string
		policy Option[string, int]
	}{
		{name: "base", policy: nil},
		{name: "lru", policy: WithLRU[string, int](lru.WithCapacity(10))},
		{name: "mru", policy: WithMRU[string, int](mru.WithCapacity(10))},
		{name: "fifo", policy: WithFIFO[string, int](fifo.WithCapacity(10))},
		{name: "lfu", policy: WithLFU[string, int](lfu.WithCapacity(10))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var c *Cache[string, int]
			if tc.policy == nil {
				c = New[string, int](ctx)
			} else {
				c = New(ctx, tc.policy)
			}
			c.Set("a", 1, WithExpiration(time.Hour))
			c.Set("b", 2, WithExpiration(2*time.Hour))
			c.Clear()
			if c.Len() != 0 {
				t.Fatalf("want len 0, got %d", c.Len())
			}
			if _, ok := c.Get("a"); ok {
				t.Fatal("want miss for a")
			}
			if _, ok := c.Get("b"); ok {
				t.Fatal("want miss for b")
			}
		})
	}
}

func TestWithDefaultEntryOptions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		c := New(ctx, WithDefaultEntryOptions[string, int](WithExpiration(10*time.Millisecond)))

		c.Set("a", 1)
		if _, ok := c.Get("a"); !ok {
			t.Fatal("want hit for a before expiry")
		}

		time.Sleep(20 * time.Millisecond)
		synctest.Wait()

		if _, ok := c.Get("a"); ok {
			t.Fatal("want miss for a after default expiration elapsed")
		}
	})
}

func TestConcurrent(t *testing.T) {
	cases := []struct {
		name   string
		policy Option[int, int]
	}{
		{
			name:   "base",
			policy: func(_ *Cache[int, int]) {},
		},
		{
			name:   "lru",
			policy: WithLRU[int, int](lru.WithCapacity(10)),
		},
		{
			name:   "mru",
			policy: WithMRU[int, int](mru.WithCapacity(10)),
		},
		{
			name:   "fifo",
			policy: WithFIFO[int, int](fifo.WithCapacity(10)),
		},
		{
			name:   "lfu",
			policy: WithLFU[int, int](lfu.WithCapacity(10)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			c := New(t.Context(), tc.policy)
			var wg sync.WaitGroup
			for range 100 {
				wg.Go(func() {
					for range 100 {
						key := rand.IntN(100000)
						c.Set(key, rand.IntN(100000))
						c.Get(key)
					}
				})
			}

			wg.Wait()
		})
	}
}
