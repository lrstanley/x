// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flight

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// blocker returns a function that counts its invocations and blocks until
// release is closed, then returns val and err.
func blocker[V any](calls *atomic.Int32, release <-chan struct{}, val V, err error) func() (V, error) {
	return func() (V, error) {
		calls.Add(1)
		<-release
		return val, err
	}
}

func TestGroup_Do(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		val     int
		err     error
		wantVal int
	}{
		{name: "value", val: 42, wantVal: 42},
		{name: "error", val: 7, err: errBoom, wantVal: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var g Group[string, int]
			got, err := g.Do("key", func() (int, error) { return tt.val, tt.err })
			if got != tt.wantVal || !errors.Is(err, tt.err) {
				t.Fatalf("Do() = (%d, %v), want (%d, %v)", got, err, tt.wantVal, tt.err)
			}
		})
	}
}

func TestGroup_Do_dedupes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g       Group[string, string]
			calls   atomic.Int32
			release = make(chan struct{})
			wg      sync.WaitGroup
		)
		const callers = 10
		results := make([]string, callers)

		fn := blocker(&calls, release, "value", nil)
		for i := range callers {
			wg.Go(func() {
				v, err := g.Do("key", fn)
				if err != nil {
					t.Errorf("Do() error = %v", err)
				}
				results[i] = v
			})
		}

		synctest.Wait() // All callers are now blocked in the single call.
		close(release)
		wg.Wait()

		if n := calls.Load(); n != 1 {
			t.Fatalf("fn called %d times, want 1", n)
		}
		for i, v := range results {
			if v != "value" {
				t.Fatalf("results[%d] = %q, want %q", i, v, "value")
			}
		}

		// The call is no longer in flight, so the next Do runs fn again.
		if _, err := g.Do("key", func() (string, error) { return "", nil }); err != nil {
			t.Fatal(err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("completed call was reused, fn called %d times", n)
		}
	})
}

func TestGroup_Do_distinctKeys(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g       Group[int, int]
			calls   atomic.Int32
			release = make(chan struct{})
			wg      sync.WaitGroup
		)
		for key := range 5 {
			wg.Go(func() {
				v, _ := g.Do(key, blocker(&calls, release, key, nil))
				if v != key {
					t.Errorf("Do(%d) = %d", key, v)
				}
			})
		}
		synctest.Wait()
		close(release)
		wg.Wait()

		if n := calls.Load(); n != 5 {
			t.Fatalf("fn called %d times, want 5", n)
		}
	})
}

func TestGroup_Forget(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g        Group[string, int]
			calls    atomic.Int32
			release1 = make(chan struct{})
			release2 = make(chan struct{})
		)

		first := g.DoChan("key", blocker(&calls, release1, 1, nil))
		synctest.Wait()
		g.Forget("key")

		second := g.DoChan("key", blocker(&calls, release2, 2, nil))
		synctest.Wait()
		if n := calls.Load(); n != 2 {
			t.Fatalf("fn called %d times after Forget, want 2", n)
		}

		// Finishing the forgotten call must not remove the new one.
		close(release1)
		if r := <-first; r.Val != 1 {
			t.Fatalf("first result = %d, want 1", r.Val)
		}
		joined := g.DoChan("key", blocker(&calls, nil, 3, nil))

		close(release2)
		if r := <-second; r.Val != 2 {
			t.Fatalf("second result = %d, want 2", r.Val)
		}
		if r := <-joined; r.Val != 2 || !r.Shared {
			t.Fatalf("joined result = %+v, want shared value 2", r)
		}
	})
}

func TestGroup_DoChan(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g       Group[string, int]
			calls   atomic.Int32
			release = make(chan struct{})
		)

		solo := <-g.DoChan("solo", func() (int, error) { return 1, nil })
		if solo.Val != 1 || solo.Err != nil || solo.Shared {
			t.Fatalf("solo result = %+v", solo)
		}

		a := g.DoChan("key", blocker(&calls, release, 5, nil))
		b := g.DoChan("key", blocker(&calls, release, 6, nil))
		synctest.Wait()

		var syncVal int
		done := make(chan struct{})
		go func() {
			defer close(done)
			syncVal, _ = g.Do("key", blocker(&calls, release, 7, nil))
		}()
		synctest.Wait()
		close(release)
		<-done

		for _, r := range []Result[int]{<-a, <-b} {
			if r.Val != 5 || r.Err != nil || !r.Shared {
				t.Fatalf("result = %+v, want shared value 5", r)
			}
		}
		if syncVal != 5 {
			t.Fatalf("Do() joined DoChan call with %d, want 5", syncVal)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("fn called %d times, want 1", n)
		}
	})
}

func TestGroup_Do_panic(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g       Group[string, int]
			release = make(chan struct{})
			errBoom = errors.New("boom")
			wg      sync.WaitGroup
			panics  atomic.Int32
		)

		ch := g.DoChan("key", func() (int, error) {
			<-release
			panic(errBoom)
		})
		synctest.Wait()

		for range 3 {
			wg.Go(func() {
				defer func() {
					var pe *PanicError
					if err, ok := recover().(error); !ok || !errors.As(err, &pe) {
						t.Errorf("recovered %v, want *PanicError", err)
						return
					}
					if !errors.Is(pe, errBoom) || len(pe.Stack) == 0 {
						t.Errorf("PanicError = %v, want wrapped errBoom with stack", pe)
					}
					panics.Add(1)
				}()
				_, _ = g.Do("key", func() (int, error) { return 0, nil })
			})
		}
		synctest.Wait()
		close(release)
		wg.Wait()

		if n := panics.Load(); n != 3 {
			t.Fatalf("%d Do callers panicked, want 3", n)
		}
		var pe *PanicError
		if r := <-ch; !errors.As(r.Err, &pe) || !errors.Is(r.Err, errBoom) {
			t.Fatalf("DoChan result = %+v, want *PanicError", r)
		}

		// The group is still usable after a panic.
		if v, err := g.Do("key", func() (int, error) { return 9, nil }); v != 9 || err != nil {
			t.Fatalf("Do() after panic = (%d, %v)", v, err)
		}
	})
}

func TestGroup_Do_panicLeader(t *testing.T) {
	t.Parallel()

	var g Group[string, int]
	defer func() {
		pe, ok := recover().(*PanicError)
		if !ok || pe.Value != "oops" {
			t.Fatalf("recovered %v, want *PanicError with value %q", pe, "oops")
		}
	}()
	_, _ = g.Do("key", func() (int, error) { panic("oops") })
}

func TestGroup_Do_goexit(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g        Group[string, int]
			release  = make(chan struct{})
			returned atomic.Bool
		)

		leaderDone := make(chan struct{})
		go func() {
			defer close(leaderDone)
			_, _ = g.Do("key", func() (int, error) {
				<-release
				runtime.Goexit()
				return 0, nil
			})
			returned.Store(true)
		}()
		synctest.Wait()

		ch := g.DoChan("key", func() (int, error) { return 1, nil })
		var err error
		waiterDone := make(chan struct{})
		go func() {
			defer close(waiterDone)
			_, err = g.Do("key", func() (int, error) { return 1, nil })
		}()
		synctest.Wait()
		close(release)
		<-leaderDone
		<-waiterDone

		if returned.Load() {
			t.Fatal("leader returned from Do after runtime.Goexit")
		}
		if !errors.Is(err, ErrGoexit) {
			t.Fatalf("waiter error = %v, want ErrGoexit", err)
		}
		if r := <-ch; !errors.Is(r.Err, ErrGoexit) {
			t.Fatalf("DoChan error = %v, want ErrGoexit", r.Err)
		}
	})
}

type ctxKey struct{}

func TestGroup_DoContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g       Group[string, string]
			release = make(chan struct{})
		)

		ctx := context.WithValue(t.Context(), ctxKey{}, "leader")
		got, err := g.DoContext(ctx, "key", func(ctx context.Context) (string, error) {
			return ctx.Value(ctxKey{}).(string), nil
		})
		if got != "leader" || err != nil {
			t.Fatalf("DoContext() = (%q, %v), want leader's context value", got, err)
		}

		// A canceled context returns without running fn.
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = g.DoContext(canceled, "key", func(context.Context) (string, error) {
			t.Error("fn called with canceled context")
			return "", nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DoContext(canceled) error = %v", err)
		}

		// The leader giving up does not cancel the work for other callers.
		leaderCtx, leaderCancel := context.WithCancel(t.Context())
		var leaderErr error
		leaderDone := make(chan struct{})
		go func() {
			defer close(leaderDone)
			_, leaderErr = g.DoContext(leaderCtx, "shared", func(ctx context.Context) (string, error) {
				select {
				case <-release:
					return "done", nil
				case <-ctx.Done():
					return "", context.Cause(ctx)
				}
			})
		}()
		synctest.Wait()

		var follower string
		followerDone := make(chan struct{})
		go func() {
			defer close(followerDone)
			follower, _ = g.DoContext(t.Context(), "shared", nil)
		}()
		synctest.Wait()

		leaderCancel()
		<-leaderDone
		if !errors.Is(leaderErr, context.Canceled) {
			t.Fatalf("leader error = %v, want context.Canceled", leaderErr)
		}
		close(release)
		<-followerDone
		if follower != "done" {
			t.Fatalf("follower = %q, want %q", follower, "done")
		}
	})
}

func TestGroup_DoContext_abandoned(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g        Group[string, int]
			calls    atomic.Int32
			errLate  = errors.New("too late")
			fnCause  = make(chan error, 1)
			deadline = 50 * time.Millisecond
			wg       sync.WaitGroup
		)

		fn := func(ctx context.Context) (int, error) {
			calls.Add(1)
			<-ctx.Done()
			fnCause <- context.Cause(ctx)
			return 0, ctx.Err()
		}

		// Two callers with the same deadline; the second leaves last.
		for i := range 2 {
			ctx, cancel := context.WithTimeoutCause(t.Context(), deadline+time.Duration(i), errLate)
			t.Cleanup(cancel)
			wg.Go(func() {
				if _, err := g.DoContext(ctx, "key", fn); !errors.Is(err, errLate) {
					t.Errorf("DoContext() error = %v, want %v", err, errLate)
				}
			})
		}
		synctest.Wait()
		wg.Wait() // Both deadlines pass.

		if cause := <-fnCause; !errors.Is(cause, errLate) {
			t.Fatalf("fn context cause = %v, want %v", cause, errLate)
		}

		// The abandoned call was forgotten, so a new caller starts fresh.
		v, err := g.DoContext(t.Context(), "key", func(context.Context) (int, error) { return 3, nil })
		if v != 3 || err != nil {
			t.Fatalf("DoContext() after abandonment = (%d, %v)", v, err)
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("abandoned fn called %d times, want 1", n)
		}
	})
}

func TestGroup_DoContext_keptAliveByDo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var (
			g       Group[string, int]
			release = make(chan struct{})
		)

		ctx, cancel := context.WithCancel(t.Context())
		leaderDone := make(chan struct{})
		go func() {
			defer close(leaderDone)
			_, _ = g.DoContext(ctx, "key", func(ctx context.Context) (int, error) {
				select {
				case <-release:
					return 1, nil
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			})
		}()
		synctest.Wait()

		ch := g.DoChan("key", nil)
		cancel()
		<-leaderDone
		close(release)

		if r := <-ch; r.Val != 1 || r.Err != nil {
			t.Fatalf("DoChan result = %+v, want value 1 from uncanceled call", r)
		}
	})
}

func TestGroup_DoContext_panic(t *testing.T) {
	t.Parallel()

	var g Group[string, int]
	defer func() {
		if _, ok := recover().(*PanicError); !ok {
			t.Fatal("DoContext did not re-panic with *PanicError")
		}
	}()
	_, _ = g.DoContext(t.Context(), "key", func(context.Context) (int, error) { panic("oops") })
}

func BenchmarkGroup_Do(b *testing.B) {
	var g Group[int, int]
	fn := func() (int, error) { return 1, nil }
	b.RunParallel(func(pb *testing.PB) {
		key := 0
		for pb.Next() {
			key++
			_, _ = g.Do(key%64, fn)
		}
	})
}
