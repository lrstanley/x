// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package broker provides a generic in-memory event broker: publishers push
// events without waiting for slow subscribers; history is optionally retained up
// to a max size; subscribers receive only events after registration, or may
// opt into replay of stored history followed by live delivery.
package broker

import (
	"context"
	"iter"
	"sync"
	"time"
)

// entry is one stored event with a monotonic sequence number.
type entry[T any] struct {
	seq uint64
	at  time.Time
	val T
}

// Broker accepts published events and fans them out to subscribers. History is
// stored in receipt order up to MaxHistory (oldest entries are dropped when at
// capacity). Publish does not run iterator bodies and does not wait for
// consumers beyond non-blocking sends to each subscriber buffer.
type Broker[T any] struct {
	opts options

	mu           sync.RWMutex
	nextSeq      uint64
	history      []entry[T]
	lastReceived time.Time
	subs         map[uint64]*subState[T]
	nextSubID    uint64
}

// New returns a Broker with the given optional settings.
func New[T any](opts ...Option) *Broker[T] {
	o := resolveOptions(opts)
	return &Broker[T]{
		opts:    o,
		history: make([]entry[T], 0, min(32, o.maxHistory)),
		subs:    make(map[uint64]*subState[T]),
	}
}

// Publish records v in history (subject to MaxHistory) and performs non-blocking
// delivery attempts to each subscriber buffer. Slow subscribers may drop events
// when their buffer is full.
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
		default:
			// Subscriber is lagging; drop rather than block publisher path.
		}
	}
}

// appendHistoryLocked appends e to history, dropping the oldest entry when at
// MaxHistory. Caller must hold b.mu exclusively; uncapped history when MaxHistory <= 0.
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

// HistoryWithTime yields stored events oldest-first (as originally published),
// each with the wall time recorded at Publish. Entries may have been trimmed by
// MaxHistory. A snapshot is taken before the first yield; concurrent publishes
// do not change this sequence.
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

// History yields stored events oldest-first (as originally published). Entries
// may have been trimmed by MaxHistory. A snapshot is taken before the first
// yield; concurrent publishes do not change this sequence.
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

// LastReceived returns the wall-clock time when the broker last accepted an
// event via Publish. If Publish has never been called, it returns the zero
// Time.
func (b *Broker[T]) LastReceived() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastReceived
}

// Subscribe yields values for events published after this call returns (history
// is not replayed). Registration is synchronous before this returns; see
// SubscribeReceived for lifecycle details.
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

// SubscribeReceived yields (received-at timestamp, value) for events published after
// this call returns (history is not replayed). The subscription is registered
// synchronously so publishers are safe to call immediately after SubscribeReceived
// returns. If another goroutine publishes concurrently, call SubscribeReceived (and
// retain the returned sequence) before starting those publishes.
// Iteration ends when ctx is cancelled or the consumer stops the range (yield
// returns false). Cleanup unregisters the subscriber and drains its buffer.
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

// SubscribeAll is like Subscribe but first yields all values currently in history
// (oldest first), then continues with live events. Registration is synchronous
// before this returns; see SubscribeAllReceived for full semantics.
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

// SubscribeAllReceived is like SubscribeReceived but first yields all events currently in
// history (oldest first), then continues with live events. Order matches
// Publish order. The live subscription is registered before this call returns;
// use the same ordering as SubscribeReceived if other goroutines publish concurrently.
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

// subscribeAllRegister copies the current history and registers a subscriber
// whose live stream starts immediately after that snapshot. Caller must not
// hold b.mu.
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

// registerLocked creates and stores a subscriber starting at fromSeq. Caller
// must hold b.mu exclusively.
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

// readLive yields values from st's queue until ctx is done or yield returns
// false. When ctx ends, the subscriber is unregistered before buffered events
// are yielded so new publishes do not keep refilling the buffer.
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

// yieldBuffered drains st's current queue into yield without blocking.
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

// subCleanup removes the subscription and discards any remaining values in st's
// channel so publishes do not block on a full buffer for an unregistered sub.
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

// unregister removes subscriber id from b; it no longer receives publishes.
func (b *Broker[T]) unregister(id uint64) {
	b.mu.Lock()
	delete(b.subs, id)
	b.mu.Unlock()
}

type subState[T any] struct {
	fromSeq uint64
	ch      chan entry[T]
}

// accept reports whether an event with sequence seq should be delivered to this
// subscriber (live replay uses fromSeq to skip duplicates already yielded from history).
func (s *subState[T]) accept(seq uint64) bool {
	return seq >= s.fromSeq
}
