// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestResultGroup_Go(t *testing.T) {
	t.Parallel()

	rg := NewResultGroup[int]()
	for i := range 5 {
		rg.Go(func() int { return i })
	}

	results := rg.Wait()
	if len(results) != 5 {
		t.Fatalf("len = %d, want 5", len(results))
	}
	for i, v := range results {
		if v != i {
			t.Errorf("results[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestResultGroup_orderedResults(t *testing.T) {
	t.Parallel()

	rg := NewResultGroup[int]().WithMaxGoroutines(2)
	for i := range 10 {
		rg.Go(func() int {
			time.Sleep(time.Duration(10-i) * time.Millisecond)
			return i
		})
	}

	results := rg.Wait()
	for i, v := range results {
		if v != i {
			t.Errorf("results[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestResultGroup_Wait_empty(t *testing.T) {
	t.Parallel()

	results := NewResultGroup[int]().Wait()
	if results != nil {
		t.Fatalf("Wait() = %v, want nil", results)
	}
}

func TestResultGroup_WithMaxGoroutines(t *testing.T) {
	t.Parallel()

	const limit = 3
	rg := NewResultGroup[int]().WithMaxGoroutines(limit)

	var peak atomic.Int32
	for i := range 20 {
		rg.Go(func() int {
			n := peak.Add(1)
			if n > limit {
				t.Errorf("concurrent = %d, exceeds limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
			return i
		})
	}
	results := rg.Wait()
	if len(results) != 20 {
		t.Fatalf("len = %d, want 20", len(results))
	}
}

func TestResultGroup_MaxGoroutines(t *testing.T) {
	t.Parallel()

	rg := NewResultGroup[int]()
	if rg.MaxGoroutines() != 0 {
		t.Fatalf("want 0, got %d", rg.MaxGoroutines())
	}
	rg.WithMaxGoroutines(5)
	if rg.MaxGoroutines() != 5 {
		t.Fatalf("want 5, got %d", rg.MaxGoroutines())
	}
}

func TestResultGroup_Panic(t *testing.T) {
	t.Parallel()

	rg := NewResultGroup[int]()
	rg.Go(func() int { return 1 })
	rg.Go(func() int { panic("boom") })

	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want 'boom'", r)
		}
	}()
	_ = rg.Wait()
	t.Fatal("Wait should have panicked")
}

func TestResultGroup_Reuse(t *testing.T) {
	t.Parallel()

	rg := NewResultGroup[int]()

	rg.Go(func() int { return 1 })
	rg.Go(func() int { return 2 })
	r1 := rg.Wait()
	if len(r1) != 2 || r1[0] != 1 || r1[1] != 2 {
		t.Fatalf("round 1 = %v, want [1 2]", r1)
	}

	rg.Go(func() int { return 3 })
	r2 := rg.Wait()
	if len(r2) != 1 || r2[0] != 3 {
		t.Fatalf("round 2 = %v, want [3]", r2)
	}
}

func TestResultContextGroup_Go(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background())
	for i := range 5 {
		rcg.Go(func(_ context.Context) (int, error) { return i, nil })
	}

	results, err := rcg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("len = %d, want 5", len(results))
	}
	for i, v := range results {
		if v != i {
			t.Errorf("results[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestResultContextGroup_orderedResults(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background()).WithMaxGoroutines(2)
	for i := range 10 {
		rcg.Go(func(_ context.Context) (int, error) {
			time.Sleep(time.Duration(10-i) * time.Millisecond)
			return i, nil
		})
	}

	results, err := rcg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range results {
		if v != i {
			t.Errorf("results[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestResultContextGroup_errorsExcluded(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background())
	rcg.Go(func(_ context.Context) (int, error) { return 1, nil })
	rcg.Go(func(_ context.Context) (int, error) { return 0, errors.New("fail") })
	rcg.Go(func(_ context.Context) (int, error) { return 3, nil })

	results, err := rcg.Wait()
	if err == nil {
		t.Fatal("expected error")
	}
	if len(results) != 2 || results[0] != 1 || results[1] != 3 {
		t.Fatalf("results = %v, want [1 3]", results)
	}
}

func TestResultContextGroup_WithCollectErrored(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background()).WithCollectErrored()
	rcg.Go(func(_ context.Context) (int, error) { return 1, nil })
	rcg.Go(func(_ context.Context) (int, error) { return 42, errors.New("fail") })
	rcg.Go(func(_ context.Context) (int, error) { return 3, nil })

	results, err := rcg.Wait()
	if err == nil {
		t.Fatal("expected error")
	}
	if len(results) != 3 || results[0] != 1 || results[1] != 42 || results[2] != 3 {
		t.Fatalf("results = %v, want [1 42 3]", results)
	}
}

func TestResultContextGroup_Wait_empty(t *testing.T) {
	t.Parallel()

	results, err := NewResultGroup[int]().WithContext(context.Background()).Wait()
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if results != nil {
		t.Fatalf("results = %v, want nil", results)
	}
}

func TestResultContextGroup_WithCancelOnError(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background()).WithCancelOnError()
	sentinel := errors.New("fail")

	rcg.Go(func(_ context.Context) (int, error) { return 0, sentinel })
	rcg.Go(func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	_, err := rcg.Wait()
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

func TestResultContextGroup_WithFailFast(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background()).WithFailFast()
	sentinel := errors.New("fail")

	rcg.Go(func(_ context.Context) (int, error) { return 0, sentinel })
	rcg.Go(func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	_, err := rcg.Wait()
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("WithFailFast should return only the first error")
	}
}

func TestResultContextGroup_WithMaxGoroutines(t *testing.T) {
	t.Parallel()

	const limit = 2
	rcg := NewResultGroup[int]().WithContext(context.Background()).WithMaxGoroutines(limit)

	var peak atomic.Int32
	for i := range 20 {
		rcg.Go(func(_ context.Context) (int, error) {
			n := peak.Add(1)
			if n > limit {
				return 0, fmt.Errorf("peak %d > limit %d", n, limit)
			}
			time.Sleep(time.Millisecond)
			peak.Add(-1)
			return i, nil
		})
	}

	results, err := rcg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 20 {
		t.Fatalf("len = %d, want 20", len(results))
	}
}

func TestResultContextGroup_Reuse(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background())

	rcg.Go(func(_ context.Context) (int, error) { return 1, nil })
	rcg.Go(func(_ context.Context) (int, error) { return 2, nil })
	r1, err := rcg.Wait()
	if err != nil {
		t.Fatalf("round 1 err: %v", err)
	}
	if len(r1) != 2 || r1[0] != 1 || r1[1] != 2 {
		t.Fatalf("round 1 = %v, want [1 2]", r1)
	}

	rcg.Go(func(_ context.Context) (int, error) { return 3, nil })
	r2, err := rcg.Wait()
	if err != nil {
		t.Fatalf("round 2 err: %v", err)
	}
	if len(r2) != 1 || r2[0] != 3 {
		t.Fatalf("round 2 = %v, want [3]", r2)
	}
}

func TestResultContextGroup_Reuse_afterError(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background())

	rcg.Go(func(_ context.Context) (int, error) { return 0, errors.New("fail") })
	_, err := rcg.Wait()
	if err == nil {
		t.Fatal("round 1: expected error")
	}

	rcg.Go(func(_ context.Context) (int, error) { return 42, nil })
	results, err := rcg.Wait()
	if err != nil {
		t.Fatalf("round 2 err: %v", err)
	}
	if len(results) != 1 || results[0] != 42 {
		t.Fatalf("round 2 = %v, want [42]", results)
	}
}

func TestResultContextGroup_Panic(t *testing.T) {
	t.Parallel()

	rcg := NewResultGroup[int]().WithContext(context.Background())
	rcg.Go(func(_ context.Context) (int, error) { return 1, nil })
	rcg.Go(func(_ context.Context) (int, error) { panic("boom") })

	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want 'boom'", r)
		}
	}()
	_, _ = rcg.Wait()
	t.Fatal("Wait should have panicked")
}
