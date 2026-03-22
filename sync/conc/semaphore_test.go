// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewSemaphore_panicsOnBadCap(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for n < 1")
		}
	}()
	_ = NewSemaphore(0)
}

func TestSemaphore_Alloc_Free(t *testing.T) {
	t.Parallel()

	s := NewSemaphore(2)
	s.Alloc()
	s.Free()
}

func TestSemaphore_Go(t *testing.T) {
	t.Parallel()

	s := NewSemaphore(2)
	var wg sync.WaitGroup
	const tasks = 16
	wg.Add(tasks)
	for range tasks {
		s.Go(func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
		})
	}
	wg.Wait()
	s.Wait()
}

func TestSemaphore_Go_limitsConcurrentHolders(t *testing.T) {
	t.Parallel()

	const maxWorkers = 3
	s := NewSemaphore(maxWorkers)

	var cur atomic.Int32
	var wg sync.WaitGroup
	const workers = 32
	wg.Add(workers)
	for range workers {
		go func() {
			s.Go(func() {
				defer wg.Done()
				n := cur.Add(1)
				if n > maxWorkers {
					t.Errorf("holders %d exceed max %d", n, maxWorkers)
				}
				time.Sleep(2 * time.Millisecond)
				cur.Add(-1)
			})
		}()
	}
	wg.Wait()
}

func TestSemaphore_Go_runtimeGoexit(t *testing.T) {
	t.Parallel()

	s := NewSemaphore(1)
	var wg sync.WaitGroup
	wg.Add(1)
	s.Go(func() {
		defer wg.Done()
		runtime.Goexit()
	})
	wg.Wait()
	s.Wait()
}

func TestSemaphore_Go_blocksWhenSlotsExhausted(t *testing.T) {
	t.Parallel()

	s := NewSemaphore(1)
	s.Alloc()

	blocked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(blocked)
		s.Go(func() { close(done) })
	}()
	<-blocked

	select {
	case <-done:
		t.Fatal("Go should block on Alloc until a slot is free")
	case <-time.After(200 * time.Millisecond):
	}

	s.Free()
	<-done
}

func TestSemaphore_Wait_idle(t *testing.T) {
	t.Parallel()

	s := NewSemaphore(1)
	acquired := make(chan struct{})
	go func() {
		s.Alloc()
		close(acquired)
		time.Sleep(50 * time.Millisecond)
		s.Free()
	}()
	<-acquired
	s.Wait()
}

func TestSemaphore_limitsConcurrentHolders(t *testing.T) {
	t.Parallel()

	const maxWorkers = 3
	s := NewSemaphore(maxWorkers)

	var cur atomic.Int32
	var wg sync.WaitGroup
	const workers = 32
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			s.Alloc()
			n := cur.Add(1)
			if n > maxWorkers {
				t.Errorf("holders %d exceed max %d", n, maxWorkers)
			}
			time.Sleep(2 * time.Millisecond)
			cur.Add(-1)
			s.Free()
		}()
	}
	wg.Wait()
}
