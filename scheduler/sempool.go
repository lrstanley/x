// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

// SemaphorePool represents a go-routine worker pool. This does NOT manage the
// workers, only how many workers are running.
type SemaphorePool struct {
	total   int
	threads chan bool
	done    bool
}

// Slot is used to wait for an open slot to start processing.
func (p *SemaphorePool) Slot() {
	if p.done {
		panic("Slot() called in go-routine on completed pool")
	}

	p.threads <- true
}

// Free is used to free the slot taken by [SemaphorePool.Slot].
func (p *SemaphorePool) Free() {
	if p.done {
		panic("Free() called in go-routine on completed pool")
	}

	<-p.threads
}

// Go is a simplified version of [SemaphorePool.Slot] and [SemaphorePool.Free],
// to reduce the chance of developer error (forgetting to call [SemaphorePool.Free]).
func (p *SemaphorePool) Go(fn func()) {
	p.Slot()
	defer p.Free()
	fn()
}

// Wait is used to wait for all open Slot()'s to be Free()'d.
func (p *SemaphorePool) Wait() {
	if p.done {
		panic("Wait() called on completed pool")
	}

	for range cap(p.threads) {
		p.threads <- true
	}

	p.done = true
}

// WaitChan returns a channel that can be used to wait for a response to a
// channel.
func (p *SemaphorePool) WaitChan() chan struct{} {
	notify := make(chan struct{}, 1)

	go func() {
		p.Wait()

		notify <- struct{}{}
	}()

	return notify
}

// NewSemaphorePool returns a new [SemaphorePool].
func NewSemaphorePool(count int) *SemaphorePool {
	if count < 1 {
		count = 1
	}
	return &SemaphorePool{total: count, threads: make(chan bool, count)}
}
