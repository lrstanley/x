// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type stringViewer interface {
	View() string
}

type commandUpdater interface {
	Update(tea.Msg) tea.Cmd
}

type replacementUpdater interface {
	Update(tea.Msg) (any, tea.Cmd)
}

// ViewModel drives a non-root model that renders with View() string.
type ViewModel struct {
	model        any
	commandLimit int
	messages     []tea.Msg
}

// NewViewModel creates a harness for a model with View() string.
func NewViewModel(tb testing.TB, model any, opts ...Option) *ViewModel {
	tb.Helper()

	if _, ok := model.(stringViewer); !ok {
		tb.Fatalf("model must implement View() string")
	}
	_, supportsCommandUpdate := model.(commandUpdater)
	_, supportsReplacementUpdate := model.(replacementUpdater)
	if !supportsCommandUpdate && !supportsReplacementUpdate {
		tb.Fatalf("model must implement Update(tea.Msg) tea.Cmd or Update(tea.Msg) (any, tea.Cmd)")
	}

	cfg := collectOptions(opts...)
	return &ViewModel{
		model:        model,
		commandLimit: cfg.commandLimit,
	}
}

// Send sends msg to the model and synchronously processes returned commands.
func (m *ViewModel) Send(msg tea.Msg) {
	queue := []tea.Msg{msg}
	processed := 0

	for len(queue) > 0 && processed < m.commandLimit {
		next := queue[0]
		queue = queue[1:]
		processed++

		if cmd := m.update(next); cmd != nil {
			if cmdMsg := cmd(); cmdMsg != nil {
				queue = append(queue, cmdMsg)
			}
		}
	}
}

// Type sends s as a sequence of key press messages.
func (m *ViewModel) Type(s string) {
	for _, r := range s {
		m.Send(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

// Quit sends a Bubble Tea quit message to the model.
func (m *ViewModel) Quit() error {
	m.Send(tea.Quit())
	return nil
}

// WaitFinished is a no-op for synchronous generic view models.
func (m *ViewModel) WaitFinished(tb testing.TB, _ ...Option) {
	tb.Helper()
}

// FinalOutput returns the current view output.
func (m *ViewModel) FinalOutput(tb testing.TB, _ ...Option) []byte {
	tb.Helper()
	return m.outputBytes()
}

// FinalModel returns the current underlying model.
func (m *ViewModel) FinalModel(tb testing.TB, _ ...Option) any {
	tb.Helper()
	return m.model
}

// WaitFor waits until condition returns true for the current view output.
func (m *ViewModel) WaitFor(tb testing.TB, condition func(bts []byte) bool, opts ...Option) []byte {
	tb.Helper()
	return WaitFor(tb, m, condition, opts...)
}

// WaitContains waits until output contains all substrings.
func (m *ViewModel) WaitContains(tb testing.TB, substr ...[]byte) []byte {
	tb.Helper()
	return WaitContains(tb, m, substr...)
}

// WaitContainsString waits until output contains all substrings.
func (m *ViewModel) WaitContainsString(tb testing.TB, substr ...string) []byte {
	tb.Helper()
	return WaitContainsString(tb, m, substr...)
}

// WaitNotContains waits until output contains none of the substrings.
func (m *ViewModel) WaitNotContains(tb testing.TB, substr ...[]byte) []byte {
	tb.Helper()
	return WaitNotContains(tb, m, substr...)
}

// WaitNotContainsString waits until output contains none of the substrings.
func (m *ViewModel) WaitNotContainsString(tb testing.TB, substr ...string) []byte {
	tb.Helper()
	return WaitNotContainsString(tb, m, substr...)
}

// ExpectStringContains fails the test unless all substrings appear in output.
func (m *ViewModel) ExpectStringContains(tb testing.TB, substr ...string) {
	tb.Helper()
	expectStringContains(tb, m, substr...)
}

// ExpectStringNotContains fails the test if any substring appears in output.
func (m *ViewModel) ExpectStringNotContains(tb testing.TB, substr ...string) {
	tb.Helper()
	expectStringNotContains(tb, m, substr...)
}

// ExpectHeight fails the test unless output has height rows.
func (m *ViewModel) ExpectHeight(tb testing.TB, height int) {
	tb.Helper()
	expectHeight(tb, m, height)
}

// ExpectWidth fails the test unless output has width columns.
func (m *ViewModel) ExpectWidth(tb testing.TB, width int) {
	tb.Helper()
	expectWidth(tb, m, width)
}

// ExpectDimensions fails the test unless output has width columns and height rows.
func (m *ViewModel) ExpectDimensions(tb testing.TB, width, height int) {
	tb.Helper()
	expectDimensions(tb, m, width, height)
}

// Messages returns a copy of messages sent to the model.
func (m *ViewModel) Messages() []tea.Msg {
	return append([]tea.Msg(nil), m.messages...)
}

func (m *ViewModel) outputBytes() []byte {
	viewer, ok := m.model.(stringViewer)
	if !ok {
		return nil
	}
	return []byte(viewer.View())
}

func (m *ViewModel) update(msg tea.Msg) tea.Cmd {
	m.messages = append(m.messages, msg)

	if updater, ok := m.model.(replacementUpdater); ok {
		next, cmd := updater.Update(msg)
		if next != nil {
			m.model = next
		}
		return cmd
	}

	updater, ok := m.model.(commandUpdater)
	if !ok {
		return nil
	}
	return updater.Update(msg)
}
