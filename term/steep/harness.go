// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"image"
	"iter"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

type runResult struct {
	model tea.Model
	err   error
}

var _ MessageCollector = (*Harness)(nil) // Enforce implementation of [MessageCollector].

// Harness is a test harness for a [tea.Program].
type Harness struct {
	tb       testing.TB
	failures *failureTB
	session  *reel.Session
	program  *tea.Program
	observer *observer
	opts     []Option
	cfg      options

	resultMu sync.RWMutex
	result   *runResult
	done     chan struct{}

	imageRenderer *render.Renderer
	// imageScreen is the last viewport clone drawn by Image. Close drops
	// it so a later draw cannot reuse a grid from before reset.
	imageScreen *screen.Buffer
	// image is the buffer [Harness.Image] draws into and returns.
	image *image.NRGBA
	// start is when the harness was created. Frames are drawn as of their
	// offset from it, which blinking and animated indicators take their
	// phase from (see [timelineAt]).
	start       time.Time
	imageMu     sync.Mutex
	hooks       []Hook
	readyHooked bool

	// marksMu guards markCount and marks, the marks placed for hook events
	// (see [Harness.Mark]).
	marksMu   sync.Mutex
	markCount map[HookEvent]int
	marks     []hookMark
	// closed is set by the first [Harness.Close].
	closed atomic.Bool
}

// NewHarness creates a test harness by running the root [tea.Model] in a
// [tea.Program]. The harness captures rendered terminal output and exposes
// helpers for driving and asserting on runtime behavior.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
	tb.Helper()
	failures := &failureTB{TB: tb}
	tb = failures

	cfg := collectOptions(tb, opts...)

	if cfg.ctx == nil || !cfg.wasContextSet {
		ctx, cancel := signal.NotifyContext(tb.Context(), os.Interrupt)
		tb.Cleanup(cancel)
		opts = append(opts, WithContext(ctx))
		cfg = collectOptions(tb, opts...)
	}

	// The renderer is built before the session, whose terminal holds a pool
	// slot, so failing to build it leaves no slot taken.
	imageRenderer, err := render.New(cfg.recording.Render...)
	if err != nil {
		tb.Fatal(err)
	}
	// Bubble Tea writes capability queries before its first paint, so the
	// recording starts on that paint rather than on the blank terminal.
	session, err := reel.Open(
		tb.Context(), max(cfg.width, 1), max(cfg.height, 1),
		reel.WithTitle(tb.Name()), reel.WithSkipPristine(),
	)
	if err != nil {
		tb.Fatal(err)
	}

	h := &Harness{
		tb:            tb,
		failures:      failures,
		session:       session,
		observer:      newObserver(tb, model),
		done:          make(chan struct{}),
		opts:          append([]Option(nil), opts...),
		cfg:           cfg,
		imageRenderer: imageRenderer,
		start:         time.Now(),
		hooks:         append([]Hook(nil), cfg.hooks...),
		markCount:     make(map[HookEvent]int),
	}

	env, programOpts := cfg.envVars, cfg.programOpts
	if cfg.color != 0 {
		env = cfg.color.env(env)
		programOpts = append([]tea.ProgramOption{tea.WithColorProfile(cfg.color.teaProfile())}, programOpts...)
	}
	h.program = tea.NewProgram(
		h.observer,
		append(
			programOpts,
			tea.WithEnvironment(env),
			tea.WithContext(cfg.ctx),
			tea.WithInput(h.session),
			tea.WithOutput(h.session),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	go func() {
		finalModel, ferr := h.program.Run()
		h.resultMu.Lock()
		h.result = &runResult{
			model: finalModel,
			err:   ferr,
		}
		h.resultMu.Unlock()
		close(h.done)
	}()

	// Registered before waiting for the program, so a startup failure still
	// stops it, exports, and returns the terminal.
	tb.Cleanup(h.Close)

	h.waitStarted(cfg)

	return h
}

// Close stops the program, exports the recording when the export policy says
// to (see [WithExportOnFailure] and [WithExportAlways]), and returns the
// terminal. [NewHarness] registers it as a cleanup of the test, so calling it
// is optional. Calls after the first do nothing.
func (h *Harness) Close() {
	if h.closed.Swap(true) {
		return
	}
	// These run last, in reverse, even when a hook or stopping the program
	// fails the test: the terminal returns to the pool once the After hooks
	// have run, and the retained screen is dropped before then so a later
	// draw cannot paint the previous checkout's grid.
	defer h.session.Close()
	defer h.dropImageScreen()
	defer func() {
		if err := h.imageRenderer.Close(); err != nil {
			h.tb.Errorf("failed to close image renderer: %v", err)
		}
	}()
	defer h.hook(HookClose, nil)()

	cfg := collectOptions(h.tb, h.mergedOpts()...)

	// The recording ends here, on the last screen the test saw: the
	// program's repaint as it exits, such as leaving the alternate screen,
	// is left out. It is exported once the program has stopped, or failed
	// to.
	end := h.markEvent(HookClose)
	defer h.exportRecording(end)

	// Unblock the program input loop before Quit.
	h.session.Terminal().CloseReader()

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
	h.waitFinished(WithContext(context.Background()))
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
		if !h.readyHooked {
			h.readyHooked = true
			done := h.hook(HookReady, nil)
			done()
		}
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

// check fails the test, without stopping it, when a terminal action returned
// err.
func (h *Harness) check(err error) {
	h.tb.Helper()
	if err != nil {
		h.tb.Errorf("steep: %v", err)
	}
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
//
// Prefer not to use SendProgram. It will error if the Harness is not a [tea.Program].
// Drive input with [Harness.Type], [Harness.Key], [Harness.Paste], or the mouse
// helpers instead.
func (h *Harness) SendProgram(msg uv.Event) *Harness {
	h.tb.Helper()
	defer h.hook(HookProgram, nil)()
	h.requireProgram()
	h.program.Send(msg)
	return h
}

// QuitProgram requests a graceful shutdown of the [tea.Program].
//
// Prefer not to use QuitProgram. It will error if the Harness is not a [tea.Program].
func (h *Harness) QuitProgram() *Harness {
	h.tb.Helper()
	defer h.hook(HookClose, nil)()
	h.requireProgram()
	h.program.Quit()
	return h
}

// FinalModel waits for the [tea.Program] to finish and returns the latest
// underlying root model.
//
// Prefer not to use FinalModel. It will error if the Harness is not a [tea.Program].
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.requireProgram()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// FinalModelAs waits for the [tea.Program] to finish and returns the underlying
// model as M. It fails the test when the model is not M.
//
// Prefer not to use FinalModelAs. It will error if the Harness is not a [tea.Program].
//
// See also [Harness.FinalModel].
func (h *Harness) FinalModelAs[M any](opts ...Option) M {
	h.tb.Helper()
	model := h.FinalModel(opts...)
	typed, ok := model.(M)
	if !ok {
		var zero M
		h.tb.Fatalf("final model type = %T, want %T", model, zero)
	}
	return typed
}

// MessageHistory returns a copy of all messages observed by the underlying model.
//
// Prefer not to use MessageHistory. It will error if the Harness is not a [tea.Program].
func (h *Harness) MessageHistory() iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.History()
}

// Messages returns a iterator to all (historical and live) messages observed
// by the underlying model.
//
// Prefer not to use Messages. It will error if the Harness is not a [tea.Program].
func (h *Harness) Messages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.SubscribeAll(ctx)
}

// LiveMessages returns a new iterator to live messages observed by the
// underlying model, starting from the first message observed after the call
// returns.
//
// Prefer not to use LiveMessages. It will error if the Harness is not a [tea.Program].
func (h *Harness) LiveMessages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.Subscribe(ctx)
}
