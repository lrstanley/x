// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rate

import (
	"testing"
	"time"
)

func TestNewLocalCounter_panicsOnBadWindow(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for non-positive windowLength")
		}
	}()
	_ = NewLocalCounter(0)
}

func TestLocalCounter_IncrementAndGet(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	now := time.Now().UTC().Truncate(window)

	if err := c.Increment("a", now); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if err := c.IncrementBy("a", now, 4); err != nil {
		t.Fatalf("IncrementBy: %v", err)
	}

	curr, prev, err := c.Get("a", now, now.Add(-window))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if curr != 5 {
		t.Fatalf("expected curr=5, got %d", curr)
	}
	if prev != 0 {
		t.Fatalf("expected prev=0, got %d", prev)
	}
}

func TestLocalCounter_Get_unknownKey(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	now := time.Now().UTC().Truncate(window)

	curr, prev, err := c.Get("nonexistent", now, now.Add(-window))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if curr != 0 || prev != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", curr, prev)
	}
}

func TestLocalCounter_Evict_oneWindowForward(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	w0 := time.Now().UTC().Truncate(window)

	if err := c.IncrementBy("k", w0, 10); err != nil {
		t.Fatalf("IncrementBy: %v", err)
	}

	w1 := w0.Add(window)
	if err := c.Increment("k", w1); err != nil {
		t.Fatalf("Increment in new window: %v", err)
	}

	curr, prev, err := c.Get("k", w1, w0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if curr != 1 {
		t.Fatalf("expected curr=1, got %d", curr)
	}
	if prev != 10 {
		t.Fatalf("expected prev=10, got %d", prev)
	}
}

func TestLocalCounter_Evict_twoWindowsForward(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	w0 := time.Now().UTC().Truncate(window)

	if err := c.IncrementBy("k", w0, 10); err != nil {
		t.Fatalf("IncrementBy: %v", err)
	}

	w2 := w0.Add(2 * window)
	if err := c.Increment("k", w2); err != nil {
		t.Fatalf("Increment: %v", err)
	}

	curr, prev, err := c.Get("k", w2, w2.Add(-window))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if curr != 1 {
		t.Fatalf("expected curr=1, got %d", curr)
	}
	if prev != 0 {
		t.Fatalf("expected prev=0 (old data evicted), got %d", prev)
	}
}

func TestLocalCounter_Evict_largeGap(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	w0 := time.Now().UTC().Truncate(window)

	_ = c.IncrementBy("k", w0, 50)

	wFar := w0.Add(10 * window)
	_ = c.Increment("k", wFar)

	curr, prev, _ := c.Get("k", wFar, wFar.Add(-window))
	if curr != 1 {
		t.Fatalf("expected curr=1, got %d", curr)
	}
	if prev != 0 {
		t.Fatalf("expected prev=0, got %d", prev)
	}
}

func TestLocalCounter_Get_staleLatestWindow(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	w0 := time.Now().UTC().Truncate(window)

	_ = c.IncrementBy("k", w0, 7)

	w1 := w0.Add(window)
	// Don't increment in w1 -- just read. latestWindow is still w0.
	curr, prev, err := c.Get("k", w1, w0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if curr != 0 {
		t.Fatalf("expected curr=0 (no writes in w1), got %d", curr)
	}
	if prev != 7 {
		t.Fatalf("expected prev=7 (w0 data as previous), got %d", prev)
	}
}

func TestLocalCounter_Get_veryStaleWindow(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	w0 := time.Now().UTC().Truncate(window)

	_ = c.IncrementBy("k", w0, 7)

	wFar := w0.Add(5 * window)
	curr, prev, _ := c.Get("k", wFar, wFar.Add(-window))
	if curr != 0 || prev != 0 {
		t.Fatalf("expected (0,0) for stale read, got (%d,%d)", curr, prev)
	}
}

func TestLocalCounter_MultipleKeys(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	now := time.Now().UTC().Truncate(window)

	_ = c.IncrementBy("alice", now, 3)
	_ = c.IncrementBy("bob", now, 7)

	ac, _, _ := c.Get("alice", now, now.Add(-window))
	bc, _, _ := c.Get("bob", now, now.Add(-window))
	if ac != 3 {
		t.Fatalf("alice: expected 3, got %d", ac)
	}
	if bc != 7 {
		t.Fatalf("bob: expected 7, got %d", bc)
	}
}

func TestLocalCounter_SwapReusesMaps(t *testing.T) {
	t.Parallel()

	window := time.Second
	c := NewLocalCounter(window)
	w0 := time.Now().UTC().Truncate(window)

	_ = c.IncrementBy("k", w0, 1)

	w1 := w0.Add(window)
	_ = c.Increment("k", w1)

	c.mu.RLock()
	latestLen := len(c.latestCounters)
	prevLen := len(c.previousCounters)
	c.mu.RUnlock()

	if latestLen != 1 || prevLen != 1 {
		t.Fatalf("expected both maps to have 1 entry, got latest=%d prev=%d", latestLen, prevLen)
	}
}
