// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"image"
	"image/color"
)

// WidthMeasurer reports how many monospaced columns a string occupies. The
// default [Terminal] uses [DefaultWidthMeasurer] when [Options.WidthMeasurer]
// is nil.
type WidthMeasurer interface {
	StringWidth(s string) int
}

// Underline selects an SGR underline style (values align with libghostty-vt
// GHOSTTY_SGR_UNDERLINE_*).
type Underline uint8

const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Text attribute flags for [Style.Attrs] (bitmask).
const (
	AttrBold = 1 << iota
	AttrFaint
	AttrItalic
	AttrBlink
	AttrRapidBlink
	AttrReverse
	AttrConceal
	AttrStrikethrough
)

// Style is the visual style for a terminal cell.
type Style struct {
	Fg             color.Color
	Bg             color.Color
	UnderlineColor color.Color
	Underline      Underline
	Attrs          uint8
}

// Link is an OSC 8-style hyperlink attached to a cell.
type Link struct {
	URL    string
	Params string
}

// Cell is one grid cell after resolving graphemes and width.
type Cell struct {
	Content string
	Style   Style
	Link    Link
	Width   int
}

// Rectangle is an axis-aligned rectangle in cell coordinates. Min is inclusive,
// Max is exclusive (same convention as [image.Rectangle]).
type Rectangle = image.Rectangle

// Point is an integer cell coordinate.
type Point = image.Point
