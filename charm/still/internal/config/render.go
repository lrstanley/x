// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"image"
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
)

// ColorSource supplies palette and opacity inputs for color resolution.
type ColorSource interface {
	Palette() types.Palette
	EmulatorState() (types.EmulatorState, bool)
	FaintFactor() float64
	BackgroundOpacity() float64
	BackgroundOpacityCells() bool
}

// FrameSource supplies per-draw frame configuration for raster helpers.
type FrameSource interface {
	ColorSource
	Now() time.Time
	CursorBlinkSpeed() time.Duration
	Metrics() types.Metrics
}

// CellFrameSource extends [FrameSource] with grid layout and per-cell color resolution.
type CellFrameSource interface {
	FrameSource
	GridBounds() image.Rectangle
	CellColorsNRGBA(cell *uv.Cell) (fg, bg color.NRGBA)
	ResolvePaletteColor(c color.Color) color.Color
}

// WindowSource supplies window geometry and effect knobs for post-processing.
type WindowSource interface {
	WindowBounds() image.Rectangle
	MarginColor() color.Color
	FocusDimming() float64
	BorderRadius() units.Px
}
