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

type simpleUpdater interface {
	Update(tea.Msg) tea.Cmd
}

type replacementUpdater[T any] interface {
	Update(tea.Msg) (T, tea.Cmd)
}

type initializer interface {
	Init() tea.Cmd
}

type componentWrapper[M any] struct {
	model M
}

func (cw *componentWrapper[M]) validate(tb testing.TB) {
	tb.Helper()

	if _, ok := any(cw.model).(View); !ok {
		tb.Fatalf("model must implement View() string")
	}
	_, supportsCommandUpdate := any(cw.model).(simpleUpdater)
	_, supportsReplacementUpdate := any(cw.model).(replacementUpdater[M])
	if !supportsCommandUpdate && !supportsReplacementUpdate {
		tb.Fatalf("model must implement Update(tea.Msg) tea.Cmd or Update(tea.Msg) (%T, tea.Cmd)", cw.model)
	}
}

func (cw *componentWrapper[M]) Init() tea.Cmd {
	if m, ok := any(cw.model).(initializer); ok {
		return m.Init()
	}
	return nil
}

func (cw *componentWrapper[M]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := any(cw.model).(type) {
	case replacementUpdater[M]:
		next, cmd := m.Update(msg)
		cw.model = next
		return cw, cmd
	case simpleUpdater:
		return cw, m.Update(msg)
	}

	return cw, nil
}

func (cw *componentWrapper[M]) View() tea.View {
	viewer, ok := any(cw.model).(View)
	if !ok {
		panic("model must implement View() string")
	}

	v := tea.NewView(viewer.View())
	v.AltScreen = true
	return v
}
