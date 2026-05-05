// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type simpleViewer interface {
	View() string
}

type simpleUpdater interface {
	Update(uv.Event) tea.Cmd
}

type replacementUpdater[T any] interface {
	Update(uv.Event) (T, tea.Cmd)
}

type initializer interface {
	Init() tea.Cmd
}

type componentWrapper[M any] struct {
	tb    testing.TB
	model M
}

func (cw *componentWrapper[M]) validate() {
	cw.tb.Helper()

	if _, ok := any(cw.model).(simpleViewer); !ok {
		cw.tb.Fatalf("model must implement View() string")
	}
	_, supportsCommandUpdate := any(cw.model).(simpleUpdater)
	_, supportsReplacementUpdate := any(cw.model).(replacementUpdater[M])
	if !supportsCommandUpdate && !supportsReplacementUpdate {
		cw.tb.Fatalf("model must implement Update(uv.Msg) tea.Cmd or Update(uv.Msg) (%T, tea.Cmd)", cw.model)
	}
}

func (cw *componentWrapper[M]) Init() tea.Cmd {
	cw.tb.Helper()
	if m, ok := any(cw.model).(initializer); ok {
		return m.Init()
	}
	return nil
}

func (cw *componentWrapper[M]) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	cw.tb.Helper()
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
	cw.tb.Helper()
	viewer, ok := any(cw.model).(simpleViewer)
	if !ok {
		panic("model must implement View() string")
	}

	v := tea.NewView(viewer.View())
	v.AltScreen = false
	return v
}
