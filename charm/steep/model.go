// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type runResult struct {
	model tea.Model
	err   error
}

// Model drives a root Bubble Tea model under test.
type Model struct {
	program *tea.Program
	wrapper *modelWrapper
	output  *safeBuffer

	resultMu sync.Mutex
	result   *runResult
	done     chan runResult
}

// NewModel starts a Bubble Tea program for model and returns a test harness.
func NewModel(tb testing.TB, model tea.Model, opts ...Option) *Model {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := &safeBuffer{}
	wrapper := newModelWrapper(model)

	programOpts := append([]tea.ProgramOption{}, cfg.programOpts...)
	programOpts = append(programOpts,
		tea.WithInput(nil),
		tea.WithOutput(out),
		tea.WithoutSignals(),
		tea.WithWindowSize(cfg.width, cfg.height),
	)

	h := &Model{
		program: tea.NewProgram(wrapper, programOpts...),
		wrapper: wrapper,
		output:  out,
		done:    make(chan runResult, 1),
	}

	go func() {
		finalModel, err := h.program.Run()
		h.done <- runResult{
			model: finalModel,
			err:   err,
		}
	}()

	if cfg.width > 0 || cfg.height > 0 {
		h.Send(tea.WindowSizeMsg{Width: cfg.width, Height: cfg.height})
	}

	return h
}

// Send sends msg to the running Bubble Tea program.
func (m *Model) Send(msg tea.Msg) {
	m.program.Send(msg)
}

// Type sends s as a sequence of key press messages.
func (m *Model) Type(s string) {
	for _, r := range s {
		m.Send(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

// Quit asks the running Bubble Tea program to exit.
func (m *Model) Quit() error {
	m.program.Quit()
	return nil
}

// WaitFinished waits for the Bubble Tea program to finish.
func (m *Model) WaitFinished(tb testing.TB, opts ...Option) {
	tb.Helper()
	m.waitFinished(tb, opts...)
}

// FinalOutput waits for the Bubble Tea program to finish and returns the last
// captured view content.
func (m *Model) FinalOutput(tb testing.TB, opts ...Option) []byte {
	tb.Helper()
	m.waitFinished(tb, opts...)
	return m.outputBytes()
}

// FinalModel waits for the Bubble Tea program to finish and returns the latest
// underlying root model.
func (m *Model) FinalModel(tb testing.TB, opts ...Option) tea.Model {
	tb.Helper()
	m.waitFinished(tb, opts...)
	return m.wrapper.currentModel()
}

// WaitFor waits until condition returns true for the latest view output.
func (m *Model) WaitFor(tb testing.TB, condition func(bts []byte) bool, opts ...Option) []byte {
	tb.Helper()
	return WaitFor(tb, m, condition, opts...)
}

// WaitContains waits until output contains all substrings.
func (m *Model) WaitContains(tb testing.TB, substr ...[]byte) []byte {
	tb.Helper()
	return WaitContains(tb, m, substr...)
}

// WaitContainsString waits until output contains all substrings.
func (m *Model) WaitContainsString(tb testing.TB, substr ...string) []byte {
	tb.Helper()
	return WaitContainsString(tb, m, substr...)
}

// WaitNotContains waits until output contains none of the substrings.
func (m *Model) WaitNotContains(tb testing.TB, substr ...[]byte) []byte {
	tb.Helper()
	return WaitNotContains(tb, m, substr...)
}

// WaitNotContainsString waits until output contains none of the substrings.
func (m *Model) WaitNotContainsString(tb testing.TB, substr ...string) []byte {
	tb.Helper()
	return WaitNotContainsString(tb, m, substr...)
}

// ExpectStringContains fails the test unless all substrings appear in output.
func (m *Model) ExpectStringContains(tb testing.TB, substr ...string) {
	tb.Helper()
	expectStringContains(tb, m, substr...)
}

// ExpectStringNotContains fails the test if any substring appears in output.
func (m *Model) ExpectStringNotContains(tb testing.TB, substr ...string) {
	tb.Helper()
	expectStringNotContains(tb, m, substr...)
}

// ExpectHeight fails the test unless output has height rows.
func (m *Model) ExpectHeight(tb testing.TB, height int) {
	tb.Helper()
	expectHeight(tb, m, height)
}

// ExpectWidth fails the test unless output has width columns.
func (m *Model) ExpectWidth(tb testing.TB, width int) {
	tb.Helper()
	expectWidth(tb, m, width)
}

// ExpectDimensions fails the test unless output has width columns and height rows.
func (m *Model) ExpectDimensions(tb testing.TB, width, height int) {
	tb.Helper()
	expectDimensions(tb, m, width, height)
}

// Messages returns a copy of messages observed by the root model wrapper.
func (m *Model) Messages() []tea.Msg {
	return m.wrapper.messages()
}

func (m *Model) outputBytes() []byte {
	return []byte(m.wrapper.output())
}

func (m *Model) waitFinished(tb testing.TB, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(opts...)
	m.resultMu.Lock()
	if result := m.result; result != nil {
		m.resultMu.Unlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		return
	}
	m.resultMu.Unlock()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case result := <-m.done:
		m.resultMu.Lock()
		m.result = &result
		m.resultMu.Unlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		if wrapper, ok := result.model.(*modelWrapper); ok {
			m.wrapper.replace(wrapper.currentModel())
		}
	case <-timer.C:
		tb.Fatalf("timeout waiting for bubble tea program to finish after %s", cfg.timeout)
	}
}

type modelWrapper struct {
	mu           sync.Mutex
	model        tea.Model
	viewOutput   string
	observedMsgs []tea.Msg
}

func newModelWrapper(model tea.Model) *modelWrapper {
	w := &modelWrapper{model: model}
	w.capture()
	return w
}

func (w *modelWrapper) Init() tea.Cmd {
	cmd := w.currentModel().Init()
	w.capture()
	return cmd
}

func (w *modelWrapper) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	w.mu.Lock()
	w.observedMsgs = append(w.observedMsgs, msg)
	model := w.model
	w.mu.Unlock()

	next, cmd := model.Update(msg)
	if next != nil {
		w.replace(next)
	}
	w.capture()

	return w, cmd
}

func (w *modelWrapper) View() tea.View {
	view := w.currentModel().View()
	w.mu.Lock()
	w.viewOutput = view.Content
	w.mu.Unlock()
	return view
}

func (w *modelWrapper) currentModel() tea.Model {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.model
}

func (w *modelWrapper) replace(model tea.Model) {
	if model == nil {
		return
	}
	if wrapper, ok := model.(*modelWrapper); ok {
		model = wrapper.currentModel()
	}

	w.mu.Lock()
	w.model = model
	w.mu.Unlock()
}

func (w *modelWrapper) capture() {
	_ = w.View()
}

func (w *modelWrapper) output() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.viewOutput
}

func (w *modelWrapper) messages() []tea.Msg {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]tea.Msg(nil), w.observedMsgs...)
}
