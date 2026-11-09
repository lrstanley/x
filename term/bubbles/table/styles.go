// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Styles is how a [Model] looks. Start from [DefaultStyles].
type Styles struct {
	// Border is the style of the border and the column separators, whose
	// foreground the labels on the border use too.
	Border lipgloss.Style
	// Title is the style of the title on the border.
	Title lipgloss.Style
	// Header is the style of the column titles, and HeaderHover of one that
	// sorts the table under the pointer.
	Header, HeaderHover lipgloss.Style
	// Cell is the style of every cell, before a column's own.
	Cell lipgloss.Style
	// Stripe is the background of every other row, Hover of the row under the
	// pointer, Cursor of the selected row, CursorCell of the selected cell,
	// and Marked of a marked row.
	Stripe, Hover, Cursor, CursorCell, Marked color.Color
	// Gutter is the style of the cursor and mark glyphs left of the rows.
	Gutter lipgloss.Style
	// Thumb is the style of the scrollbar's thumb.
	Thumb lipgloss.Style
	// Empty is the style of the message shown when there are no rows.
	Empty lipgloss.Style
}

// DefaultStyles returns styles in the terminal's basic colors.
func DefaultStyles() Styles {
	return Styles{
		Border:      lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).BorderForeground(lipgloss.BrightBlack),
		Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Blue),
		Header:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Magenta),
		HeaderHover: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Black).Background(lipgloss.Magenta),
		Stripe:      lipgloss.Color("236"),
		Hover:       lipgloss.Color("238"),
		Cursor:      lipgloss.Color("24"),
		CursorCell:  lipgloss.Color("31"),
		Marked:      lipgloss.Color("22"),
		Gutter:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Blue),
		Thumb:       lipgloss.NewStyle().Foreground(lipgloss.Blue),
		Empty:       lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Yellow),
	}
}
