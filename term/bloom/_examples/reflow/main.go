// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command reflow shows automatic layout animation: a spring sidebar, a
// sortable list, a percentage split, a details panel, and an anchored layer
// following the selected item. The model holds only application state;
// ID'd slots and layers use Animate to follow their layout targets.
//
// Keys: b toggles the sidebar, s reverses the list, r cycles the split ratio,
// d toggles details, up/down select an item, and q or ctrl+c quits.
package main

import (
	"fmt"
	"os"
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
)

var (
	panel         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#565f89"))
	heading       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	itemStyle     = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("#c0caf5"))
	selectedStyle = itemStyle.Background(lipgloss.Color("#414868"))
	markerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a")).Bold(true)
)

type model struct {
	ui                         *bloom.Root
	w, h                       int
	sidebar, details, reversed bool
	ratio, selected            int
}

var names = []string{"Atlas", "Birch", "Cedar", "Delta", "Ember"}

func newModel() *model {
	return &model{ui: bloom.New(), sidebar: true, details: true, ratio: 30}
}

func (m *model) Init() tea.Cmd { return m.ui.Init() }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.ui.Update(msg)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "b":
			m.sidebar = !m.sidebar
		case "s":
			m.reversed = !m.reversed
		case "r":
			m.ratio += 20
			if m.ratio > 70 {
				m.ratio = 30
			}
		case "d":
			m.details = !m.details
		case "up":
			m.selected = (m.selected + len(names) - 1) % len(names)
		case "down":
			m.selected = (m.selected + 1) % len(names)
		}
	}
	return m, cmd
}

func (m *model) View() tea.View {
	sidebarWidth, detailsHeight := 0, 0
	if m.sidebar {
		sidebarWidth = 18
	}
	if m.details {
		detailsHeight = 5
	}
	order := []int{0, 1, 2, 3, 4}
	if m.reversed {
		slices.Reverse(order)
	}
	items := make([]bloom.Child, 0, len(order)+1)
	for _, i := range order {
		style := itemStyle
		if i == m.selected {
			style = selectedStyle
		}
		items = append(items, bloom.Fixed(2, bloom.Text(names[i]).Style(style)).
			ID(names[i]).Animate())
	}
	items = append(items, bloom.Space())
	base := bloom.Column(
		bloom.Text("Reflow / automatic layout animations").Style(heading),
		bloom.Fill(bloom.Row(
			bloom.Fixed(sidebarWidth, bloom.Box(panel, bloom.Text("Workspace\n\nProjects\nActivity\nSettings"))).
				ID("sidebar").Animate(anim.Spring(16, 0.85)),
			bloom.Fill(bloom.Column(
				bloom.Fill(bloom.Row(
					bloom.Percent(m.ratio, bloom.Box(panel, bloom.Column(items...))).ID("list").Animate(),
					bloom.Fill(bloom.Box(panel, bloom.Text("Preview\n\nChildren reflow at the current visual size.\n\nTry b, s, r and d."))).ID("preview").Animate(),
				)),
				bloom.Fixed(detailsHeight, bloom.Box(panel, bloom.Text("Details\nSelected: "+names[m.selected]))).ID("details").Animate(),
			)).ID("content").Animate(),
		)),
		bloom.Text(fmt.Sprintf("sidebar: %t | reverse: %t | ratio: %d | details: %t | selected: %s",
			m.sidebar, m.reversed, m.ratio, m.details, names[m.selected])).Truncate(),
		bloom.Text("b sidebar / s sort / r ratio / d details / up,down select / q quit").Truncate(),
	)
	v := m.ui.View(m.w, m.h, bloom.Overlay(base,
		bloom.Layer(bloom.Text("< selected").Style(markerStyle)).Size(10, 1).
			AnchorTo(names[m.selected], bloom.Right).ID("selection").Animate(anim.Spring(20, 1)),
	))
	v.AltScreen = true
	return v
}

func main() {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
