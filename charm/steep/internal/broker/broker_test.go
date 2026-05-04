// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package broker

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrokerLastReceived(t *testing.T) {
	t.Parallel()

	b := New[int]()

	if !b.LastReceived().IsZero() {
		t.Fatalf("wanted zero LastReceived before any publish, got %v", b.LastReceived())
	}

	before := time.Now()
	b.Publish(1)
	after := time.Now()

	got := b.LastReceived()
	if got.Before(before) || got.After(after) {
		t.Fatalf("LastReceived outside publish window: %v not in [%v, %v]", got, before, after)
	}

	b.Publish(2)
	second := b.LastReceived()
	if second.Before(got) {
		t.Fatalf("LastReceived regressed: %v before %v", second, got)
	}
}

func TestBrokerSubscribeOnlyLive(t *testing.T) {
	t.Parallel()

	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())

	b.Publish(-1)

	ch := make(chan int, 8)
	done := make(chan struct{})
	live := b.Subscribe(ctx)
	go func() {
		defer close(done)
		for v := range live {
			ch <- v
		}
	}()

	b.Publish(1)
	b.Publish(2)
	cancel()
	<-done

	got := drain(t, ch, 150*time.Millisecond)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("wanted [1 2], got %+v", got)
	}
}

func TestBrokerHistorySeq(t *testing.T) {
	t.Parallel()

	b := New[int](WithMaxHistory(5))
	for i := range 3 {
		b.Publish(i)
	}

	var got []int
	for v := range b.History() {
		got = append(got, v)
	}
	if !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("History = %v, want [0 1 2]", got)
	}
}

func TestBrokerSubscribeReceived(t *testing.T) {
	t.Parallel()

	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())

	// Registration completes before [Seq2] iteration starts; capture the iterator
	// before publishing so events are not missed.
	seq := b.SubscribeReceived(ctx)

	type pair struct {
		at time.Time
		v  int
	}
	var pairs []pair
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		for at, v := range seq {
			mu.Lock()
			pairs = append(pairs, pair{at, v})
			mu.Unlock()
		}
	}()

	b.Publish(1)
	b.Publish(2)
	cancel()
	<-done

	mu.Lock()
	got := slices.Clone(pairs)
	mu.Unlock()

	if len(got) != 2 || got[0].v != 1 || got[1].v != 2 {
		t.Fatalf("wanted values [1 2], got %+v", got)
	}
	if got[1].at.Before(got[0].at) {
		t.Fatalf("received timestamps out of order: %v before %v", got[1].at, got[0].at)
	}
}

func TestBrokerSubscribeAllReceived(t *testing.T) {
	t.Parallel()

	b := New[int]()
	b.Publish(1)

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var buf []int
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, v := range b.SubscribeAllReceived(ctx) {
			mu.Lock()
			buf = append(buf, v)
			mu.Unlock()
		}
	}()

	b.Publish(2)

	waitUntil(t, func() bool {
		mu.Lock()
		n := len(buf)
		mu.Unlock()
		return n >= 2
	}, 2*time.Second)
	cancel()
	<-done

	mu.Lock()
	final := slices.Clone(buf)
	mu.Unlock()

	if !slices.Equal(final, []int{1, 2}) {
		t.Fatalf("SubscribeAllReceived = %v, want [1 2]", final)
	}
}

func TestBrokerHistoryTrim(t *testing.T) {
	t.Parallel()

	b := New[int](WithMaxHistory(3))

	for i := range 10 {
		b.Publish(i)
	}

	var h []int
	for _, v := range b.HistoryWithTime() {
		h = append(h, v)
	}
	if len(h) != 3 || h[0] != 7 || h[1] != 8 || h[2] != 9 {
		t.Fatalf("unexpected history: %+v", h)
	}
}

func TestBrokerNegativeMaxHistoryIsUnbounded(t *testing.T) {
	t.Parallel()

	b := New[int](WithMaxHistory(-1))

	for i := range DefaultMaxHistory + 1 {
		b.Publish(i)
	}

	var h []int
	for _, v := range b.HistoryWithTime() {
		h = append(h, v)
	}
	if len(h) != DefaultMaxHistory+1 {
		t.Fatalf("unexpected history length: got %d want %d", len(h), DefaultMaxHistory+1)
	}
}

func TestBrokerReplayThenLiveOrdering(t *testing.T) {
	t.Parallel()

	b := New[int](WithSubscriberBuffer(32))
	ctx, cancel := context.WithCancel(context.Background())

	b.Publish(1)
	b.Publish(2)

	var buf []int
	var mu sync.Mutex
	appendMsg := func(v int) {
		mu.Lock()
		buf = append(buf, v)
		mu.Unlock()
	}

	done := make(chan struct{})
	replay := b.SubscribeAll(ctx)
	go func() {
		defer close(done)
		for v := range replay {
			appendMsg(v)
		}
	}()

	b.Publish(3)

	waitUntil(t, func() bool {
		mu.Lock()
		ok := len(buf) == 3
		mu.Unlock()
		return ok
	}, 2*time.Second)
	cancel()
	<-done

	mu.Lock()
	got := append([]int(nil), buf...)
	mu.Unlock()

	if len(got) != 3 {
		t.Fatalf("wanted 3 events, got %d: %+v", len(got), got)
	}
	if got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("want order 1,2,3 got %+v", got)
	}
}

func TestBrokerCancelUnregister(t *testing.T) {
	t.Parallel()

	b := New[int]()
	ctx, cancel := context.WithCancel(context.Background())

	var n atomic.Int32
	done := make(chan struct{})
	live := b.Subscribe(ctx)
	go func() {
		defer close(done)
		for range live {
			n.Add(1)
		}
	}()

	b.Publish(1)
	cancel()
	<-done

	time.Sleep(30 * time.Millisecond)
	b.Publish(2)

	if n.Load() != 1 {
		t.Fatalf("wanted one delivery before cancel, got %d", n.Load())
	}
}

func TestBrokerPublishNeverBlocksOnSlowSubscriber(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	b := New[int](WithSubscriberBuffer(2))

	done := make(chan struct{})
	live := b.Subscribe(ctx)
	go func() {
		defer close(done)
		for v := range live {
			time.Sleep(2 * time.Millisecond)
			_ = v
		}
	}()

	pubDone := make(chan struct{})
	go func() {
		for range 400 {
			b.Publish(0)
		}
		close(pubDone)
	}()

	select {
	case <-pubDone:
	case <-time.After(3 * time.Second):
		t.Fatal("publish stalled")
	}
	cancel()
	<-done
}

// waitUntil polls pred until it returns true or d elapses.
func waitUntil(t *testing.T, pred func() bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

// drain reads values from ch until no value arrives before d elapses.
func drain(t *testing.T, ch <-chan int, d time.Duration) []int {
	t.Helper()
	deadline := time.After(d)
	var out []int
	for {
		select {
		case v := <-ch:
			out = append(out, v)
		case <-deadline:
			return out
		}
	}
}
