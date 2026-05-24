// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package draw provides low-level raster helpers for fills, overlays, glyph
// fallbacks, text decorations, and cursor blink visibility.
package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/types"
)

// FrameContext exposes per-draw frame configuration and color resolution.
type FrameContext interface {
	Snapshot() config.Snapshot
	Metrics() types.Metrics
	GridBounds() image.Rectangle
	CellColors(cell *uv.Cell) (fg, bg color.Color)
}
