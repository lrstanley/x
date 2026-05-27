// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package types holds shared terminal rendering data types.
package types //nolint:revive // shared terminal rendering data types

import (
	"image"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/units"
)

// Metrics describes the terminal grid geometry used for a draw.
type Metrics struct {
	// DPI is the display resolution used when converting points and fonts to
	// pixels.
	DPI units.DPI
	// FontSize is the primary font body size in typographic points.
	FontSize units.Pt
	// CellWidth is the horizontal size of one character cell in pixels.
	CellWidth units.Px
	// CellHeight is the vertical size of one character cell in pixels.
	CellHeight units.Px
	// FontBaseline is the distance from the bottom of the cell to the font
	// baseline in pixels.
	FontBaseline units.Px
	// UnderlinePosition is the distance from the top of the cell to the
	// underline placement in pixels.
	UnderlinePosition units.Px
	// UnderlineThickness is the underline stroke width in pixels.
	UnderlineThickness units.Px
	// StrikethroughPosition is the distance from the top of the cell to the
	// strikethrough placement in pixels.
	StrikethroughPosition units.Px
	// StrikethroughThickness is the strikethrough stroke width in pixels.
	StrikethroughThickness units.Px
	// CursorThickness is the width of the cursor bar or block edge in pixels.
	CursorThickness units.Px
	// CursorHeight is the vertical extent of the cursor in pixels (often the
	// cell height).
	CursorHeight units.Px
	// BoxThickness is the stroke width for drawn boxes and borders in pixels.
	BoxThickness units.Px
	// IconHeight is the target vertical band in pixels for scaling Nerd
	// Font–style icons.
	IconHeight units.Px
	// IconHeightSingle is the target vertical band in pixels for single-cell
	// nerd icons.
	IconHeightSingle units.Px
	// FaceWidth is the measured advance width of the grid font face in
	// fractional pixels (for centering).
	FaceWidth units.FractionalPx
	// FaceHeight is the line box height of the grid font (ascent + descent
	// + line gap) in fractional pixels.
	FaceHeight units.FractionalPx
	// FaceY is the vertical offset of the face box within the cell in
	// fractional pixels.
	FaceY units.FractionalPx
}

// CellSize returns the pixel size of one terminal cell.
func (m Metrics) CellSize() image.Point {
	return image.Pt(m.CellWidth.Int(), m.CellHeight.Int())
}

// Palette holds baseline colors for terminal rendering when a cell or control
// sequence does not specify a color. Indexed entries map ANSI palette indices
// (e.g. 0–255) to RGB values; other fields apply to UI chrome and defaults.
type Palette struct {
	// Indexed maps 8- or 256-color palette indices to colors.
	Indexed map[int]color.Color
	// DefaultForeground is the text color when no explicit foreground is set.
	DefaultForeground color.Color
	// DefaultBackground is the cell background when no explicit background is
	// set.
	DefaultBackground color.Color
	// Cursor is the fill or outline color of the text cursor.
	Cursor color.Color
	// CursorText is the foreground color of text drawn over the cursor region.
	CursorText color.Color
	// Margin is the color used for padding or gutter areas outside the terminal
	// grid.
	Margin color.Color
}

// EmulatorState snapshots host-terminal properties that influence how a session
// is drawn but are not represented on [uv.Screen], such as title, focus, cursor
// mode, and scrollback metadata.
type EmulatorState struct {
	// Title is the window or tab title reported by the terminal.
	Title string
	// Focused is true when the terminal emulator window has keyboard focus.
	Focused bool
	// AltScreen is true when the alternate screen buffer is active.
	AltScreen bool
	// CursorVisible is true when the caret should be shown (subject to blink).
	CursorVisible bool
	// CursorX is the cursor column in the active screen buffer (zero-based).
	CursorX int
	// CursorY is the cursor row in the active screen buffer (zero-based).
	CursorY int
	// CursorColor is the cursor color chosen by DECSCUSR or similar, if known.
	CursorColor color.Color
	// CursorStyle is the cursor shape (block, bar, underline, etc.).
	CursorStyle uv.CursorShape
	// CursorBlink is true when cursor blinking is enabled.
	CursorBlink bool
	// FgColor is the default foreground when the emulator resets attributes.
	FgColor color.Color
	// BgColor is the default background when the emulator resets attributes.
	BgColor color.Color
	// ScrollbackCount is the number of lines retained above the viewport, if
	// tracked.
	ScrollbackCount int
}
