// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import "charm.land/lipgloss/v2"

// The output only styles text, never changes it, so it reads the same with
// colors stripped. It uses the 16 ANSI colors, which follow the terminal's own
// theme, so it works on light and dark backgrounds alike.
var (
	styleBold      = lipgloss.NewStyle().Bold(true)
	styleFaint     = lipgloss.NewStyle().Faint(true)
	styleHeading   = lipgloss.NewStyle().Foreground(lipgloss.Magenta).Bold(true)
	styleRef       = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	styleHighlight = lipgloss.NewStyle().Foreground(lipgloss.Cyan).Bold(true)
	styleOK        = lipgloss.NewStyle().Foreground(lipgloss.Green)
	styleWarn      = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	styleFail      = lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true)
	styleRemoved   = lipgloss.NewStyle().Foreground(lipgloss.Red)
	styleSuccess   = lipgloss.NewStyle().Foreground(lipgloss.Green).Bold(true)
	stylePartial   = lipgloss.NewStyle().Foreground(lipgloss.Yellow).Bold(true)
)

// warning is the label that starts every warning line.
var warning = styleWarn.Render("warning:")
