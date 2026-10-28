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
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

type runResult struct {
	model tea.Model
	err   error
}

var _ MessageCollector = (*Harness)(nil)

// Harness is a test harness for a [tea.Program].
//
// Its checks follow one naming convention:
//
//   - Assert* report a failed check with [testing.TB.Errorf] and let the
//     test continue.
//   - Require* stop the test with [testing.TB.FailNow] when the check fails.
//   - Wait* block until the condition holds, and stop the test with
//     [testing.TB.Fatalf] if [WithTimeout] passes or the context is canceled
//     first.
//
// Most methods return the harness for chaining. The package-level
// functions of the same names check any [Viewable] or [MessageCollector]
// and return a result instead: whether an Assert* passed, or the view or
// message that satisfied a Wait*.
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
	imageScreen *vt.Buffer
	// image is the buffer [Harness.Image] draws into and returns.
	image *image.NRGBA
	// start is when the harness was created; blink and animation phases in
	// images are drawn relative to it.
	start       time.Time
	imageMu     sync.Mutex
	hooks       []Hook
	readyHooked bool

	// marksMu guards markCount and marks.
	marksMu   sync.Mutex
	markCount map[HookEvent]int
	marks     []hookMark
	closed    atomic.Bool
	// waits numbers recorded waits, for their annotation ids.
	waits atomic.Int64
}

// NewHarness runs the root [tea.Model] in a [tea.Program] and returns a
// harness to drive and assert on it. It waits up to the [WithTimeout]
// timeout for the model's first message, and registers [Harness.Close] as a
// test cleanup.
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

	// Built before the session, whose terminal holds a pool slot, so a
	// failure here leaves no slot taken.
	imageRenderer, err := render.New(cfg.recording.Render...)
	if err != nil {
		tb.Fatal(err)
	}
	// Bubble Tea writes capability queries before its first paint, so the
	// recording starts on that paint rather than on the blank terminal.
	session, err := reel.Open(
		tb.Context(), max(cfg.width, 1), max(cfg.height, 1),
		reel.WithTitle(tb.Name()), reel.WithSkipPristine(),
		reel.WithScrollOnInput(cfg.scrollOnInput),
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
	// Clipped, so one call's appends cannot write into another's.
	h.opts = slices.Clip(append(h.opts, withHarness(h)))
	failures.note = h.noteFailure
	if cfg.recordMessages {
		h.observer.onMessage = h.noteMessage
	}
	h.annotate(newStartNote(tb.Name(), cfg))

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
		h.noteExit(ferr)
		h.resultMu.Lock()
		h.result = &runResult{
			model: finalModel,
			err:   ferr,
		}
		h.resultMu.Unlock()
		close(h.done)
	}()

	// Registered before waiting, so a startup failure still cleans up.
	tb.Cleanup(h.Close)

	h.waitStarted(cfg)

	return h
}

// Close stops the program, exports the recording if the export policy says
// to, and releases the terminal. [NewHarness] registers it as a test cleanup,
// so calling it is optional. Calls after the first do nothing.
func (h *Harness) Close() {
	if h.closed.Swap(true) {
		return
	}
	// These run even when a hook or stopping the program fails the test. The
	// terminal returns to the pool after the After hooks, and the retained
	// screen is dropped first so a later draw cannot paint the previous
	// checkout's grid.
	defer h.session.Close()
	defer h.dropImageScreen()
	defer func() {
		if err := h.imageRenderer.Close(); err != nil {
			h.tb.Errorf("failed to close image renderer: %v", err)
		}
	}()
	defer h.hook(HookClose, nil)()

	cfg := collectOptions(h.tb, h.mergedOpts()...)

	// The recording ends here, on the last screen the test saw, leaving out
	// the program's exit repaint. The result is recorded first so the
	// recording holds it.
	h.annotate(h.newResultNote())
	end := h.markEvent(HookClose)
	defer h.exportRecording(end)

	// Unblock the program's input loop before Quit.
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
	// The test's context is canceled during cleanups.
	h.waitFinished(collectOptions(h.tb, h.mergedOpts(WithContext(context.Background()))...), nil)
}

func (h *Harness) waitStarted(cfg options) {
	h.tb.Helper()

	if h.program == nil {
		return
	}

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	note := func(o outcome) {
		h.annotate(&readyNote{Outcome: o, ElapsedMS: time.Since(h.start).Milliseconds()})
	}
	select {
	case <-h.observer.firstEvent:
		note(outcomeReady)
		if !h.readyHooked {
			h.readyHooked = true
			done := h.hook(HookReady, nil)
			done()
		}
	case <-timer.C:
		note(outcomeTimeout)
	case <-cfg.ctx.Done():
		note(outcomeCanceled)
	}
}

// NewComponentHarness is [NewHarness] for a component model, which must have
// a View() string method and an Update method returning either a [tea.Cmd]
// or the updated model and a [tea.Cmd]. Init is optional.
func NewComponentHarness[M any](tb testing.TB, model M, opts ...Option) *Harness {
	tb.Helper()
	m := &componentWrapper[M]{tb: tb, model: model}
	m.validate()
	return NewHarness(tb, m, opts...)
}

// check reports err from a terminal action without stopping the test.
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

// SendProgram sends msg to the [tea.Program].
//
// Prefer driving input with [Harness.Type], [Harness.Key], [Harness.Paste],
// or the mouse helpers, which go through the terminal as real input would.
func (h *Harness) SendProgram(msg uv.Event) *Harness {
	h.tb.Helper()
	defer h.hook(HookProgram, nil)()
	h.requireProgram()
	n := &programNote{Action: "send"}
	n.Type, n.Value = valueNote(msg)
	h.annotate(n)
	h.program.Send(msg)
	return h
}

// QuitProgram requests a graceful shutdown of the [tea.Program].
func (h *Harness) QuitProgram() *Harness {
	h.tb.Helper()
	defer h.hook(HookClose, nil)()
	h.requireProgram()
	h.annotate(&programNote{Action: "quit"})
	h.program.Quit()
	return h
}

// FinalModel waits for the [tea.Program] to finish and returns the root
// model.
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.requireProgram()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// FinalModelAs is [Harness.FinalModel], with the model as M. It fails the
// test when the model is not M.
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

// MessageHistory returns a copy of all messages the model received.
func (h *Harness) MessageHistory() iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.History()
}

// Messages returns an iterator over all messages the model received, past
// and future, until ctx is done. Past messages are complete, but the iterator
// buffers only 32 new ones: a loop that falls further behind misses the
// newer messages, which [Harness.MessageHistory] still has.
func (h *Harness) Messages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.SubscribeAll(ctx)
}

// LiveMessages returns an iterator over messages the model receives after
// the call returns, until ctx is done. Like [Harness.Messages], it buffers
// only 32 messages, and a loop that falls further behind misses the newer ones.
func (h *Harness) LiveMessages(ctx context.Context) iter.Seq[uv.Event] {
	h.tb.Helper()
	h.requireProgram()
	return h.observer.messages.Subscribe(ctx)
}
