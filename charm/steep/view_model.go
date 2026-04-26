// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type View interface {
	View() string
}

type commandUpdater interface {
	Update(tea.Msg) tea.Cmd
}

type replacementUpdater[T any] interface {
	Update(tea.Msg) (T, tea.Cmd)
}

type initializer interface {
	Init() tea.Cmd
}

// NewViewModel starts a Bubble Tea program for a component model that renders
// with View() string.
func NewViewModel[M any](tb testing.TB, model M, opts ...Option) *Model {
	tb.Helper()

	m := &viewModelWrapper[M]{model: model}

	m.validate(tb)

	return NewModel(tb, m, opts...)
}

type viewModelWrapper[M any] struct {
	model M
}

func (b *viewModelWrapper[M]) validate(tb testing.TB) {
	tb.Helper()

	if _, ok := any(b.model).(View); !ok {
		tb.Fatalf("model must implement View() string")
	}
	_, supportsCommandUpdate := any(b.model).(commandUpdater)
	_, supportsReplacementUpdate := any(b.model).(replacementUpdater[M])
	if !supportsCommandUpdate && !supportsReplacementUpdate {
		tb.Fatalf("model must implement Update(tea.Msg) tea.Cmd or Update(tea.Msg) (%T, tea.Cmd)", b.model)
	}
}

func (b *viewModelWrapper[M]) Init() tea.Cmd {
	if m, ok := any(b.model).(initializer); ok {
		return m.Init()
	}
	return nil
}

func (b *viewModelWrapper[M]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := any(b.model).(replacementUpdater[M]); ok {
		next, cmd := m.Update(msg)
		b.model = next
		return b, cmd
	}

	if m, ok := any(b.model).(commandUpdater); ok {
		return b, m.Update(msg)
	}

	return b, nil
}

func (b *viewModelWrapper[M]) View() tea.View {
	viewer, ok := any(b.model).(View)
	if !ok {
		panic("model must implement View() string")
	}

	v := tea.NewView(viewer.View())
	v.AltScreen = true
	return v
}
