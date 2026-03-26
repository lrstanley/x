// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewWeightedSemaphore_panicsOnBadCap(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for n < 1")
		}
	}()
	_ = NewWeightedSemaphore(0)
}

func TestWeightedSemaphore_Alloc_Free(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(10)
	if err := s.Alloc(context.Background(), 5); err != nil {
		t.Fatalf("Alloc(5): %v", err)
	}
	s.Free(5)
}

func TestWeightedSemaphore_Alloc_canceledContext(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Alloc(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWeightedSemaphore_Alloc_exceedsSize(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := s.Alloc(ctx, 10); err == nil {
		t.Fatal("expected error when requesting more than total capacity")
	}
}

func TestWeightedSemaphore_Alloc_cancelWhileWaiting(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	if err := s.Alloc(context.Background(), 5); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := s.Alloc(ctx, 3); err == nil {
		t.Fatal("expected error when context times out while waiting")
	}

	s.Free(5)

	if err := s.Alloc(context.Background(), 3); err != nil {
		t.Fatalf("Alloc after Free: %v", err)
	}
	s.Free(3)
}

func TestWeightedSemaphore_TryAlloc(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(10)
	if !s.TryAlloc(5) {
		t.Fatal("TryAlloc(5) should succeed on empty semaphore")
	}
	if !s.TryAlloc(5) {
		t.Fatal("TryAlloc(5) should succeed with remaining capacity")
	}
	if s.TryAlloc(1) {
		t.Fatal("TryAlloc(1) should fail with no capacity remaining")
	}
	s.Free(10)
}

func TestWeightedSemaphore_TryAlloc_blockedByWaiters(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	if err := s.Alloc(context.Background(), 4); err != nil {
		t.Fatal(err)
	}

	// Start a waiter that needs 3 (won't fit with cur=4).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = s.Alloc(ctx, 3) }()
	time.Sleep(20 * time.Millisecond)

	// TryAlloc(1) should fail because there's a pending waiter.
	if s.TryAlloc(1) {
		t.Fatal("TryAlloc should fail when waiters are queued")
	}

	s.Free(4)
}

func TestWeightedSemaphore_Free_panicsOnOverrelease(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on over-release")
		}
	}()

	s := NewWeightedSemaphore(5)
	s.Free(1)
}

func TestWeightedSemaphore_Wait(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(10)
	if err := s.Alloc(context.Background(), 7); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		s.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Wait should block while weight is held")
	case <-time.After(50 * time.Millisecond):
	}

	s.Free(7)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait should return after all weight freed")
	}
}

func TestWeightedSemaphore_Wait_alreadyIdle(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	s.Wait()
}

func TestWeightedSemaphore_Go(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(10)
	var wg sync.WaitGroup
	const tasks = 16
	wg.Add(tasks)
	for range tasks {
		if err := s.Go(context.Background(), 2, func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
		}); err != nil {
			t.Fatalf("Go: %v", err)
		}
	}
	wg.Wait()
	s.Wait()
}

func TestWeightedSemaphore_Go_canceledContext(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Go(ctx, 1, func() {
		t.Fatal("f should not run when context is canceled")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWeightedSemaphore_Go_limitsWeight(t *testing.T) {
	t.Parallel()

	const maxWeight int64 = 6
	s := NewWeightedSemaphore(maxWeight)

	var cur atomic.Int64
	var wg sync.WaitGroup
	const workers = 32
	wg.Add(workers)
	for range workers {
		go func() {
			_ = s.Go(context.Background(), 2, func() {
				defer wg.Done()
				n := cur.Add(2)
				if n > maxWeight {
					t.Errorf("weight %d exceeds max %d", n, maxWeight)
				}
				time.Sleep(2 * time.Millisecond)
				cur.Add(-2)
			})
		}()
	}
	wg.Wait()
}

func TestWeightedSemaphore_Go_runtimeGoexit(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	var wg sync.WaitGroup
	wg.Add(1)
	if err := s.Go(context.Background(), 3, func() {
		defer wg.Done()
		runtime.Goexit()
	}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	s.Wait()
}

func TestWeightedSemaphore_Go_blocksWhenFull(t *testing.T) {
	t.Parallel()

	s := NewWeightedSemaphore(5)
	if err := s.Alloc(context.Background(), 5); err != nil {
		t.Fatal(err)
	}

	blocked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(blocked)
		_ = s.Go(context.Background(), 3, func() { close(done) })
	}()
	<-blocked

	select {
	case <-done:
		t.Fatal("Go should block on Alloc until weight is freed")
	case <-time.After(200 * time.Millisecond):
	}

	s.Free(5)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Go should proceed after weight freed")
	}
}

func TestWeightedSemaphore_FIFO(t *testing.T) {
	t.Parallel()

	// Capacity 2, hold 1 so there is 1 free slot. A requests 2, which exceeds
	// the 1 available, so it enters the wait queue. We detect this by polling
	// TryAlloc(1): it succeeds while no waiters exist (1 slot free) and fails
	// once A is queued (waiters.Len() > 0). This gives a scheduler-independent
	// synchronization point.
	//
	// Both A and B request the full capacity (2), so notifyWaiters can only
	// serve one at a time. A must complete and Free before B is woken,
	// guaranteeing deterministic ordering regardless of GOMAXPROCS.
	s := NewWeightedSemaphore(2)
	if err := s.Alloc(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	var order []string
	var mu sync.Mutex
	record := func(label string) {
		mu.Lock()
		order = append(order, label)
		mu.Unlock()
	}

	aDone := make(chan struct{})
	bDone := make(chan struct{})

	go func() {
		_ = s.Alloc(context.Background(), 2)
		record("A")
		s.Free(2)
		close(aDone)
	}()

	// Spin until A is in the wait queue. TryAlloc(1) succeeds while no
	// waiters exist (1 free slot); once A is queued it returns false.
	for s.TryAlloc(1) {
		s.Free(1)
		runtime.Gosched()
	}

	go func() {
		_ = s.Alloc(context.Background(), 2)
		record("B")
		s.Free(2)
		close(bDone)
	}()

	// Release the held slot. notifyWaiters serves A (needs 2, 2 available).
	// B cannot be served until A frees (needs 2, 0 available).
	s.Free(1)
	<-aDone
	<-bDone

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Fatalf("expected FIFO order [A B], got %v", order)
	}
}
