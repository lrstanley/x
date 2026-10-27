// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"image"
	"image/color"
	"time"

	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt"
)

// ColorSource supplies palette and opacity inputs for color resolution.
type ColorSource interface {
	Palette() types.Palette
	State() vt.State
	FaintFactor() float64
	BackgroundOpacity() float64
	BackgroundOpacityCells() bool
}

// FrameSource supplies per-draw frame configuration for raster helpers.
type FrameSource interface {
	ColorSource
	// At is the offset into the timeline the frame is drawn at. Blinking
	// and other animations derive their phase from it.
	At() time.Duration
	CursorBlinkSpeed() time.Duration
	Metrics() types.Metrics
}

// CellFrameSource extends [FrameSource] with grid layout and per-cell color resolution.
type CellFrameSource interface {
	FrameSource
	GridBounds() image.Rectangle
	CellColorsNRGBA(cell *vt.Cell) (fg, bg color.NRGBA)
	ResolvePaletteColor(c color.Color) color.Color
}

// WindowSource supplies window geometry and effect knobs for post-processing.
type WindowSource interface {
	WindowBounds() image.Rectangle
	MarginColor() color.Color
	FocusDimming() float64
	BorderRadius() units.Px
}
