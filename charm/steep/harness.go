// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

type runResult struct {
	model tea.Model
	err   error
}

// Harness is a test harness for a Bubble Tea program.
type Harness struct {
	*terminal
	tb       testing.TB
	program  *tea.Program
	observer *observer
	opts     []Option

	resultMu sync.RWMutex
	result   *runResult
	done     chan runResult
}

// NewHarness creates a new test harness for a Bubble Tea program (one which has
// a [tea.Model] as the root model). The test harness will run the program,
// capture its output, and provide assertions for the program's behavior.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
	tb.Helper()

	cfg := collectOptions(opts...)

	h := &Harness{
		tb:       tb,
		terminal: &terminal{vt: vt.NewEmulator(cfg.width, cfg.height)},
		observer: newObserver(tb, model),
		done:     make(chan runResult, 1),
		opts:     append([]Option(nil), opts...),
	}

	go func() {
		h.terminal.mu.Lock()
		h.terminal.vt.Resize(cfg.width, cfg.height)
		h.terminal.mu.Unlock()
	}() // So the emulator knows about it too.

	h.program = tea.NewProgram(
		h.observer,
		append(
			cfg.programOpts,
			// TODO: remove this default environment once Bubble Tea either flushes
			// startup DECRQM mode probes before input shutdown, or x/vt makes query
			// responses non-blocking. Without this, WT_SESSION/kitty-like envs make
			// Bubble Tea emit synchronized-output/unicode-core queries that x/vt can
			// answer by blocking on its input pipe during fast test cleanup.
			tea.WithEnvironment(harnessEnvironment()),
			tea.WithContext(tb.Context()),
			tea.WithInput(h.terminal),
			tea.WithOutput(h.terminal),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	// TODO: switch back to direct Kill once Bubble Tea synchronizes Kill with
	// Run startup. Calling Kill while Run is still initializing handlers,
	// cancelReader, renderer, and renderer sync.Once currently races and can
	// corrupt shutdown state under fast tests and the race detector.
	tb.Cleanup(func() {
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

	go func() {
		finalModel, err := h.program.Run()
		h.done <- runResult{
			model: finalModel,
			err:   err,
		}
	}()

	return h
}

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

// NewComponentHarness creates a new test harness for a Bubble Tea component model.
// This effectively emulates a component as a full Bubble Tea program.
func NewComponentHarness[M any](tb testing.TB, model M, opts ...Option) *Harness {
	tb.Helper()

	m := &componentWrapper[M]{tb: tb, model: model}

	m.validate()

	return NewHarness(tb, m, opts...)
}

func (h *Harness) mergedOpts(call ...Option) []Option {
	return append(h.opts, call...)
}

// Send sends msg ([tea.Msg] or [uv.Event], either works) to the running program.
func (h *Harness) Send(msg uv.Event) {
	h.program.Send(msg)
}

// Type sends s as a sequence of key press messages.
func (h *Harness) Type(s string) {
	for _, r := range s {
		h.Send(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

// Quit asks the running Bubble Tea program to exit.
func (h *Harness) Quit() {
	h.program.Quit()
}

// FinalOutput waits for the Bubble Tea program to finish and returns the last
// captured screen buffer output.
func (h *Harness) FinalOutput(opts ...Option) string {
	h.tb.Helper()
	h.WaitFinished(opts...)
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.Render()
}

// FinalView waits for the Bubble Tea program to finish and returns the last
// captured view content (returned by the model's View method).
func (h *Harness) FinalView(opts ...Option) string {
	h.tb.Helper()
	h.WaitFinished(opts...)
	h.observer.mu.RLock()
	defer h.observer.mu.RUnlock()
	return h.observer.lastViewSnapshot
}

// FinalModel waits for the Bubble Tea program to finish and returns the latest
// underlying root model.
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// Messages returns a copy of messages observed by the underlying model.
func (h *Harness) Messages() []uv.Event {
	return h.observer.messages()
}

// View invokes the current underlying models View method and returns the result.
func (h *Harness) View() string {
	return h.observer.View().Content
}

// ViewWidth returns the width of the view.
func (h *Harness) ViewWidth(opts ...Option) int {
	v, _ := Dimensions(h.View(), opts...)
	return v
}

// ViewHeight returns the height of the view.
func (h *Harness) ViewHeight(opts ...Option) int {
	_, v := Dimensions(h.View(), opts...)
	return v
}

// ViewDimensions returns the width and height of the view.
func (h *Harness) ViewDimensions(opts ...Option) (width, height int) {
	return Dimensions(h.View(), opts...)
}
