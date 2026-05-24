// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package config holds merged renderer [Options], defaults, and immutable
// [Snapshot] values shared across the still render pipeline.
package config

import (
	"image/color"
	"maps"
	"time"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
)

const (
	DefaultDPI              = units.DPI(96)
	DefaultFontSize         = units.Pt(11)
	DefaultCursorBlinkSpeed = 600 * time.Millisecond
	DefaultBgOpacity        = 1.0
	DefaultFaintFactor      = 0.5
	DefaultFocusDimming     = 0.16
)

// Options is the merged renderer configuration owned by a [Renderer].
type Options struct {
	DPI      units.DPI
	FontSize units.Pt

	CellWidth              units.Adjustment
	CellHeight             units.Adjustment
	FontBaseline           units.Adjustment
	UnderlinePosition      units.Adjustment
	UnderlineThickness     units.Px
	StrikethroughPosition  units.Adjustment
	StrikethroughThickness units.Px
	CursorThickness        units.Px
	CursorHeight           units.Adjustment
	BoxThickness           units.Px
	IconHeightScale        float64
	IconHeightSingleScale  float64
	IconHeightSingleSet    bool

	FontFamily      fonts.FontFamily
	FontFamilySet   bool
	CodepointMap    map[string]fonts.FontFamily
	CodepointMapSet bool

	Palette types.Palette

	State    types.EmulatorState
	HasState bool

	Scrollbar              bool
	BorderRadius           units.Px
	Margin                 units.Px
	MarginFill             color.Color
	Padding                units.Px
	BackgroundOpacity      float64
	BackgroundOpacityCells bool
	FaintFactor            float64
	FocusDimming           float64
	CursorBlinkSpeed       time.Duration
	Now                    func() time.Time
}

// Dirty labels rebuild buckets for font loading and metric derivation.
type Dirty uint8

const DirtyNone Dirty = 0

const (
	DirtyMetrics Dirty = 1 << iota
	DirtyFonts
)

// DefaultOptions returns baseline renderer options without drawer hooks.
func DefaultOptions() Options {
	return Options{
		DPI:                   DefaultDPI,
		FontSize:              DefaultFontSize,
		CursorThickness:       1,
		BackgroundOpacity:     DefaultBgOpacity,
		FaintFactor:           DefaultFaintFactor,
		FocusDimming:          DefaultFocusDimming,
		CursorBlinkSpeed:      DefaultCursorBlinkSpeed,
		IconHeightScale:       1,
		IconHeightSingleScale: 1,
		Now:                   time.Now,
	}
}

// PositivePx validates a non-negative pixel count.
func PositivePx(px int, name string) units.Px {
	if px < 0 {
		panic(name + " must be >= 0")
	}
	return units.Px(px)
}

// RequiredPx validates a positive pixel count.
func RequiredPx(px int, name string) units.Px {
	if px <= 0 {
		panic(name + " must be > 0")
	}
	return units.Px(px)
}

// ClonePalette returns a defensive copy of palette indexed colors.
func ClonePalette(p types.Palette) types.Palette {
	if len(p.Indexed) == 0 {
		p.Indexed = nil
		return p
	}
	indexed := make(map[int]color.Color, len(p.Indexed))
	maps.Copy(indexed, p.Indexed)
	p.Indexed = indexed
	return p
}

// NormalizeIconHeightScale validates icon height scale.
func NormalizeIconHeightScale(scale float64) float64 {
	if scale < 0 {
		panic("icon height scale must be >= 0")
	}
	return scale
}

// BoxThicknessOverride reports whether explicit box thickness was configured.
func (o Options) BoxThicknessOverride() bool {
	return o.BoxThickness > 0
}
