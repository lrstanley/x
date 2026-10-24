// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"cmp"
	"context"
	"iter"
	"os"
	"os/signal"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/vt/screen"
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
	emulator *emulator
	program  *tea.Program
	observer *observer
	opts     []Option
	cfg      options

	resultMu sync.RWMutex
	result   *runResult
	done     chan struct{}

	imageRenderer *still.Renderer
	// imageScreen is the last viewport clone drawn by Image. Close drops
	// it so a later draw cannot reuse a grid from before reset.
	imageScreen *screen.Buffer
	// imageState is the last emulator state copied into the renderer.
	// Its color interfaces stay when the NRGBA bytes are unchanged, and
	// imageStatusGen is the generation of the OSC 7501 records it holds.
	imageState     still.EmulatorState
	imageStateSet  bool
	imageStatusGen uint64
	imageMu        sync.Mutex
	export         *exportManager
	hooks          []Hook
	logHook        *hookLog
	readyHooked    bool

	// imageAt pins the renderer clock while a frame is drawn as of when it
	// was on screen. Zero is the wall clock. It is read and written under
	// imageMu, which every renderer draw holds.
	imageAt time.Time
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

	// The renderer is built before the emulator, which holds a pool slot, so
	// its clock reads h once h exists. It comes before the caller's options,
	// so a [still.WithClock] in [WithRenderer] replaces it, and export can no
	// longer redraw a held frame's progress bar and program status as of a
	// later time.
	var h *Harness
	clock := still.WithClock(func() time.Time { return cmp.Or(h.imageAt, time.Now()) })
	imageRenderer, err := still.New(append([]still.Option{clock}, cfg.imageRendererOpts...)...)
	if err != nil {
		tb.Fatal(err)
	}

	h = &Harness{
		tb:            tb,
		failures:      failures,
		emulator:      newEmulator(tb, cfg.width, cfg.height),
		observer:      newObserver(tb, model),
		done:          make(chan struct{}),
		opts:          append([]Option(nil), opts...),
		cfg:           cfg,
		imageRenderer: imageRenderer,
		hooks:         append([]Hook(nil), cfg.hooks...),
	}

	em, err := newExportManager(h, cfg)
	if err != nil {
		tb.Fatal(err)
	}
	h.export = em
	if em != nil && em.log != nil {
		h.logHook = em.log
	}

	if h.export != nil && h.export.gifCh != nil {
		h.emulator.setWriteNotify(h.export.noteCommit)
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
		finalModel, ferr := h.program.Run()
		h.resultMu.Lock()
		h.result = &runResult{
			model: finalModel,
			err:   ferr,
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
	defer h.hook(HookClose, nil)()

	cfg := collectOptions(h.tb, h.mergedOpts()...)

	// Unblock the program input loop before Quit. The terminal stays
	// readable for export, then returns to the pool when Close returns.
	// Drop the retained screen before the terminal is pooled so a later
	// draw cannot paint the previous checkout's grid.
	defer h.emulator.Close()
	defer h.dropImageScreen()
	h.emulator.closeInput()

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

	if h.export != nil {
		if err := h.export.Finalize(h.tb.Failed()); err != nil {
			h.tb.Errorf("export finalize: %v", err)
		}
	}

	if err := h.imageRenderer.Close(); err != nil {
		h.tb.Errorf("failed to close image renderer: %v", err)
	}
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
