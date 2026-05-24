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
	DPI                    units.DPI
	FontSize               units.Pt
	CellWidth              units.Px
	CellHeight             units.Px
	FontBaseline           units.Px
	UnderlinePosition      units.Px
	UnderlineThickness     units.Px
	StrikethroughPosition  units.Px
	StrikethroughThickness units.Px
	CursorThickness        units.Px
	CursorHeight           units.Px
	BoxThickness           units.Px
	IconHeight             units.Px
	IconHeightSingle       units.Px
	FaceWidth              units.FractionalPx
	FaceHeight             units.FractionalPx
	FaceY                  units.FractionalPx
}

// CellSize returns the pixel size of one terminal cell.
func (m Metrics) CellSize() image.Point {
	return image.Pt(m.CellWidth.Int(), m.CellHeight.Int())
}

// Palette contains sparse terminal colors used when rendered cells or emulator
// state do not provide more specific colors.
type Palette struct {
	Indexed           map[int]color.Color
	DefaultForeground color.Color
	DefaultBackground color.Color
	Cursor            color.Color
	CursorText        color.Color
	Margin            color.Color
}

// EmulatorState contains optional terminal state that is not carried by
// [uv.Screen].
type EmulatorState struct {
	Title           string
	Focused         bool
	AltScreen       bool
	CursorVisible   bool
	CursorX         int
	CursorY         int
	CursorColor     color.Color
	CursorStyle     uv.CursorShape
	CursorBlink     bool
	FgColor         color.Color
	BgColor         color.Color
	ScrollbackCount int
}
