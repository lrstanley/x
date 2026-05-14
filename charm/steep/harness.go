// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"iter"
	"os"
	"os/signal"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type runResult struct {
	model tea.Model
	err   error
}

var _ MessageCollector = (*Harness)(nil) // Enforce implementation of [MessageCollector].

// Harness is a test harness for a [tea.Program].
type Harness struct {
	tb       testing.TB
	emulator *emulator
	program  *tea.Program
	observer *observer
	opts     []Option

	resultMu sync.RWMutex
	result   *runResult
	done     chan struct{}
}

// NewHarness creates a test harness by running the root [tea.Model] in a
// [tea.Program]. The harness captures rendered terminal output and exposes
// helpers for driving and asserting on runtime behavior.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
	tb.Helper()

	cfg := collectOptions(opts...)

	if cfg.ctx == nil || !cfg.wasContextSet {
		ctx, cancel := signal.NotifyContext(tb.Context(), os.Interrupt)
		tb.Cleanup(cancel)
		opts = append(opts, WithContext(ctx))
		cfg = collectOptions(opts...)
	}

	h := &Harness{
		tb:       tb,
		emulator: newEmulator(cfg.width, cfg.height),
		observer: newObserver(tb, model),
		done:     make(chan struct{}),
		opts:     append([]Option(nil), opts...),
	}

	h.program = tea.NewProgram(
		h.observer,
		append(
			cfg.programOpts,
			tea.WithEnvironment(cfg.envVars),
			tea.WithContext(cfg.ctx),
			tea.WithInput(h.emulator),
			tea.WithOutput(h.emulator),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	go func() {
		finalModel, err := h.program.Run()
		h.resultMu.Lock()
		h.result = &runResult{
			model: finalModel,
			err:   err,
		}
		h.resultMu.Unlock()
		close(h.done)
	}()

	h.waitStarted(cfg)

	tb.Cleanup(func() {
		h.Close()
	})

	return h
}

func (h *Harness) Close() {
	cfg := collectOptions(h.mergedOpts()...)

	go h.emulator.Close()

	quitDone := make(chan struct{})
	go func() {
		if h.program != nil {
			h.program.Quit()
		} else {
			// Should generally be sufficient for most programs to quit.
			h.KeyCtrlC()
			h.KeyCtrlC()
		}
		<-h.done
		close(quitDone)
	}()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-quitDone:
	case <-cfg.ctx.Done():
		if h.program != nil {
			h.program.Kill()
		}
	case <-timer.C:
		if h.program != nil {
			h.program.Kill()
		}
	}
	// TB context is already canceled during Cleanups (see testing.T.Context); join with
	// a detached parent so WaitFinished observes h.done/timer only.
	h.WaitFinished(WithContext(context.Background()))
}

func (h *Harness) waitStarted(cfg options) {
	h.tb.Helper()

	if h.program == nil {
		return
	}

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-h.observer.firstEvent:
	case <-timer.C:
	case <-cfg.ctx.Done():
	}
}

// NewComponentHarness creates a test harness for a component model by wrapping it
// in a root [tea.Model] and running it inside a [tea.Program].
func NewComponentHarness[M any](tb testing.TB, model M, opts ...Option) *Harness {
	tb.Helper()
	m := &componentWrapper[M]{tb: tb, model: model}
	m.validate()
	return NewHarness(tb, m, opts...)
}

func (h *Harness) mergedOpts(call ...Option) []Option {
	h.tb.Helper()
	return append(h.opts, call...)
}

func (h *Harness) requireProgram() {
	h.tb.Helper()
	if h.program == nil {
		h.tb.Fatalf("Harness is not a Bubble Tea program, but called a tea.Program-related method")
	}
}

// SendProgram sends msg ([tea.Msg] or [uv.Event], either works) to the [tea.Program].
func (h *Harness) SendProgram(msg uv.Event) *Harness {
	h.tb.Helper()
	h.requireProgram()
	h.program.Send(msg)
	return h
}

// QuitProgram requests a graceful shutdown of the [tea.Program].
func (h *Harness) QuitProgram() *Harness {
	h.tb.Helper()
	h.requireProgram()
	h.program.Quit()
	return h
}

// FinalModel waits for the [tea.Program] to finish and returns the latest
// underlying root model.
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.requireProgram()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// MessageHistory returns a copy of all messages observed by the underlying model.
func (h *Harness) MessageHistory() iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.History()
}

// Messages returns a iterator to all (historical and live) messages observed
// by the underlying model.
func (h *Harness) Messages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.SubscribeAll(ctx)
}

// LiveMessages returns a new iterator to live messages observed by the
// underlying model, starting from the first message observed after the call
// returns.
func (h *Harness) LiveMessages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.Subscribe(ctx)
}
