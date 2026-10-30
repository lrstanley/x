// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// Runtime is a pool of emulator instances owned by the caller. Live
// terminals never exceed the capacity passed to [NewRuntime], if any.
// [Runtime.Open] blocks until [Terminal.Close] when the pool is full.
type Runtime struct {
	// sem counts live checkouts, or is nil for a pool without a limit.
	// Send blocks when the pool is at capacity. Close receives one token.
	sem chan struct{}
	// keep is how many idle instances the pool keeps for reuse.
	keep int
	// mu guards idle.
	mu sync.Mutex
	// idle holds reset instances waiting for the next Open. Each still
	// owns its Wasm module.
	idle []*Terminal
}

// NewRuntime returns a pool that lends at most n instances. Zero uses
// max(1, GOMAXPROCS).
//
// A negative n sets no limit, for terminals their callers hold for as long
// as they choose: with a limit, callers that each hold a terminal while
// opening another, such as parallel tests, can take every slot and wait on
// each other forever. Such a pool keeps up to max(1, GOMAXPROCS) closed
// instances for reuse and lets the rest be collected.
func NewRuntime(n int) *Runtime {
	procs := max(1, runtime.GOMAXPROCS(0))
	switch {
	case n < 0:
		return &Runtime{keep: procs}
	case n == 0:
		n = procs
	}
	return &Runtime{sem: make(chan struct{}, n), keep: n}
}

// Option configures [Runtime.Open].
type Option func(*openConfig)

type openConfig struct {
	scrollback int
	clock      func() time.Duration
	cellW      int
	cellH      int
	kitty      uint64
	unknown    func(UnknownSequence)
}

// UnknownSequenceMaxBytes is how much of each sequence
// [WithUnknownSequence] reports. Longer ones are cut and marked
// [UnknownSequence.Truncated].
const UnknownSequenceMaxBytes = 4096

// WithUnknownSequence calls fn with each APC or OSC sequence the terminal
// does not implement, such as a newer protocol, so a caller can log what it
// drops. An OSC whose number it implements is never reported, even when
// malformed. fn runs inside [Terminal.Write] with the terminal locked, so it
// must not call the terminal's methods.
func WithUnknownSequence(fn func(UnknownSequence)) Option {
	return func(cfg *openConfig) { cfg.unknown = fn }
}

// WithKittyStorage sets how many bytes of decoded Kitty graphics the
// terminal keeps, which a full reset restores. Zero turns Kitty graphics
// off. The default is [DefaultKittyStorage]. Images are decoded in the
// terminal's Wasm memory, which never shrinks, so a terminal whose memory grew
// past 32 MiB is not returned to its pool when closed.
func WithKittyStorage(n uint64) Option {
	return func(cfg *openConfig) { cfg.kitty = n }
}

// WithCellSize sets the pixel size of one cell, which a program reads with
// a size report and which sizes Kitty graphics placements, in cells. A
// renderer with another cell size scales images to the same cells. The
// default is 10 by 20. Sizes that are not positive are ignored.
func WithCellSize(width, height int) Option {
	return func(cfg *openConfig) {
		if width > 0 && height > 0 {
			cfg.cellW, cfg.cellH = width, height
		}
	}
}

// WithClock sets the clock that stamps when program events arrive, such as
// [State.Bell] and [State.Notifications]. A replay passes the timeline
// offset of the event it is applying, so the times line up with frames.
// The default is the time since [Runtime.Open].
func WithClock(now func() time.Duration) Option {
	return func(cfg *openConfig) { cfg.clock = now }
}

// WithScrollbackLines sets the retained history cap for this terminal.
// Negative n leaves the default in place.
func WithScrollbackLines(n int) Option {
	return func(cfg *openConfig) {
		if n >= 0 {
			cfg.scrollback = n
		}
	}
}

// Open borrows a terminal of cols by rows from this pool. It blocks until
// the pool has room or ctx is canceled. The instance is reset when it is
// closed. Scrollback defaults to 1000 lines unless [WithScrollbackLines]
// sets another non-negative cap.
func (r *Runtime) Open(ctx context.Context, cols, rows int, opts ...Option) (*Terminal, error) {
	if cols <= 0 || rows <= 0 {
		return nil, fmt.Errorf("vt: size %dx%d", cols, rows)
	}
	cfg := openConfig{scrollback: DefaultScrollback, kitty: DefaultKittyStorage}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	term := r.take()
	var err error
	if term == nil {
		term, err = newTerminal(r, cols, rows, cfg.scrollback)
	} else {
		err = term.checkout(cols, rows, cfg.scrollback)
	}
	if err != nil {
		r.release()
		return nil, err
	}
	if cfg.clock == nil {
		start := time.Now()
		cfg.clock = func() time.Duration { return time.Since(start) }
	}
	if err = term.configure(cfg); err != nil {
		_ = term.Close()
		return nil, err
	}
	return term, nil
}

// acquire takes a slot of a limited pool, waiting until one is free or ctx
// is done.
func (r *Runtime) acquire(ctx context.Context) error {
	if r.sem == nil {
		return nil
	}
	select {
	case r.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("vt: open: %w", ctx.Err())
	}
}

// release frees the slot acquire took.
func (r *Runtime) release() {
	if r.sem != nil {
		<-r.sem
	}
}

func (r *Runtime) take() *Terminal {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.idle) == 0 {
		return nil
	}
	term := r.idle[len(r.idle)-1]
	r.idle = r.idle[:len(r.idle)-1]
	return term
}

// configure applies the options of [Runtime.Open] to a checked-out terminal.
func (t *Terminal) configure(cfg openConfig) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clock = cfg.clock
	t.unknown = cfg.unknown
	var unknownMax uint32
	if cfg.unknown != nil {
		unknownMax = UnknownSequenceMaxBytes
	}
	// The limit is a size_t, 4 bytes on wasm32. Zero turns reports off. It
	// belongs to the parser, which a full reset leaves alone, so
	// reapplyLocked does not set it again.
	if err := t.mod.SetU32Option(t.term, wasmvt.TerminalOptionUnknownMaxBytes, unknownMax); err != nil {
		return err
	}
	if cfg.kitty != t.kitty.def || cfg.kitty != t.kitty.limit {
		t.kitty.def, t.kitty.limit = cfg.kitty, cfg.kitty
		if err := t.mod.SetU64Option(t.term, wasmvt.TerminalOptionKittyImageStorageLimit, cfg.kitty); err != nil {
			return err
		}
	}
	w, h := uint32(defaultCellWidth), uint32(defaultCellHeight)
	if cfg.cellW > 0 {
		w, h = uint32(cfg.cellW), uint32(cfg.cellH) //nolint:gosec // WithCellSize keeps them positive.
	}
	if w == t.cellW && h == t.cellH {
		return nil
	}
	t.cellW, t.cellH = w, h
	t.grid = nil
	t.gridFresh = false
	return t.resizeLocked(t.cols, t.rows)
}

// maxPooledMemory is the most Wasm memory a closed terminal may hold and
// still go back to its pool. Memory never shrinks, so one that grew past it,
// such as for large images, is left to the garbage collector. A fresh
// terminal holds under 2 MiB.
const maxPooledMemory = 32 << 20

// put returns a closed term to the pool, keeping it for reuse if reuse is
// set and the pool keeps fewer than its share.
func (r *Runtime) put(term *Terminal, reuse bool) {
	r.mu.Lock()
	if reuse && len(r.idle) < r.keep {
		r.idle = append(r.idle, term)
	}
	r.mu.Unlock()
	r.release()
}

func newTerminal(rt *Runtime, cols, rows, scrollback int) (*Terminal, error) {
	term := &Terminal{rt: rt, mod: wasmvt.New()}
	if err := term.prepare(cols, rows, scrollback); err != nil {
		return nil, err
	}
	return term, nil
}

var errClosed = errors.New("vt: terminal is closed")

// ErrNoValue means a libghostty read had nothing to return, such as a
// selection on an empty cell or a search with no matches.
//
// It is [wasmvt.ErrNoValue], GhosttyResult GHOSTTY_NO_VALUE.
var ErrNoValue = wasmvt.ErrNoValue
