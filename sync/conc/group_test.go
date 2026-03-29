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

func TestGroup_Go(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	var count atomic.Int32
	for range 10 {
		g.Go(func() { count.Add(1) })
	}
	g.Wait()

	if n := count.Load(); n != 10 {
		t.Fatalf("count = %d, want 10", n)
	}
}

func TestGroup_Wait_empty(t *testing.T) {
	t.Parallel()
	NewGroup().Wait()
}

func TestGroup_WithMaxGoroutines(t *testing.T) {
	t.Parallel()

	const limit = 3
	g := NewGroup().WithMaxGoroutines(limit)

	var peak atomic.Int32
	for range 32 {
		g.Go(func() {
			n := peak.Add(1)
			if n > limit {
				t.Errorf("concurrent = %d, exceeds limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
		})
	}
	g.Wait()
}

func TestGroup_WithMaxGoroutines_panicsIfLessThan1(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for n < 1")
		}
	}()
	NewGroup().WithMaxGoroutines(0)
}

func TestGroup_MaxGoroutines(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	if g.MaxGoroutines() != 0 {
		t.Fatalf("want 0, got %d", g.MaxGoroutines())
	}
	g.WithMaxGoroutines(5)
	if g.MaxGoroutines() != 5 {
		t.Fatalf("want 5, got %d", g.MaxGoroutines())
	}
}

func TestGroup_Panic(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	g.Go(func() { panic("boom") })

	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want 'boom'", r)
		}
	}()
	g.Wait()
	t.Fatal("Wait should have panicked")
}

func TestGroup_Panic_preservesFirstValue(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	barrier := make(chan struct{})
	g.Go(func() { <-barrier; panic("first") })
	g.Go(func() { <-barrier; panic("second") })
	close(barrier)

	defer func() {
		r := recover()
		if r != "first" && r != "second" {
			t.Fatalf("recover = %v, want 'first' or 'second'", r)
		}
	}()
	g.Wait()
	t.Fatal("Wait should have panicked")
}

func TestGroup_Goexit(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	done := make(chan struct{})
	g.Go(func() {
		defer close(done)
		runtime.Goexit()
	})
	<-done
	g.Wait()
}

func TestGroup_Reuse(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	var count atomic.Int32

	for range 5 {
		g.Go(func() { count.Add(1) })
	}
	g.Wait()
	if n := count.Load(); n != 5 {
		t.Fatalf("round 1: count = %d, want 5", n)
	}

	for range 3 {
		g.Go(func() { count.Add(1) })
	}
	g.Wait()
	if n := count.Load(); n != 8 {
		t.Fatalf("round 2: count = %d, want 8", n)
	}
}

func TestGroup_Reuse_withLimit(t *testing.T) {
	t.Parallel()

	const limit = 2
	g := NewGroup().WithMaxGoroutines(limit)

	var peak atomic.Int32
	for range 10 {
		g.Go(func() {
			n := peak.Add(1)
			if n > limit {
				t.Errorf("concurrent = %d, exceeds limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
		})
	}
	g.Wait()

	var count atomic.Int32
	for range 5 {
		g.Go(func() { count.Add(1) })
	}
	g.Wait()
	if n := count.Load(); n != 5 {
		t.Fatalf("round 2: count = %d, want 5", n)
	}
}

func TestGroup_PanicIfStarted(t *testing.T) {
	t.Parallel()

	g := NewGroup()
	g.Go(func() {})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on reconfigure after Go()")
		}
	}()
	g.WithMaxGoroutines(5)
}

func TestErrorGroup_Go_noError(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors()
	var count atomic.Int32
	for range 10 {
		eg.Go(func() error { count.Add(1); return nil })
	}
	if err := eg.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
	if n := count.Load(); n != 10 {
		t.Fatalf("count = %d, want 10", n)
	}
}

func TestErrorGroup_Go_singleError(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors()
	sentinel := errors.New("fail")
	eg.Go(func() error { return sentinel })

	if err := eg.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestErrorGroup_Go_multipleErrors(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors()
	e1 := errors.New("first")
	e2 := errors.New("second")
	eg.Go(func() error { return e1 })
	eg.Go(func() error { return e2 })

	err := eg.Wait()
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("Wait() = %v, want both first and second via errors.Join", err)
	}
}

func TestErrorGroup_WithFirstError(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors().WithFirstError()

	barrier := make(chan struct{})
	e1 := errors.New("first")
	e2 := errors.New("second")
	eg.Go(func() error { <-barrier; return e1 })
	eg.Go(func() error { <-barrier; return e2 })
	close(barrier)

	err := eg.Wait()
	if !errors.Is(err, e1) && !errors.Is(err, e2) {
		t.Fatalf("Wait() = %v, want e1 or e2", err)
	}
	// Should be a single error, not a joined error.
	if errors.Is(err, e1) && errors.Is(err, e2) {
		t.Fatalf("WithFirstError should return only one error, got both")
	}
}

func TestErrorGroup_Wait_empty(t *testing.T) {
	t.Parallel()

	if err := NewGroup().WithErrors().Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestErrorGroup_WithMaxGoroutines(t *testing.T) {
	t.Parallel()

	const limit = 2
	eg := NewGroup().WithErrors().WithMaxGoroutines(limit)

	var peak atomic.Int32
	for range 20 {
		eg.Go(func() error {
			n := peak.Add(1)
			if n > limit {
				return fmt.Errorf("concurrent goroutines %d exceed limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestErrorGroup_Panic(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors()
	eg.Go(func() error { panic("boom") })

	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want 'boom'", r)
		}
	}()
	_ = eg.Wait()
	t.Fatal("Wait should have panicked")
}

func TestErrorGroup_Reuse(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors()

	sentinel := errors.New("round1")
	eg.Go(func() error { return sentinel })
	if err := eg.Wait(); !errors.Is(err, sentinel) {
		t.Fatalf("round 1: %v, want %v", err, sentinel)
	}

	eg.Go(func() error { return nil })
	if err := eg.Wait(); err != nil {
		t.Fatalf("round 2: %v, want nil", err)
	}
}

func TestErrorGroup_Reuse_independentErrors(t *testing.T) {
	t.Parallel()

	eg := NewGroup().WithErrors()

	first := errors.New("first")
	second := errors.New("second")

	eg.Go(func() error { return first })
	if err := eg.Wait(); !errors.Is(err, first) {
		t.Fatalf("round 1: %v, want %v", err, first)
	}

	eg.Go(func() error { return second })
	if err := eg.Wait(); !errors.Is(err, second) {
		t.Fatalf("round 2: %v, want %v", err, second)
	}

	eg.Go(func() error { return nil })
	if err := eg.Wait(); err != nil {
		t.Fatalf("round 3: %v, want nil", err)
	}
}

func TestContextGroup_Go(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background())
	var count atomic.Int32

	for range 5 {
		cg.Go(func(ctx context.Context) error {
			if ctx == nil {
				return errors.New("nil context")
			}
			count.Add(1)
			return nil
		})
	}
	if err := cg.Wait(); err != nil {
		t.Fatal(err)
	}
	if n := count.Load(); n != 5 {
		t.Fatalf("count = %d, want 5", n)
	}
}

func TestContextGroup_Go_noError(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background())
	cg.Go(func(_ context.Context) error { return nil })
	if err := cg.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestContextGroup_WithCancelOnError(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background()).WithCancelOnError()

	sentinel := errors.New("fail")
	cg.Go(func(_ context.Context) error { return sentinel })
	cg.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errors.New("timed out waiting for cancellation")
		}
	})

	err := cg.Wait()
	if !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
}

func TestContextGroup_WithFailFast(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background()).WithFailFast()

	sentinel := errors.New("fail")
	cg.Go(func(_ context.Context) error { return sentinel })
	cg.Go(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	err := cg.Wait()
	if !errors.Is(err, sentinel) {
		t.Fatalf("Wait() = %v, want %v", err, sentinel)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("WithFailFast should return only the first error, not context.Canceled")
	}
}

func TestContextGroup_Wait_empty(t *testing.T) {
	t.Parallel()

	if err := NewGroup().WithContext(context.Background()).Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestContextGroup_WithMaxGoroutines(t *testing.T) {
	t.Parallel()

	const limit = 2
	cg := NewGroup().WithContext(context.Background()).WithMaxGoroutines(limit)

	var peak atomic.Int32
	for range 20 {
		cg.Go(func(_ context.Context) error {
			n := peak.Add(1)
			if n > limit {
				return fmt.Errorf("peak %d > limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
			return nil
		})
	}
	if err := cg.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestContextGroup_Reuse(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background()).WithCancelOnError()

	cg.Go(func(_ context.Context) error { return errors.New("fail") })
	_ = cg.Wait()

	ctxErr := make(chan error, 1)
	cg.Go(func(ctx context.Context) error {
		ctxErr <- ctx.Err()
		return nil
	})
	_ = cg.Wait()

	if err := <-ctxErr; err != nil {
		t.Fatalf("context was already canceled after reuse: %v", err)
	}
}

func TestContextGroup_Reuse_independentErrors(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background())

	first := errors.New("first")
	second := errors.New("second")

	cg.Go(func(_ context.Context) error { return first })
	if err := cg.Wait(); !errors.Is(err, first) {
		t.Fatalf("round 1: %v, want %v", err, first)
	}

	cg.Go(func(_ context.Context) error { return second })
	if err := cg.Wait(); !errors.Is(err, second) {
		t.Fatalf("round 2: %v, want %v", err, second)
	}

	cg.Go(func(_ context.Context) error { return nil })
	if err := cg.Wait(); err != nil {
		t.Fatalf("round 3: %v, want nil", err)
	}
}

func TestContextGroup_Panic_cancelOnError(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background()).WithCancelOnError()

	ctxCanceled := make(chan bool, 1)
	cg.Go(func(_ context.Context) error { panic("boom") })
	cg.Go(func(ctx context.Context) error {
		<-ctx.Done()
		ctxCanceled <- true
		return nil
	})

	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want 'boom'", r)
		}
		select {
		case <-ctxCanceled:
		case <-time.After(time.Second):
			t.Fatal("context was not canceled after panic")
		}
	}()
	_ = cg.Wait()
	t.Fatal("Wait should have panicked")
}

func TestContextGroup_Panic_noCancelOnError(t *testing.T) {
	t.Parallel()

	cg := NewGroup().WithContext(context.Background())

	cg.Go(func(_ context.Context) error { panic("boom") })
	cg.Go(func(_ context.Context) error { return nil })

	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want 'boom'", r)
		}
	}()
	_ = cg.Wait()
	t.Fatal("Wait should have panicked")
}

func TestContextGroup_parentCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cg := NewGroup().WithContext(ctx)

	cancel()

	cg.Go(func(ctx context.Context) error {
		return ctx.Err()
	})

	err := cg.Wait()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() = %v, want context.Canceled", err)
	}
}
