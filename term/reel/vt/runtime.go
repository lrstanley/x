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

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// Runtime is a pool of emulator instances owned by the caller. Live
// terminals never exceed the capacity passed to [NewRuntime].
// [Runtime.Open] blocks until [Terminal.Close] when the pool is full.
type Runtime struct {
	// sem counts live checkouts. Send blocks when the pool is at capacity.
	// Close receives one token.
	sem chan struct{}
	// mu guards idle.
	mu sync.Mutex
	// idle holds reset instances waiting for the next Open. Each still
	// owns its Wasm module.
	idle []*Terminal
}

// NewRuntime returns a pool that lends at most n instances. n less than 1
// uses max(1, GOMAXPROCS).
func NewRuntime(n int) *Runtime {
	if n < 1 {
		n = max(1, runtime.GOMAXPROCS(0))
	}
	return &Runtime{sem: make(chan struct{}, n)}
}

// Option configures [Runtime.Open].
type Option func(*openConfig)

type openConfig struct {
	scrollback int
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
	cfg := openConfig{scrollback: defaultScrollback}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("vt: open: %w", ctx.Err())
	}
	term := r.take()
	var err error
	if term == nil {
		term, err = newTerminal(r, cols, rows, cfg.scrollback)
	} else {
		err = term.checkout(cols, rows, cfg.scrollback)
	}
	if err != nil {
		<-r.sem
		return nil, err
	}
	return term, nil
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

func (r *Runtime) put(term *Terminal) {
	r.mu.Lock()
	r.idle = append(r.idle, term)
	r.mu.Unlock()
	<-r.sem
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
