// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"iter"
	"os"
	"strings"
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

// TODO: remove this default environment once Bubble Tea either flushes
// startup DECRQM mode probes before input shutdown, or x/vt makes query
// responses non-blocking. Without this, WT_SESSION/kitty-like envs make
// Bubble Tea emit synchronized-output/unicode-core queries that x/vt can
// answer by blocking on its input pipe during fast test cleanup.
func harnessEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "TERM", "TERM_PROGRAM", "WT_SESSION":
			continue
		default:
			env = append(env, entry)
		}
	}
	return append(env, "TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal")
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
	done     chan runResult
}

// NewHarness creates a test harness by running the root [tea.Model] in a
// [tea.Program]. The harness captures rendered terminal output and exposes
// helpers for driving and asserting on runtime behavior.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
	tb.Helper()

	cfg := collectOptions(opts...)

	h := &Harness{
		tb:       tb,
		emulator: newEmulator(cfg.width, cfg.height),
		observer: newObserver(tb, model),
		done:     make(chan runResult, 1),
		opts:     append([]Option(nil), opts...),
	}

	h.program = tea.NewProgram(
		h.observer,
		append(
			cfg.programOpts,
			tea.WithEnvironment(harnessEnvironment()),
			tea.WithContext(tb.Context()),
			tea.WithInput(h.emulator),
			tea.WithOutput(h.emulator),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	go func() {
		finalModel, err := h.program.Run()
		h.done <- runResult{
			model: finalModel,
			err:   err,
		}
	}()

	h.waitStarted(cfg)

	// TODO: have to add some extra protections for now due to upstream deadlock
	// scenarios, due to [vt.Emulator] using an [io.Pipe].
	tb.Cleanup(func() {
		// On graceful Quit(), Bubble Tea's shutdown waits for the input read
		// loop to finish ([Program.shutdown] → waitForReadLoop). Reads come
		// from emulator.Read, fed by pumpVTToTea copying vt.Read. vt.Read blocks
		// until vt's input pipe closes. That pipe is wired from closeInput().
		// If closeInput ran only inside emulator.Close() after Run() returned,
		// we'd deadlock: shutdown waits read loop → read waits pump → pump waits
		// vt.Read → vt input still open → Run never completes.
		h.emulator.closeInput()

		quitDone := make(chan struct{})
		go func() {
			h.program.Quit()
			close(quitDone)
		}()

		timer := time.NewTimer(cfg.timeout)
		defer timer.Stop()

		select {
		case <-quitDone:
		case <-timer.C:
			h.program.Kill()
		}
		h.WaitFinished()
	})

	return h
}

// TODO: have to add some extra protections for now due to upstream deadlock
// scenarios, due to [vt.Emulator] using an [io.Pipe].
func (h *Harness) Close() error {
	// On graceful Quit(), Bubble Tea's shutdown waits for the input read
	// loop to finish ([Program.shutdown] → waitForReadLoop). Reads come
	// from emulator.Read, fed by pumpVTToTea copying vt.Read. vt.Read blocks
	// until vt's input pipe closes. That pipe is wired from closeInput().
	// If closeInput ran only inside emulator.Close() after Run() returned,
	// we'd deadlock: shutdown waits read loop → read waits pump → pump waits
	// vt.Read → vt input still open → Run never completes.
	h.emulator.closeInput()

	cfg := collectOptions(h.mergedOpts()...)

	quitDone := make(chan struct{})
	go func() {
		if h.program != nil {
			h.program.Quit()
		}
		close(quitDone)
	}()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-quitDone:
	case <-timer.C:
		if h.program != nil {
			h.program.Kill()
		}
	}
	if h.program != nil {
		h.WaitFinished()
	}
	return nil
}

// TODO: workaround due to https://github.com/charmbracelet/bubbletea/issues/1689
// and a few other misc upstream bugs.
func (h *Harness) waitStarted(cfg options) {
	h.tb.Helper()

	if h.program == nil {
		return
	}

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-h.observer.firstEvent:
	case result := <-h.done:
		h.resultMu.Lock()
		h.result = &result
		h.resultMu.Unlock()
		_ = h.emulator.Close()
		h.tb.Fatalf("bubble tea program finished before receiving a startup message")
	case <-timer.C:
		h.program.Kill()
		h.WaitFinished()
		h.tb.Fatalf("timeout waiting for bubble tea program to receive a startup message after %s", cfg.timeout)
	case <-h.tb.Context().Done():
		h.program.Kill()
		h.WaitFinished()
		h.tb.Fatalf("wait for bubble tea program startup canceled: %v", h.tb.Context().Err())
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
