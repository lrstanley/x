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

type initializer interface {
	Init() tea.Cmd
}

// NewViewModel starts a Bubble Tea program for a component model that renders
// with View() string.
func NewViewModel(tb testing.TB, model any, opts ...Option) *Model {
	tb.Helper()

	validateViewModel(tb, model)

	return NewModel(tb, &programViewModelBridge{model: model}, opts...)
}

func validateViewModel(tb testing.TB, model any) {
	tb.Helper()

	if _, ok := model.(stringViewer); !ok {
		tb.Fatalf("model must implement View() string")
	}
	_, supportsCommandUpdate := model.(commandUpdater)
	_, supportsReplacementUpdate := model.(replacementUpdater)
	if !supportsCommandUpdate && !supportsReplacementUpdate {
		tb.Fatalf("model must implement Update(tea.Msg) tea.Cmd or Update(tea.Msg) (any, tea.Cmd)")
	}
}

type programViewModelBridge struct {
	model any
}

func (b *programViewModelBridge) Init() tea.Cmd {
	if m, ok := b.model.(initializer); ok {
		return m.Init()
	}
	return nil
}

func (b *programViewModelBridge) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := b.model.(replacementUpdater); ok {
		next, cmd := m.Update(msg)
		if next != nil {
			b.model = next
		}
		return b, cmd
	}

	if m, ok := b.model.(commandUpdater); ok {
		return b, m.Update(msg)
	}

	return b, nil
}

func (b *programViewModelBridge) View() tea.View {
	viewer, ok := b.model.(stringViewer)
	if !ok {
		return tea.NewView("")
	}

	v := tea.NewView(viewer.View())
	v.AltScreen = false
	return v
}
