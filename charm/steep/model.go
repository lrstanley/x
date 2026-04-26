// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"errors"
	"io"
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
	output  *bytes.Buffer

	resultMu sync.RWMutex
	result   *runResult
	done     chan runResult
}

// NewModel starts a Bubble Tea program for model and returns a test harness.
func NewModel(tb testing.TB, model tea.Model, opts ...Option) *Model {
	tb.Helper()

	cfg := collectOptions(opts...)
	wrapper := newModelWrapper(model)
	buf := &bytes.Buffer{}

	programOpts := append([]tea.ProgramOption{}, cfg.programOpts...)
	programOpts = append(programOpts,
		tea.WithInput(nil),
		tea.WithOutput(buf),
		tea.WithoutSignals(),
		tea.WithWindowSize(cfg.width, cfg.height),
	)

	h := &Model{
		program: tea.NewProgram(wrapper, programOpts...),
		wrapper: wrapper,
		output:  buf,
		done:    make(chan runResult, 1),
	}

	tb.Cleanup(func() {
		_ = h.Quit()
		h.WaitFinished(tb)
	})

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

// FinalOutput waits for the Bubble Tea program to finish and returns the last
// captured output.
func (m *Model) FinalOutput(tb testing.TB, opts ...Option) io.Reader {
	tb.Helper()
	m.waitFinished(tb, opts...)
	return m.Output()
}

// Output returns the last captured output.
func (m *Model) Output() io.Reader {
	return m.output
}

// FinalView waits for the Bubble Tea program to finish and returns the last
// captured view content.
func (m *Model) FinalView(tb testing.TB, opts ...Option) string {
	tb.Helper()
	m.waitFinished(tb, opts...)
	m.wrapper.mu.RLock()
	defer m.wrapper.mu.RUnlock()
	return m.wrapper.lastViewSnapshot
}

// FinalModel waits for the Bubble Tea program to finish and returns the latest
// underlying root model.
func (m *Model) FinalModel(tb testing.TB, opts ...Option) tea.Model {
	tb.Helper()
	m.waitFinished(tb, opts...)
	return m.wrapper.currentModel()
}

// Messages returns a copy of messages observed by the underlying model.
func (m *Model) Messages() []tea.Msg {
	return m.wrapper.messages()
}

// View invokes the current underlying models View method and returns the result.
func (m *Model) View() string {
	return m.wrapper.View().Content
}

// WaitFinished waits for the Bubble Tea program to finish.
func (m *Model) WaitFinished(tb testing.TB, opts ...Option) {
	tb.Helper()
	m.waitFinished(tb, opts...)
}

// WaitSettleMessages waits until no messages have been observed for the
// configured settle timeout. See also [WithSettleTimeout], [WithCheckInterval],
// and [WithTimeout].
func (m *Model) WaitSettleMessages(tb testing.TB, opts ...Option) *Model {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)

	for {
		m.wrapper.mu.RLock()
		updateCount, lastReceivedMessage := len(m.wrapper.observedMsgs), m.wrapper.lastReceivedMessage
		m.wrapper.mu.RUnlock()
		now := time.Now()
		quietFor := now.Sub(lastReceivedMessage)

		if quietFor >= cfg.settleTimeout {
			return m
		}

		remainingTimeout := deadline.Sub(now)
		if remainingTimeout <= 0 {
			tb.Fatalf(
				"timeout waiting for Update() to settle after %s; last update was %s ago after %d update(s)",
				cfg.timeout,
				quietFor,
				updateCount,
			)
			return m
		}

		time.Sleep(min(cfg.checkInterval, cfg.settleTimeout-quietFor, remainingTimeout))
	}
}

// WaitSettleView waits until the rendered view string has not changed for the
// configured settle timeout. It polls [Model.View] and compares each result to
// the previous sample. See also [WithSettleTimeout], [WithCheckInterval], and
// [WithTimeout].
func (m *Model) WaitSettleView(tb testing.TB, opts ...Option) *Model {
	tb.Helper()
	WaitSettleView(tb, m, opts...)
	return m
}

// WaitForBytes waits until condition returns true for the latest view output.
func (m *Model) WaitForBytes(tb testing.TB, condition func(view []byte) bool, opts ...Option) []byte {
	tb.Helper()
	return WaitFor(tb, m, condition, opts...)
}

// WaitForString waits until condition returns true for the latest view output.
func (m *Model) WaitForString(tb testing.TB, condition func(view string) bool, opts ...Option) string {
	tb.Helper()
	return WaitFor(tb, m, condition, opts...)
}

// WaitContainsBytes waits until output contains contents.
func (m *Model) WaitContainsBytes(tb testing.TB, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitContainsBytes(tb, m, contents, opts...)
}

// WaitContainsString waits until output contains contents.
func (m *Model) WaitContainsString(tb testing.TB, contents string, opts ...Option) string {
	tb.Helper()
	return WaitContainsString(tb, m, contents, opts...)
}

// WaitContainsStrings waits until output contains all contents.
func (m *Model) WaitContainsStrings(tb testing.TB, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitContainsStrings(tb, m, contents, opts...)
}

// WaitNotContainsBytes waits until output contains none of the contents.
func (m *Model) WaitNotContainsBytes(tb testing.TB, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitNotContainsBytes(tb, m, contents, opts...)
}

// WaitNotContainsString waits until output contains none of the contents.
func (m *Model) WaitNotContainsString(tb testing.TB, contents string, opts ...Option) string {
	tb.Helper()
	return WaitNotContainsString(tb, m, contents, opts...)
}

// WaitNotContainsStrings waits until output contains none of the contents.
func (m *Model) WaitNotContainsStrings(tb testing.TB, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitNotContainsStrings(tb, m, contents, opts...)
}

// ExpectStringContains fails the test unless all substrings appear in output.
func (m *Model) ExpectStringContains(tb testing.TB, substr ...string) *Model {
	tb.Helper()
	expectStringContains(tb, m, substr...)
	return m
}

// ExpectStringNotContains fails the test if any substring appears in output.
func (m *Model) ExpectStringNotContains(tb testing.TB, substr ...string) *Model {
	tb.Helper()
	expectStringNotContains(tb, m, substr...)
	return m
}

// ExpectHeight fails the test unless output has height rows.
func (m *Model) ExpectHeight(tb testing.TB, height int) *Model {
	tb.Helper()
	expectHeight(tb, m, height)
	return m
}

// ExpectWidth fails the test unless output has width columns.
func (m *Model) ExpectWidth(tb testing.TB, width int) *Model {
	tb.Helper()
	expectWidth(tb, m, width)
	return m
}

// ExpectDimensions fails the test unless output has width columns and height rows.
func (m *Model) ExpectDimensions(tb testing.TB, width, height int) *Model {
	tb.Helper()
	expectDimensions(tb, m, width, height)
	return m
}

func (m *Model) waitFinished(tb testing.TB, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(opts...)
	m.resultMu.RLock()
	if result := m.result; result != nil {
		m.resultMu.RUnlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		return
	}
	m.resultMu.RUnlock()

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
