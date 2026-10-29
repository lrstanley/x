// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import "image/color"

// Attr is a bit set of cell text attributes.
type Attr uint8

// Cell attribute bits. Values are stable so snapshots and draw tests keep
// the same numeric style.
const (
	AttrBold Attr = 1 << iota
	AttrFaint
	AttrItalic
	AttrBlink
	AttrRapidBlink
	AttrReverse
	AttrConceal
	AttrStrikethrough
)

// Underline is the SGR underline style drawn for a cell.
type Underline uint8

// Underline styles. The numeric order matches ANSI underline selectors.
const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// CursorShape is the caret shape reported with a screen.
type CursorShape uint8

// Cursor shapes. Block is the default.
const (
	CursorBlock CursorShape = iota
	CursorUnderline
	CursorBar
	CursorBlockHollow
)

// Style is the visual style of one cell.
type Style struct {
	// Fg is the text color. Nil means the terminal default, not transparent black.
	Fg color.Color
	// Bg is the background color. Nil means the terminal default.
	Bg color.Color
	// UnderlineColor is the underline color. Nil uses Fg.
	UnderlineColor color.Color
	// Underline is the line style. UnderlineNone draws no line.
	Underline Underline
	// Attrs is the attribute bit set.
	Attrs Attr
}

// Link is an OSC 8 hyperlink stored on a cell.
type Link struct {
	// URL is the OSC 8 target. Empty means the cell has no link.
	URL string
	// Params is the OSC 8 parameter string, such as an id. It may be empty.
	Params string
}

// Cell is one grid cell. Content is one grapheme cluster. Width 0 marks the
// spacer tail of a wide grapheme and is not drawn.
type Cell struct {
	// Content is one grapheme cluster. A width-0 spacer may be empty.
	Content string
	// Style is the SGR style decoded for this cell.
	Style Style
	// Link is the OSC 8 hyperlink, if the cell has one.
	Link Link
	// Width is the number of columns this grapheme occupies. Zero marks
	// the spacer tail of a wide grapheme and is not drawn on its own.
	Width int
}

// Indexed is a palette index from 0 through 255. A drawer that has a theme
// palette should map the index before falling back to [Indexed.RGBA].
type Indexed uint8

// RGBA returns a default sRGB approximation for the index.
func (c Indexed) RGBA() (r, g, b, a uint32) {
	rr, gg, bb := indexedRGB(uint8(c))
	return uint32(rr) * 0x101, uint32(gg) * 0x101, uint32(bb) * 0x101, 0xffff
}

// EmptyCell is a blank cell: one space, width 1, no style.
var EmptyCell = Cell{Content: " ", Width: 1}

func indexedRGB(i uint8) (r, g, b uint8) {
	if i < 16 {
		base := [16][3]uint8{
			{0, 0, 0},
			{205, 0, 0},
			{0, 205, 0},
			{205, 205, 0},
			{0, 0, 238},
			{205, 0, 205},
			{0, 205, 205},
			{229, 229, 229},
			{127, 127, 127},
			{255, 0, 0},
			{0, 255, 0},
			{255, 255, 0},
			{92, 92, 255},
			{255, 0, 255},
			{0, 255, 255},
			{255, 255, 255},
		}
		return base[i][0], base[i][1], base[i][2] //nolint:gosec // i < 16, the length of base.
	}
	if i >= 232 {
		v := uint8((8 + (int(i)-232)*10) & 0xff)
		return v, v, v
	}
	i -= 16
	levels := [6]uint8{0, 95, 135, 175, 215, 255}
	return levels[(i/36)%6], levels[(i/6)%6], levels[i%6]
}

// Clone returns a copy of c.
func (c *Cell) Clone() *Cell {
	if c == nil {
		return nil
	}
	n := *c
	return &n
}

// Empty turns c into [EmptyCell] while keeping the cell's storage.
func (c *Cell) Empty() {
	*c = EmptyCell
}
