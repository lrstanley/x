// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package broker provides a generic in-memory event broker. Publishers never
// wait for slow subscribers, which drop events when their buffer is full.
// History is retained up to a maximum size, and subscribers can replay it
// before live events.
package broker

import (
	"context"
	"iter"
	"sync"
	"time"
)

type entry[T any] struct {
	seq uint64
	at  time.Time
	val T
}

// Broker fans published events out to subscribers, and keeps history up to
// [WithMaxHistory], dropping the oldest entries.
type Broker[T any] struct {
	opts options

	mu           sync.RWMutex
	nextSeq      uint64
	history      []entry[T]
	lastReceived time.Time
	subs         map[uint64]*subState[T]
	nextSubID    uint64
}

func New[T any](opts ...Option) *Broker[T] {
	o := resolveOptions(opts)
	return &Broker[T]{
		opts:    o,
		history: make([]entry[T], 0, min(32, o.maxHistory)),
		subs:    make(map[uint64]*subState[T]),
	}
}

// Publish records v in history and sends it to each subscriber without
// blocking: a subscriber whose buffer is full misses it.
func (b *Broker[T]) Publish(v T) {
	b.mu.Lock()
	b.nextSeq++
	seq := b.nextSeq
	now := time.Now()
	e := entry[T]{seq: seq, at: now, val: v}
	b.lastReceived = now
	b.appendHistoryLocked(e)

	snap := make([]*subState[T], 0, len(b.subs))
	for _, s := range b.subs {
		snap = append(snap, s)
	}
	b.mu.Unlock()

	for _, s := range snap {
		if !s.accept(seq) {
			continue
		}
		select {
		case s.ch <- e:
		default: // Buffer full: drop rather than block Publish.
		}
	}
}

// appendHistoryLocked appends e to history, dropping the oldest entry when
// full. Caller must hold b.mu.
func (b *Broker[T]) appendHistoryLocked(e entry[T]) {
	if b.opts.maxHistory <= 0 {
		b.history = append(b.history, e)
		return
	}
	if len(b.history) >= b.opts.maxHistory {
		copy(b.history, b.history[1:])
		b.history[len(b.history)-1] = e
		return
	}
	b.history = append(b.history, e)
}

// HistoryWithTime is [Broker.History], with the time each event was
// published.
func (b *Broker[T]) HistoryWithTime() iter.Seq2[time.Time, T] {
	b.mu.RLock()
	snap := make([]entry[T], len(b.history))
	copy(snap, b.history)
	b.mu.RUnlock()

	return func(yield func(time.Time, T) bool) {
		for _, e := range snap {
			if !yield(e.at, e.val) {
				return
			}
		}
	}
}

// History yields a snapshot of stored events, oldest first, taken when it is
// called.
func (b *Broker[T]) History() iter.Seq[T] {
	b.mu.RLock()
	snap := make([]entry[T], len(b.history))
	copy(snap, b.history)
	b.mu.RUnlock()

	return func(yield func(T) bool) {
		for _, e := range snap {
			if !yield(e.val) {
				return
			}
		}
	}
}

// LastReceived returns when the last event was published, or the zero Time.
func (b *Broker[T]) LastReceived() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastReceived
}

// Subscribe is [Broker.SubscribeReceived] without the times.
func (b *Broker[T]) Subscribe(ctx context.Context) iter.Seq[T] {
	b.mu.Lock()
	liveMin := b.nextSeq + 1
	id, st := b.registerLocked(liveMin)
	b.mu.Unlock()

	return func(yield func(T) bool) {
		defer b.subCleanup(id, st)
		b.readLive(ctx, id, st, func(_ time.Time, v T) bool {
			return yield(v)
		})
	}
}

// SubscribeReceived yields the publish time and value of events published
// after it returns, even before iteration starts, until ctx is done or the
// loop stops.
func (b *Broker[T]) SubscribeReceived(ctx context.Context) iter.Seq2[time.Time, T] {
	b.mu.Lock()
	liveMin := b.nextSeq + 1
	id, st := b.registerLocked(liveMin)
	b.mu.Unlock()

	return func(yield func(time.Time, T) bool) {
		defer b.subCleanup(id, st)
		b.readLive(ctx, id, st, yield)
	}
}

// SubscribeAll is [Broker.SubscribeAllReceived] without the times.
func (b *Broker[T]) SubscribeAll(ctx context.Context) iter.Seq[T] {
	hCopy, id, st := b.subscribeAllRegister()

	return func(yield func(T) bool) {
		defer b.subCleanup(id, st)

		for _, e := range hCopy {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !yield(e.val) {
				return
			}
		}

		b.readLive(ctx, id, st, func(_ time.Time, v T) bool {
			return yield(v)
		})
	}
}

// SubscribeAllReceived is [Broker.SubscribeReceived], but first yields the
// history, with no gap or duplicate between it and the live events.
func (b *Broker[T]) SubscribeAllReceived(ctx context.Context) iter.Seq2[time.Time, T] {
	hCopy, id, st := b.subscribeAllRegister()

	return func(yield func(time.Time, T) bool) {
		defer b.subCleanup(id, st)

		for _, e := range hCopy {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !yield(e.at, e.val) {
				return
			}
		}

		b.readLive(ctx, id, st, yield)
	}
}

// subscribeAllRegister copies the history and registers a subscriber whose
// live events start right after it.
func (b *Broker[T]) subscribeAllRegister() (hCopy []entry[T], id uint64, st *subState[T]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	hCopy = make([]entry[T], len(b.history))
	copy(hCopy, b.history)
	var liveMin uint64
	if len(hCopy) > 0 {
		liveMin = hCopy[len(hCopy)-1].seq + 1
	} else {
		liveMin = b.nextSeq + 1
	}
	id, st = b.registerLocked(liveMin)
	return hCopy, id, st
}

// registerLocked registers a subscriber starting at fromSeq. Caller must hold
// b.mu.
func (b *Broker[T]) registerLocked(fromSeq uint64) (uint64, *subState[T]) {
	id := b.nextSubID
	b.nextSubID++
	st := &subState[T]{
		fromSeq: fromSeq,
		ch:      make(chan entry[T], b.opts.subBuffer),
	}
	b.subs[id] = st
	return id, st
}

// readLive yields events from st until ctx is done or yield returns false.
// When ctx ends, the subscriber is unregistered before its buffer is drained,
// so publishes cannot keep refilling it.
func (b *Broker[T]) readLive(ctx context.Context, id uint64, st *subState[T], yield func(time.Time, T) bool) {
	for {
		select {
		case <-ctx.Done():
			b.unregister(id)
			b.yieldBuffered(st, yield)
			return
		default:
		}

		select {
		case e := <-st.ch:
			if !yield(e.at, e.val) {
				return
			}
		case <-ctx.Done():
			b.unregister(id)
			b.yieldBuffered(st, yield)
			return
		}
	}
}

func (b *Broker[T]) yieldBuffered(st *subState[T], yield func(time.Time, T) bool) {
	for {
		select {
		case e := <-st.ch:
			if !yield(e.at, e.val) {
				return
			}
		default:
			return
		}
	}
}

// subCleanup unregisters the subscriber and discards its buffer.
func (b *Broker[T]) subCleanup(id uint64, st *subState[T]) {
	b.unregister(id)
	for {
		select {
		case <-st.ch:
		default:
			return
		}
	}
}

func (b *Broker[T]) unregister(id uint64) {
	b.mu.Lock()
	delete(b.subs, id)
	b.mu.Unlock()
}

type subState[T any] struct {
	fromSeq uint64
	ch      chan entry[T]
}

// accept reports whether seq is new to the subscriber, skipping events it
// already got from history.
func (s *subState[T]) accept(seq uint64) bool {
	return seq >= s.fromSeq
}
