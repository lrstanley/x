// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/charm/layout"
)

type statusbarModel struct {
	uuid
}

func newStatusbar() *statusbarModel {
	return &statusbarModel{}
}

func (m *statusbarModel) Init() tea.Cmd {
	return nil
}

func (m *statusbarModel) Update(_ tea.Msg) tea.Cmd {
	return nil
}

func (m *statusbarModel) View(availableWidth, _ int) layout.Layout {
	entry := lipgloss.NewStyle().
		Background(theme.BrightBlack).
		Foreground(lipgloss.Lighten(theme.Fg, 0.3)).
		Padding(0, 1, 0, 1)

	status := entry.Background(theme.Cyan).Render("STATUS")
	encoding := entry.Render("UTF-8")
	fishCake := entry.Render("🍥 Fish Cake")

	return layout.Frame(
		lipgloss.NewStyle().Height(1).Width(availableWidth).Background(theme.BrightBlack),
		layout.Horizontal(
			status,
			encoding,
			layout.Space(),
			fishCake,
		),
	)
}
