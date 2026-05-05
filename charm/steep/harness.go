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

// Harness is a test harness for a Bubble Tea program.
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

// NewHarness creates a new test harness for a Bubble Tea program (one which has
// a [tea.Model] as the root model). The test harness will run the program,
// capture its output, and provide assertions for the program's behavior.
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

// TODO: workaround due to https://github.com/charmbracelet/bubbletea/issues/1689
// and a few other misc upstream bugs.
func (h *Harness) waitStarted(cfg options) {
	h.tb.Helper()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-h.observer.firstEvent:
	case result := <-h.done:
		h.resultMu.Lock()
		h.result = &result
		h.resultMu.Unlock()
		h.emulator.closeInput()
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

// NewComponentHarness creates a new test harness for a Bubble Tea component model.
// This effectively emulates a component as a full [tea.Program].
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

// Send sends msg ([tea.Msg] or [uv.Event], either works) to the [tea.Program].
func (h *Harness) Send(msg uv.Event) *Harness {
	h.tb.Helper()
	h.program.Send(msg)
	return h
}

// Type sends a sequence of key press messages to the [tea.Program]. This is designed
// for providing regular text input, not more complex key combinations (ctrl-key,
// alt-key, etc).
func (h *Harness) Type(s string) *Harness {
	h.tb.Helper()
	for _, r := range s {
		h.program.Send(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
	return h
}

// Key sends a single key press message to the [tea.Program], e.g. "ctrl+a",
// "enter", "space", etc.
func (h *Harness) Key(key string) *Harness {
	h.tb.Helper()
	h.program.Send(keyEventToTea(mapKeyToEvent(key)))
	return h
}

// KeyUp sends an up-arrow key press to the [tea.Program].
func (h *Harness) KeyUp() *Harness {
	h.tb.Helper()
	return h.Key("up")
}

// KeyDown sends a down-arrow key press to the [tea.Program].
func (h *Harness) KeyDown() *Harness {
	h.tb.Helper()
	return h.Key("down")
}

// KeyLeft sends a left-arrow key press to the [tea.Program].
func (h *Harness) KeyLeft() *Harness {
	h.tb.Helper()
	return h.Key("left")
}

// KeyRight sends a right-arrow key press to the [tea.Program].
func (h *Harness) KeyRight() *Harness {
	h.tb.Helper()
	return h.Key("right")
}

// KeyEsc sends an escape key press to the [tea.Program].
func (h *Harness) KeyEsc() *Harness {
	h.tb.Helper()
	return h.Key("esc")
}

// KeyDelete sends a delete (forward-delete) key press to the [tea.Program].
func (h *Harness) KeyDelete() *Harness {
	h.tb.Helper()
	return h.Key("delete")
}

// KeyBackspace sends a backspace key press to the [tea.Program].
func (h *Harness) KeyBackspace() *Harness {
	h.tb.Helper()
	return h.Key("backspace")
}

// KeyCtrlC sends ctrl+c to the [tea.Program].
func (h *Harness) KeyCtrlC() *Harness {
	h.tb.Helper()
	return h.Key("ctrl+c")
}

// KeyCtrlD sends ctrl+d to the [tea.Program].
func (h *Harness) KeyCtrlD() *Harness {
	h.tb.Helper()
	return h.Key("ctrl+d")
}

// Quit asks the running [tea.Program] to exit.
func (h *Harness) Quit() *Harness {
	h.tb.Helper()
	h.program.Quit()
	return h
}

// FinalOutput waits for the [tea.Program] to finish and returns the last captured
// screen buffer output.
func (h *Harness) FinalOutput(opts ...Option) string {
	h.tb.Helper()
	h.WaitFinished(opts...)
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Render()
}

// FinalView waits for the [tea.Program] to finish and returns the last captured
// view content (returned by the model's View method).
func (h *Harness) FinalView(opts ...Option) string {
	h.tb.Helper()
	h.WaitFinished(opts...)
	h.observer.mu.RLock()
	defer h.observer.mu.RUnlock()
	return h.observer.lastViewSnapshot
}

// FinalModel waits for the [tea.Program] to finish and returns the latest
// underlying root model.
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// MessageHistory returns a copy of all messages observed by the underlying model.
func (h *Harness) MessageHistory() iter.Seq[uv.Event] {
	return h.observer.messages.History()
}

// Messages returns a iterator to all (historical and live) messages observed
// by the underlying model.
func (h *Harness) Messages(ctx context.Context) iter.Seq[uv.Event] {
	return h.observer.messages.SubscribeAll(ctx)
}

// LiveMessages returns a new iterator to live messages observed by the
// underlying model, starting from the first message observed after the call
// returns.
func (h *Harness) LiveMessages(ctx context.Context) iter.Seq[uv.Event] {
	return h.observer.messages.Subscribe(ctx)
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
