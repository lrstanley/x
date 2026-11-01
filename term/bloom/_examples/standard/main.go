// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command standard is the usual shape of a Bubble Tea program: a sidebar, a
// reader, and a status bar. The sidebar builds its own layout with Layout,
// the reader is a plain component with a View string, and the status bar
// sizes itself.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/bloom"
)

var (
	panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#565f89"))
	item     = lipgloss.NewStyle().Padding(0, 1)
	selected = item.Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#7aa2f7"))
)

type page int

var pages = []string{"Home", "Settings", "About"}

// sidebar lists the pages; it is a [bloom.Layouter], so its items fill the
// sidebar's width.
type sidebar struct{ selected int }

func (s sidebar) View() string { return bloom.Render(s.Layout(bloom.Context{}), 0, 0) }

func (s sidebar) Layout(bloom.Context) bloom.Child {
	items := make([]bloom.Child, 0, len(pages)+2)
	for i, name := range pages {
		st := item
		if i == s.selected {
			st = selected
		}
		items = append(items, bloom.Text(name).Style(st).ID(page(i)))
	}
	return bloom.Box(panel, bloom.Column(append(items, bloom.Space(), bloom.Text("v1.0").Style(item))...))
}

// reader is a plain Bubble Tea component.
type reader struct{ page int }

func (r reader) View() string {
	return fmt.Sprintf("# %s\n\n%s", pages[r.page], strings.Repeat("Lorem ipsum dolor sit amet. ", 3))
}

// statusbar knows its own height, so it measures itself.
type statusbar struct{ msg string }

func (s statusbar) View() string { return " " + s.msg }

func (statusbar) Measure(avail bloom.Size) bloom.Size { return bloom.Size{W: avail.W, H: 1} }

type model struct {
	ui      *bloom.Root
	w, h    int
	sidebar sidebar
	reader  reader
	status  statusbar
}

func newModel() *model {
	return &model{ui: bloom.New(), status: statusbar{msg: "click a page, or q to quit"}}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case bloom.MouseMsg:
		if click, ok := msg.Msg.(tea.MouseClickMsg); !ok || click.Button != tea.MouseLeft {
			break
		}
		if p, ok := msg.Target[page](); ok {
			m.sidebar.selected, m.reader.page = int(p), int(p)
			m.status.msg = "opened " + pages[p]
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, bloom.Column(
		bloom.Fill(bloom.Row(
			bloom.Fixed(20, m.sidebar).Collapsible(1),
			bloom.Fill(bloom.Box(panel.Padding(0, 1), m.reader)).Min(30),
		)),
		m.status,
	))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func main() {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
