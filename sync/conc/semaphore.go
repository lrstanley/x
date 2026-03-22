// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import "sync"

// Semaphore limits concurrent work to a fixed number of logical slots using a
// counting semaphore. See [NewSemaphore] for more details.
type Semaphore struct {
	mu sync.Mutex

	tokens chan struct{}
	inUse  int
	idle   *sync.Cond
}

// NewSemaphore returns a semaphore that allows at most n concurrent holders.
// It panics if n is less than 1.
func NewSemaphore(n int) *Semaphore {
	if n < 1 {
		panic("conc: NewSemaphore: n must be at least 1")
	}
	s := &Semaphore{
		tokens: make(chan struct{}, n),
	}
	for range n {
		s.tokens <- struct{}{}
	}
	s.idle = sync.NewCond(&s.mu)
	return s
}

// Alloc blocks until it can take one slot from the pool.
func (s *Semaphore) Alloc() {
	<-s.tokens
	s.mu.Lock()
	s.inUse++
	s.mu.Unlock()
}

// Free returns one slot to the pool. A caller must not invoke Free more times than
// they have successfully taken slots via [Semaphore.Alloc].
func (s *Semaphore) Free() {
	s.mu.Lock()
	s.inUse--
	if s.inUse == 0 {
		s.idle.Broadcast()
	}
	s.mu.Unlock()
	s.tokens <- struct{}{}
}

// Wait blocks until every slot has been returned (inUse is zero).
func (s *Semaphore) Wait() {
	s.mu.Lock()
	for s.inUse != 0 {
		s.idle.Wait()
	}
	s.mu.Unlock()
}

// Go runs f in a new goroutine after acquiring a slot from the semaphore. When f
// returns or terminates with [runtime.Goexit], the slot is released. If f panics,
// the panic value is re-raised and Free is not called, matching [sync.WaitGroup.Go].
//
// In the terminology of [the Go memory model], the return from f
// "synchronizes before" the return of any Wait call that it unblocks.
//
// [the Go memory model]: https://go.dev/ref/mem
func (s *Semaphore) Go(f func()) {
	s.Alloc()
	go func() {
		defer func() {
			if x := recover(); x != nil {
				// f panicked, which will be fatal because this is a new goroutine.
				//
				// Calling [Semaphore.Free] will unblock Wait in another goroutine,
				// allowing it to race with the fatal panic and possibly even exit
				// the process (os.Exit(0)) before the panic completes.
				//
				// This is almost certainly undesirable, so instead avoid calling
				// [Semaphore.Free] and simply panic.
				panic(x)
			}

			// f completed normally, or abruptly using goexit. Either way, release
			// the slot.
			s.Free()
		}()
		f()
	}()
}
