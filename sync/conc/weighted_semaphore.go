// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"container/list"
	"context"
	"sync"
)

type weightedWaiter struct {
	n     int64
	ready chan<- struct{}
}

// WeightedSemaphore limits concurrent work by tracking weighted resource usage
// rather than simple slot counts. Callers request varying amounts of capacity,
// and the semaphore ensures the combined weight of all active holders never
// exceeds the configured maximum. See [NewWeightedSemaphore] for more details.
type WeightedSemaphore struct {
	mu      sync.Mutex
	size    int64
	cur     int64
	waiters list.List
	idle    *sync.Cond
}

// NewWeightedSemaphore returns a semaphore that allows at most n combined weight
// for concurrent holders. It panics if n is less than 1.
func NewWeightedSemaphore(n int64) *WeightedSemaphore {
	if n < 1 {
		panic("conc: NewWeightedSemaphore: n must be at least 1")
	}
	s := &WeightedSemaphore{size: n}
	s.idle = sync.NewCond(&s.mu)
	return s
}

// Alloc blocks until it can acquire n weight from the semaphore or ctx is done.
// On success, returns nil. On failure, returns ctx.Err() and leaves the semaphore
// unchanged.
//
// Waiters are served in FIFO order: a large request at the head of the queue
// will block smaller later requests to prevent starvation.
func (s *WeightedSemaphore) Alloc(ctx context.Context, n int64) error {
	done := ctx.Done()

	s.mu.Lock()
	select {
	case <-done:
		s.mu.Unlock()
		return ctx.Err()
	default:
	}

	if s.size-s.cur >= n && s.waiters.Len() == 0 {
		s.cur += n
		s.mu.Unlock()
		return nil
	}

	if n > s.size {
		s.mu.Unlock()
		<-done
		return ctx.Err()
	}

	ready := make(chan struct{})
	w := weightedWaiter{n: n, ready: ready}
	elem := s.waiters.PushBack(w)
	s.mu.Unlock()

	select {
	case <-done:
		s.mu.Lock()
		select {
		case <-ready:
			s.cur -= n
			s.notifyWaiters()
		default:
			isFront := s.waiters.Front() == elem
			s.waiters.Remove(elem)
			if isFront && s.size > s.cur {
				s.notifyWaiters()
			}
		}
		s.mu.Unlock()
		return ctx.Err()

	case <-ready:
		select {
		case <-done:
			s.Free(n)
			return ctx.Err()
		default:
		}
		return nil
	}
}

// TryAlloc attempts to acquire n weight without blocking. On success, returns
// true. On failure, returns false and leaves the semaphore unchanged.
func (s *WeightedSemaphore) TryAlloc(n int64) bool {
	s.mu.Lock()
	ok := s.size-s.cur >= n && s.waiters.Len() == 0
	if ok {
		s.cur += n
	}
	s.mu.Unlock()
	return ok
}

// Free releases n weight back to the semaphore. A caller must not free more
// weight than they have successfully acquired via [WeightedSemaphore.Alloc] or
// [WeightedSemaphore.TryAlloc].
func (s *WeightedSemaphore) Free(n int64) {
	s.mu.Lock()
	s.cur -= n
	if s.cur < 0 {
		s.mu.Unlock()
		panic("conc: WeightedSemaphore: freed more than held")
	}
	s.notifyWaiters()
	if s.cur == 0 {
		s.idle.Broadcast()
	}
	s.mu.Unlock()
}

// Wait blocks until all acquired weight has been returned (current usage is zero).
func (s *WeightedSemaphore) Wait() {
	s.mu.Lock()
	for s.cur != 0 {
		s.idle.Wait()
	}
	s.mu.Unlock()
}

// Go acquires n weight (honoring ctx), then runs f in a new goroutine. When f
// returns or terminates with [runtime.Goexit], the weight is released. If f
// panics, the panic value is re-raised and Free is not called, matching
// [sync.WaitGroup.Go].
//
// Returns ctx.Err() immediately if ctx is already done or canceled while
// waiting for capacity.
//
// In the terminology of [the Go memory model], the return from f
// "synchronizes before" the return of any Wait call that it unblocks.
//
// [the Go memory model]: https://go.dev/ref/mem
func (s *WeightedSemaphore) Go(ctx context.Context, n int64, f func()) error {
	if err := s.Alloc(ctx, n); err != nil {
		return err
	}
	go func() {
		defer func() {
			if x := recover(); x != nil {
				panic(x)
			}
			s.Free(n)
		}()
		f()
	}()
	return nil
}

func (s *WeightedSemaphore) notifyWaiters() {
	for {
		next := s.waiters.Front()
		if next == nil {
			break
		}

		w := next.Value.(weightedWaiter)
		if s.size-s.cur < w.n {
			// Not enough capacity for the next waiter. We leave all remaining
			// waiters blocked to prevent starvation of large requests.
			break
		}

		s.cur += w.n
		s.waiters.Remove(next)
		close(w.ready)
	}
}
