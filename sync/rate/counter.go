// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rate

import (
	"hash/fnv"
	"sync"
	"time"
)

var _ WindowCounter = (*LocalCounter)(nil)

// LocalCounter is an in-memory [WindowCounter] backed by two maps that track
// the current and previous time windows. Keys are hashed to uint64 using FNV-1
// to reduce per-entry memory. Stale windows are evicted on every write using the
// same double-map swap strategy as httprate.
//
// All methods are safe for concurrent use and are guaranteed to return nil
// errors.
type LocalCounter struct {
	windowLength     time.Duration
	latestWindow     time.Time
	latestCounters   map[uint64]int
	previousCounters map[uint64]int
	mu               sync.RWMutex
}

// NewLocalCounter returns a [LocalCounter] sized for windowLength. It panics if
// windowLength is zero or negative.
func NewLocalCounter(windowLength time.Duration) *LocalCounter {
	if windowLength <= 0 {
		panic("rate: NewLocalCounter: windowLength must be positive")
	}
	return &LocalCounter{
		windowLength:     windowLength,
		latestWindow:     time.Now().UTC().Truncate(windowLength),
		latestCounters:   make(map[uint64]int),
		previousCounters: make(map[uint64]int),
	}
}

// Increment adds 1 to the counter for key in currentWindow.
func (c *LocalCounter) Increment(key string, currentWindow time.Time) error {
	return c.IncrementBy(key, currentWindow, 1)
}

// IncrementBy adds amount to the counter for key in currentWindow. Stale
// windows are evicted before the counter is updated.
func (c *LocalCounter) IncrementBy(key string, currentWindow time.Time, amount int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evict(currentWindow)

	hk := hashKey(key)
	c.latestCounters[hk] += amount
	return nil
}

// Get returns the counts for key in the current and previous windows. The
// caller is expected to pass window-aligned timestamps (truncated to
// windowLength).
func (c *LocalCounter) Get(key string, currentWindow, previousWindow time.Time) (curr, prev int, err error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hk := hashKey(key)

	if c.latestWindow.Equal(currentWindow) {
		return c.latestCounters[hk], c.previousCounters[hk], nil
	}

	if c.latestWindow.Equal(previousWindow) {
		return 0, c.latestCounters[hk], nil
	}

	return 0, 0, nil
}

func (c *LocalCounter) evict(currentWindow time.Time) {
	if c.latestWindow.Equal(currentWindow) {
		return
	}

	previousWindow := currentWindow.Add(-c.windowLength)
	if c.latestWindow.Equal(previousWindow) {
		c.latestWindow = currentWindow
		clear(c.previousCounters)
		c.latestCounters, c.previousCounters = c.previousCounters, c.latestCounters
		return
	}

	c.latestWindow = currentWindow
	clear(c.previousCounters)
	clear(c.latestCounters)
}

func hashKey(key string) uint64 {
	h := fnv.New64()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}
