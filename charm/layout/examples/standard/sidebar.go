// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/lrstanley/x/charm/layout"
)

type item struct {
	uuid
	text     string
	selected bool
}

func (i *item) View(availableWidth, _ int) layout.Layer {
	style := lipgloss.NewStyle().Width(availableWidth).Height(1)
	if i.selected {
		style = style.Background(theme.Cyan).Foreground(lipgloss.Lighten(theme.Fg, 0.3))
	}
	return layout.NewLayer(i.UUID(), style.Render(" • "+i.text)).Z(1)
}

type newSelectionMsg struct {
	index int
}

type sidebarModel struct {
	uuid
	items []*item
	style lipgloss.Style
}

func newSidebar() *sidebarModel {
	return &sidebarModel{
		items: []*item{
			{text: "Home", selected: true},
			{text: "Settings"},
			{text: "About"},
		},
		style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(charmtone.Oyster),
	}
}

func (m *sidebarModel) Init() tea.Cmd {
	return nil
}

func (m *sidebarModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case layout.LayerMouseMsg:
		if _, ok := msg.MouseMsg.(tea.MouseClickMsg); !ok || msg.Mouse().Button != tea.MouseLeft {
			return nil
		}
		for i, item := range m.items {
			if item.UUID() == msg.LayerID {
				return func() tea.Msg {
					return newSelectionMsg{index: i}
				}
			}
		}
	case newSelectionMsg:
		for i, item := range m.items {
			item.selected = i == msg.index
		}
		return nil
	}
	return nil
}

func (m *sidebarModel) View() layout.Layout {
	layers := make([]any, 0, len(m.items))

	for _, item := range m.items {
		layers = append(layers, item)
	}

	layers = append(layers, layout.Space()) // TODO: this doesn't work.

	return layout.Frame(
		m.style,
		layout.Vertical(layers...),
	)
}
