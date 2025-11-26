// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

type readerModel struct {
	uuid
	items    []string
	selected int
	style    lipgloss.Style
}

func newReader() *readerModel {
	return &readerModel{
		items: []string{
			"Lorem ipsum dolor sit amet, consectetur adipiscing elit.\nSed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
			"The quick brown fox jumps over the lazy dog.",
			"The slow yellow fox jumps over the fast cat.",
		},
		style: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(charmtone.Oyster),
	}
}

func (m *readerModel) Init() tea.Cmd {
	return nil
}

func (m *readerModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case newSelectionMsg:
		m.selected = msg.index
		return nil
	}
	return nil
}

func (m *readerModel) View(availableWidth, availableHeight int) string {
	return m.style.
		Width(availableWidth).
		Height(availableHeight).
		Render(m.items[m.selected])
}
