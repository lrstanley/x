// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package integration_test

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/fx"
	"github.com/lrstanley/x/term/steep"
)

type item int

// app is a small program using every part of the package together: a
// collapsible sidebar of clickable, focusable items, and a modal help dialog
// that slides in over a dimmed backdrop and closes on a click outside it.
type app struct {
	ui       *bloom.Root
	w, h     int
	selected int
	focus    bloom.Focus
	help     anim.Presence
}

func newApp() *app {
	return &app{ui: bloom.New(), selected: -1, help: anim.NewPresence(anim.Duration(120 * time.Millisecond))}
}

func (m *app) Init() tea.Cmd { return m.ui.Init() }

func (m *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.ui.Update(msg)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab":
			m.focus.Next(m.ui.Frame())
		case "?":
			cmd := m.toggleHelp()
			return m, cmd
		}
	case bloom.MouseMsg:
		if _, ok := msg.Msg.(tea.MouseClickMsg); !ok {
			break
		}
		if msg.Outside {
			cmd := m.toggleHelp()
			return m, cmd
		}
		if it, ok := msg.Target[item](); ok {
			m.selected = int(it)
		}
		m.focus.Click(msg)
	}
	return m, m.help.Update(msg)
}

func (m *app) toggleHelp() tea.Cmd {
	if m.help.Shown() {
		m.focus.Pop()
	} else {
		m.focus.Push("help")
	}
	return m.help.Toggle()
}

func (m *app) View() tea.View {
	items := make([]bloom.Child, 0, 5)
	for i := range 4 {
		label := fmt.Sprintf(" item %02d", i)
		if m.focus.Is(item(i)) {
			label = ">" + label[1:]
		}
		items = append(items, bloom.Text(label).ID(item(i)).Focusable())
	}
	items = append(items, bloom.Space())

	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	base := bloom.Column(
		bloom.Fill(bloom.Row(
			bloom.Fixed(14, bloom.Box(border, bloom.Column(items...))).ID("sidebar").Collapsible(1),
			bloom.Fill(bloom.Box(border, bloom.Text(fmt.Sprintf("selected: %d", m.selected)))).Min(24),
		)),
		bloom.Text(fmt.Sprintf("focus: %v", m.focus.Current())),
	)

	var layers []bloom.LayerNode
	if m.help.Visible() {
		layers = append(layers, bloom.Layer(bloom.Box(border.Padding(0, 1), bloom.Text("Keyboard help\n\ntab  focus\n?    close"))).
			ID("help").
			Modal().
			Backdrop(fx.Dim{Amount: 0.5}).
			Transition(m.help.Progress(), fx.Slide{From: fx.Bottom}, fx.Fade{}))
	}

	v := m.ui.View(m.w, m.h, bloom.Overlay(base, layers...))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func TestApp(t *testing.T) {
	t.Parallel()

	h := steep.NewHarness(t, newApp(), steep.WithWindowSize(50, 12), steep.WithTimeout(10*time.Second))
	h.Wait("item 03")

	// The sidebar's border is the first row and column, so item 2 is at 2,3.
	h.LeftClick(3, 3).Wait("selected: 2").Wait("focus: 2")

	h.Key("tab").Wait(">item 03")
	h.Key("tab").Wait(">item 00")

	h.Key("?").Wait("Keyboard help").WaitSettle()
	h.AssertSnapshot()

	// Focus is confined to the dialog, which has nothing to focus.
	h.Key("tab").Wait("focus: <nil>")

	// A click on the backdrop closes the dialog and gives focus back.
	h.LeftClick(1, 1).WaitNot("Keyboard help").Wait("focus: 0")

	// The harness has no pseudo-terminal to signal a resize with, so it is
	// reported in band, as steep's own resize tests do.
	if _, err := h.Session().Write([]byte(ansi.SetModeInBandResize)); err != nil {
		t.Fatal(err)
	}
	h.Resize(30, 12).WaitNot("item 00").Wait("selected: 2")
	h.QuitProgram()
	h.WaitFinished()
}
