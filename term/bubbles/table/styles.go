// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The glyphs [DefaultStyles] draws in the gutter.
const (
	DefaultCursorGlyph = " ›"
	DefaultMarkGlyph   = " ●"
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
	// and Marked of a marked row. A nil Cursor or Marked leaves the row with
	// the background it would have had otherwise, so the gutter's glyph alone
	// shows it.
	Stripe, Hover, Cursor, CursorCell, Marked color.Color
	// Gutter is the style of the cursor and mark glyphs left of the rows.
	Gutter lipgloss.Style
	// CursorText is laid over Cell, and a column's own style, on the
	// selected row, and CursorGutter over Gutter, such as to keep them
	// readable on a strong Cursor fill. Their padding is ignored.
	CursorText, CursorGutter lipgloss.Style
	// CursorGlyph is drawn in the gutter of the selected row, and MarkGlyph
	// in that of a marked row, which wins when the row is both. Empty is the
	// default, [DefaultCursorGlyph] or [DefaultMarkGlyph]. The gutter is as
	// wide as the widest glyph the table can show, and narrower glyphs are
	// padded on the right.
	CursorGlyph, MarkGlyph string
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
		CursorGlyph: DefaultCursorGlyph,
		MarkGlyph:   DefaultMarkGlyph,
		Thumb:       lipgloss.NewStyle().Foreground(lipgloss.Blue),
		Empty:       lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Yellow),
	}
}
