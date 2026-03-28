// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rate

import (
	"context"
	"sync"
	"time"
)

// WindowCounter abstracts the storage backend for a sliding-window rate
// counter. Implementations must be safe for concurrent use. See
// [NewLocalCounter] for an in-memory implementation.
type WindowCounter interface {
	// Increment adds 1 to the counter for key in the given window.
	Increment(key string, currentWindow time.Time) error

	// IncrementBy adds amount to the counter for key in the given window.
	IncrementBy(key string, currentWindow time.Time, amount int) error

	// Get returns the counts for key in the current and previous windows.
	Get(key string, currentWindow, previousWindow time.Time) (curr int, prev int, err error)
}

// KeyWindowLimiter applies per-key rate limiting using the sliding window
// counter pattern. It counts events in discrete time windows and linearly
// interpolates the previous window's contribution to produce a smoothed rate
// estimate. Storage is provided by a [WindowCounter] implementation.
//
// All methods are safe for concurrent use.
type KeyWindowLimiter struct {
	requestLimit int
	windowLength time.Duration
	counter      WindowCounter
	mu           sync.Mutex
}

// NewKeyWindowLimiter returns a [KeyWindowLimiter] that permits at most limit
// requests per window for each distinct key. The counter argument supplies the
// storage backend; use [NewLocalCounter] for an in-memory backend.
//
// It panics if limit is less than 1, window is zero or negative, or counter is
// nil.
func NewKeyWindowLimiter(limit int, window time.Duration, counter WindowCounter) *KeyWindowLimiter {
	if limit < 1 {
		panic("rate: NewKeyWindowLimiter: limit must be at least 1")
	}
	if window <= 0 {
		panic("rate: NewKeyWindowLimiter: window must be positive")
	}
	if counter == nil {
		panic("rate: NewKeyWindowLimiter: counter must not be nil")
	}
	return &KeyWindowLimiter{
		requestLimit: limit,
		windowLength: window,
		counter:      counter,
	}
}

// Allow reports whether a single event for key is permitted under the current
// sliding-window rate. If allowed, the event is counted; otherwise the counter
// is not modified. The returned error comes from the [WindowCounter] backend.
func (l *KeyWindowLimiter) Allow(key string) (bool, error) {
	return l.AllowN(key, 1)
}

// AllowN reports whether n events for key are permitted under the current
// sliding-window rate. If allowed, the events are counted; otherwise the
// counter is not modified.
func (l *KeyWindowLimiter) AllowN(key string, n int) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	currentWindow := now.UTC().Truncate(l.windowLength)
	previousWindow := currentWindow.Add(-l.windowLength)

	curr, prev, err := l.counter.Get(key, currentWindow, previousWindow)
	if err != nil {
		return false, err
	}

	r := l.slidingRate(now, currentWindow, curr, prev)
	if r+float64(n) > float64(l.requestLimit) {
		return false, nil
	}

	if incErr := l.counter.IncrementBy(key, currentWindow, n); incErr != nil {
		return false, incErr
	}
	return true, nil
}

// Wait blocks until a single event for key is permitted or ctx is done.
func (l *KeyWindowLimiter) Wait(ctx context.Context, key string) error {
	return l.WaitN(ctx, key, 1)
}

// WaitN blocks until n events for key are permitted or ctx is done. On each
// iteration that is rate-limited, it computes a back-off delay based on the
// sliding-window state and sleeps accordingly.
func (l *KeyWindowLimiter) WaitN(ctx context.Context, key string, n int) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		allowed, err := l.AllowN(key, n)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}

		delay := l.retryDelay(key, n)

		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Status returns the current rate for key without counting an event. The
// boolean reports whether an additional event would be permitted.
func (l *KeyWindowLimiter) Status(key string) (allowed bool, currentRate float64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	currentWindow := now.UTC().Truncate(l.windowLength)
	previousWindow := currentWindow.Add(-l.windowLength)

	curr, prev, err := l.counter.Get(key, currentWindow, previousWindow)
	if err != nil {
		return false, 0, err
	}

	r := l.slidingRate(now, currentWindow, curr, prev)
	return r < float64(l.requestLimit), r, nil
}

func (l *KeyWindowLimiter) slidingRate(now, currentWindow time.Time, curr, prev int) float64 {
	elapsed := now.Sub(currentWindow)
	weight := float64(l.windowLength-elapsed) / float64(l.windowLength)
	return float64(prev)*weight + float64(curr)
}

// retryDelay estimates the shortest sleep before the rate for key drops below
// the limit. It reads the counter under the limiter's lock but does not
// increment.
func (l *KeyWindowLimiter) retryDelay(key string, n int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	currentWindow := now.UTC().Truncate(l.windowLength)
	previousWindow := currentWindow.Add(-l.windowLength)

	curr, prev, err := l.counter.Get(key, currentWindow, previousWindow)
	if err != nil {
		return l.windowLength
	}

	if prev == 0 {
		remaining := l.windowLength - now.Sub(currentWindow)
		if remaining <= 0 {
			remaining = time.Millisecond
		}
		return remaining
	}

	// Solve for dt: prev * ((windowLength - elapsed - dt) / windowLength) + curr + n <= limit
	// dt >= windowLength * (prev + curr + n - limit) / prev - elapsed  ... approximately
	elapsed := now.Sub(currentWindow)
	excess := float64(prev) + float64(curr) + float64(n) - float64(l.requestLimit)
	dt := time.Duration(excess/float64(prev)*float64(l.windowLength)) - elapsed
	if dt <= 0 {
		dt = time.Millisecond
	}
	return dt
}
