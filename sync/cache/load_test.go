// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lrstanley/x/sync/flight"
)

// gatedLoader returns a load function that counts its calls and blocks until
// release is closed (ignoring its context), then returns val and err.
func gatedLoader(calls *atomic.Int32, release <-chan struct{}, val int, err error) func(context.Context, string) (int, error) {
	return func(context.Context, string) (int, error) {
		calls.Add(1)
		<-release
		return val, err
	}
}

func TestGetOrLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New[string, int](t.Context())
		var calls atomic.Int32
		load := func(_ context.Context, key string) (int, error) {
			calls.Add(1)
			return len(key), nil
		}

		v, err := c.GetOrLoad(t.Context(), "abc", load, WithExpiration(time.Second))
		if v != 3 || err != nil {
			t.Fatalf("GetOrLoad() = (%d, %v), want (3, nil)", v, err)
		}
		if got, ok := c.Get("abc"); !ok || got != 3 {
			t.Fatalf("Get() after load = (%d, %v), want (3, true)", got, ok)
		}

		// A hit does not call load.
		v, err = c.GetOrLoad(t.Context(), "abc", load)
		if v != 3 || err != nil || calls.Load() != 1 {
			t.Fatalf("cached GetOrLoad() = %d with %d loads, want 3 with 1 load", v, calls.Load())
		}

		// Entry options apply to the loaded entry, so it expires and reloads.
		synctest.Sleep(2 * time.Second)
		if _, ok := c.Get("abc"); ok {
			t.Fatal("loaded entry did not expire")
		}
		_, err = c.GetOrLoad(t.Context(), "abc", load)
		if err != nil || calls.Load() != 2 {
			t.Fatalf("expired GetOrLoad() err = %v with %d loads, want reload", err, calls.Load())
		}
	})
}

func TestGetOrLoad_sharesLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New[string, int](t.Context())
		var (
			calls   atomic.Int32
			release = make(chan struct{})
			wg      sync.WaitGroup
		)

		for range 10 {
			wg.Go(func() {
				v, err := c.GetOrLoad(t.Context(), "key", gatedLoader(&calls, release, 7, nil))
				if v != 7 || err != nil {
					t.Errorf("GetOrLoad() = (%d, %v), want (7, nil)", v, err)
				}
			})
		}
		synctest.Wait()
		close(release)
		wg.Wait()

		if n := calls.Load(); n != 1 {
			t.Fatalf("load called %d times, want 1", n)
		}
		if v, ok := c.Get("key"); !ok || v != 7 {
			t.Fatalf("Get() = (%d, %v), want (7, true)", v, ok)
		}
	})
}

func TestGetOrLoad_errorNotCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New[string, int](t.Context())
		errBoom := errors.New("boom")

		if _, err := c.GetOrLoad(t.Context(), "key", func(context.Context, string) (int, error) {
			return 1, errBoom
		}); !errors.Is(err, errBoom) {
			t.Fatalf("GetOrLoad() error = %v, want %v", err, errBoom)
		}
		if c.Contains("key") {
			t.Fatal("failed load was cached")
		}

		v, err := c.GetOrLoad(t.Context(), "key", func(context.Context, string) (int, error) { return 2, nil })
		if v != 2 || err != nil {
			t.Fatalf("GetOrLoad() retry = (%d, %v), want (2, nil)", v, err)
		}
	})
}

func TestGetOrLoad_writeDuringLoad(t *testing.T) {
	tests := []struct {
		name    string
		write   func(c *Cache[string, int])
		want    int
		wantHit bool
	}{
		{name: "Set", write: func(c *Cache[string, int]) { c.Set("key", 2) }, want: 2, wantHit: true},
		{name: "GetOrSet", write: func(c *Cache[string, int]) { c.GetOrSet("key", 3) }, want: 3, wantHit: true},
		{name: "Delete", write: func(c *Cache[string, int]) { c.Delete("key") }},
		{name: "Clear", write: func(c *Cache[string, int]) { c.Clear() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := New[string, int](t.Context())
				var (
					calls   atomic.Int32
					release = make(chan struct{})
					loaded  int
				)

				done := make(chan struct{})
				go func() {
					defer close(done)
					loaded, _ = c.GetOrLoad(t.Context(), "key", gatedLoader(&calls, release, 1, nil))
				}()
				synctest.Wait()

				tt.write(c)
				close(release)
				<-done

				if loaded != 1 {
					t.Fatalf("GetOrLoad() = %d, want loaded value 1", loaded)
				}
				if v, ok := c.Get("key"); ok != tt.wantHit || v != tt.want {
					t.Fatalf("Get() = (%d, %v), want (%d, %v); stale load overwrote %s", v, ok, tt.want, tt.wantHit, tt.name)
				}
				if len(c.loading) != 0 {
					t.Fatalf("%d load tokens leaked", len(c.loading))
				}
			})
		})
	}
}

func TestGetOrLoad_abandonedLoadDoesNotStore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New[string, int](t.Context())
		var (
			calls    atomic.Int32
			release1 = make(chan struct{})
			release2 = make(chan struct{})
		)

		// The only caller of the first load gives up, so the key is forgotten
		// while that load keeps running.
		ctx, cancel := context.WithCancel(t.Context())
		abandoned := make(chan struct{})
		go func() {
			defer close(abandoned)
			if _, err := c.GetOrLoad(ctx, "key", gatedLoader(&calls, release1, 1, nil)); !errors.Is(err, context.Canceled) {
				t.Errorf("abandoned GetOrLoad() error = %v, want context.Canceled", err)
			}
		}()
		synctest.Wait()
		cancel()
		<-abandoned

		var second int
		done := make(chan struct{})
		go func() {
			defer close(done)
			second, _ = c.GetOrLoad(t.Context(), "key", gatedLoader(&calls, release2, 2, nil))
		}()
		synctest.Wait()

		// The older load finishing first must not store its result.
		close(release1)
		synctest.Wait()
		if c.Contains("key") {
			t.Fatal("abandoned load stored its result")
		}

		close(release2)
		<-done
		if v, ok := c.Get("key"); second != 2 || !ok || v != 2 {
			t.Fatalf("second load = %d, Get() = (%d, %v), want 2 cached", second, v, ok)
		}
		if n := calls.Load(); n != 2 {
			t.Fatalf("load called %d times, want 2", n)
		}
	})
}

func TestGetOrLoad_panic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New[string, int](t.Context())

		func() {
			defer func() {
				if _, ok := recover().(*flight.PanicError); !ok {
					t.Fatal("GetOrLoad did not panic with *flight.PanicError")
				}
			}()
			_, _ = c.GetOrLoad(t.Context(), "key", func(context.Context, string) (int, error) { panic("oops") })
		}()

		if c.Contains("key") || len(c.loading) != 0 {
			t.Fatal("panicking load left state behind")
		}
		if v, err := c.GetOrLoad(t.Context(), "key", func(context.Context, string) (int, error) { return 4, nil }); v != 4 || err != nil {
			t.Fatalf("GetOrLoad() after panic = (%d, %v)", v, err)
		}
	})
}
