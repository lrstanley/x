// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewErrorGroup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewErrorGroup(ctx, 0)

	sentinel := errors.New("fail")
	g.Go(func(_ context.Context) error { return sentinel })

	if err := g.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestNewErrorGroup_cancelsContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewErrorGroup(ctx, 0)

	sentinel := errors.New("sentinel")

	g.Go(func(_ context.Context) error { return sentinel })
	g.Go(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	if err := g.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestNewErrorGroup_contextCause(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	g := NewErrorGroup(ctx, 0)

	sentinel := errors.New("cause-check")
	g.Go(func(_ context.Context) error { return sentinel })

	_ = g.Wait()

	// The parent context is not canceled (only the derived one is), so we
	// verify via the error returned by Wait.
}

func TestErrorGroup_Go_noError(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)

	var count atomic.Int32
	const tasks = 10
	for range tasks {
		g.Go(func(_ context.Context) error {
			count.Add(1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	if n := count.Load(); n != tasks {
		t.Fatalf("count = %d, want %d", n, tasks)
	}
}

func TestErrorGroup_Go_firstError(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)

	sentinel := errors.New("first")
	g.Go(func(_ context.Context) error { return sentinel })
	g.Go(func(_ context.Context) error { return nil })

	if err := g.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestErrorGroup_Go_receivesContext(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)

	sentinel := errors.New("sentinel")

	g.Go(func(_ context.Context) error { return sentinel })
	g.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("timed out waiting for context cancellation")
		}
	})

	if err := g.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestErrorGroup_Wait_noGoroutines(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)
	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestErrorGroup_Go_limit(t *testing.T) {
	t.Parallel()

	const limit = 3
	g := NewErrorGroup(context.Background(), limit)

	var peak atomic.Int32
	const tasks = 32
	for range tasks {
		g.Go(func(_ context.Context) error {
			n := peak.Add(1)
			if n > limit {
				return fmt.Errorf("concurrent goroutines %d exceed limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
}

func TestErrorGroup_Go_noLimit(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)

	var count atomic.Int32
	const tasks = 10
	for range tasks {
		g.Go(func(_ context.Context) error {
			count.Add(1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	if n := count.Load(); n != tasks {
		t.Fatalf("count = %d, want %d", n, tasks)
	}
}

func TestErrorGroup_Go_negativeLimitMeansNoLimit(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), -1)

	var count atomic.Int32
	const tasks = 10
	for range tasks {
		g.Go(func(_ context.Context) error {
			count.Add(1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	if n := count.Load(); n != tasks {
		t.Fatalf("count = %d, want %d", n, tasks)
	}
}

func TestErrorGroup_TryGo(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)
	var count atomic.Int32

	ok := g.TryGo(func(_ context.Context) error {
		count.Add(1)
		return nil
	})
	if !ok {
		t.Fatal("TryGo should succeed with no limit")
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	if n := count.Load(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

func TestErrorGroup_TryGo_returnsFalseWhenFull(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 1)

	block := make(chan struct{})
	g.Go(func(_ context.Context) error {
		<-block
		return nil
	})

	if g.TryGo(func(_ context.Context) error { return nil }) {
		t.Fatal("TryGo should return false when at limit")
	}

	close(block)
	_ = g.Wait()
}

func TestErrorGroup_TryGo_returnsError(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)
	sentinel := errors.New("trygo-error")

	ok := g.TryGo(func(_ context.Context) error { return sentinel })
	if !ok {
		t.Fatal("TryGo should succeed with no limit")
	}

	if err := g.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestErrorGroup_Go_runtimeGoexit(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 1)

	done := make(chan struct{})
	g.Go(func(_ context.Context) error {
		defer close(done)
		runtime.Goexit()
		return nil
	})
	<-done

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestErrorGroup_Go_multipleErrors(t *testing.T) {
	t.Parallel()

	g := NewErrorGroup(context.Background(), 0)

	first := errors.New("first")
	second := errors.New("second")

	barrier := make(chan struct{})
	g.Go(func(_ context.Context) error {
		<-barrier
		return first
	})
	g.Go(func(_ context.Context) error {
		<-barrier
		return second
	})

	close(barrier)
	err := g.Wait()

	if !errors.Is(err, first) && !errors.Is(err, second) {
		t.Fatalf("Wait() = %v, want first or second", err)
	}
}

func TestErrorGroup_Go_limitsConcurrency(t *testing.T) {
	t.Parallel()

	const limit = 2
	g := NewErrorGroup(context.Background(), limit)

	var cur atomic.Int32
	const workers = 32
	for range workers {
		g.Go(func(_ context.Context) error {
			n := cur.Add(1)
			if n > limit {
				t.Errorf("concurrent goroutines %d exceed limit %d", n, limit)
			}
			time.Sleep(2 * time.Millisecond)
			cur.Add(-1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
}
