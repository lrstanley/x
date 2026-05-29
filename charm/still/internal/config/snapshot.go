// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
	"golang.org/x/image/font"
)

// FontFaces resolves grid and mapped font faces for a draw.
type FontFaces interface {
	FaceForCell(cell *uv.Cell) font.Face
	UsesGridLayout(face font.Face) bool
}

// Snapshot is the immutable per-draw configuration built from [Options],
// derived metrics, loaded fonts, and live emulator state.
type Snapshot struct {
	Metrics types.Metrics
	Fonts   FontFaces
	Palette types.Palette

	State    types.EmulatorState
	HasState bool

	Margin     units.Px
	MarginFill color.Color
	Padding    units.Px
	Scrollbar  bool

	BorderRadius           units.Px
	BackgroundOpacity      float64
	BackgroundOpacityCells bool
	FaintFactor            float64
	FocusDimming           float64
	CursorBlinkSpeed       time.Duration
	Now                    time.Time
	BoxThicknessOverride   bool
}

// NewSnapshot builds a draw snapshot from renderer options, derived state, and
// optional live emulator state.
func NewSnapshot(opts Options, metrics types.Metrics, fonts FontFaces, emu *types.EmulatorState) Snapshot {
	snap := Snapshot{
		Metrics:                metrics,
		Fonts:                  fonts,
		Palette:                ClonePalette(opts.Palette),
		Margin:                 opts.Margin,
		MarginFill:             opts.MarginFill,
		Padding:                opts.Padding,
		Scrollbar:              opts.Scrollbar,
		BorderRadius:           opts.BorderRadius,
		BackgroundOpacity:      opts.BackgroundOpacity,
		BackgroundOpacityCells: opts.BackgroundOpacityCells,
		FaintFactor:            opts.FaintFactor,
		FocusDimming:           opts.FocusDimming,
		CursorBlinkSpeed:       opts.CursorBlinkSpeed,
		Now:                    opts.Now(),
		BoxThicknessOverride:   opts.BoxThicknessOverride(),
	}
	if emu != nil {
		snap.State = *emu
		snap.HasState = true
	}
	return snap
}
