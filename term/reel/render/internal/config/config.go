// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package config holds merged renderer [Options], defaults, and render-context
// interfaces shared across the render pipeline.
package config

import (
	"errors"
	"fmt"
	"image/color"
	"maps"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel/render/fonts"
	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/render/units"
)

const (
	// BaseFontSizePx is the font size in pixels at a scale of 1 (11pt at 96
	// DPI). Scale is the only font sizing knob; there is no physical display, so
	// points and DPI carry no extra meaning.
	BaseFontSizePx = 11.0 * 96.0 / 72.0

	DefaultScale            = 1.0
	DefaultCursorBlinkSpeed = 600 * time.Millisecond
	DefaultBgOpacity        = 1.0
	DefaultFaintFactor      = 0.5
	DefaultFocusDimming     = 0.16

	DefaultProgressBarHeight = units.Px(2)
)

// Options is the merged renderer configuration owned by a [Renderer].
type Options struct {
	// Scale multiplies the base font size, and therefore every cell metric
	// derived from it. See [BaseFontSizePx].
	Scale float64
	// ScaleSet reports whether Scale was configured explicitly.
	ScaleSet bool

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

	SystemFontFallback bool

	Palette types.Palette

	Scrollbar              bool
	ScrollbarWidth         units.Px // zero scales with the cell width
	WindowChrome           bool
	ProgressBar            bool
	ProgressBarHeight      units.Px
	ProgramStatus          bool
	BorderRadius           units.Px
	Margin                 units.Px
	MarginFill             color.Color
	Padding                units.Px
	BackgroundOpacity      float64
	BackgroundOpacityCells bool
	FaintFactor            float64
	FocusDimming           float64
	CursorBlinkSpeed       time.Duration
}

// DefaultOptions returns baseline renderer options.
func DefaultOptions() Options {
	return Options{
		Scale:                 DefaultScale,
		CursorThickness:       1,
		BackgroundOpacity:     DefaultBgOpacity,
		FaintFactor:           DefaultFaintFactor,
		FocusDimming:          DefaultFocusDimming,
		CursorBlinkSpeed:      DefaultCursorBlinkSpeed,
		IconHeightScale:       1,
		IconHeightSingleScale: 1,
		SystemFontFallback:    true,
		ProgressBar:           true,
		ProgressBarHeight:     DefaultProgressBarHeight,
		ProgramStatus:         true,
	}
}

// ParsePositivePx validates a non-negative pixel count.
func ParsePositivePx(px int, name string) (units.Px, error) {
	if px < 0 {
		return 0, fmt.Errorf("render: %s must be >= 0", name)
	}
	return units.Px(px), nil
}

// ParseRequiredPx validates a positive pixel count.
func ParseRequiredPx(px int, name string) (units.Px, error) {
	if px <= 0 {
		return 0, fmt.Errorf("render: %s must be > 0", name)
	}
	return units.Px(px), nil
}

// ParseScale validates a font scale, which must be positive and finite.
func ParseScale(scale float64) (float64, error) {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 0, fmt.Errorf("render: scale must be > 0, got %v", scale)
	}
	return scale, nil
}

// FontSizePx is the primary font size in pixels at the configured scale.
func (o Options) FontSizePx() float64 {
	return BaseFontSizePx * o.Scale
}

// ParseIconHeightScale validates icon height scale.
func ParseIconHeightScale(scale float64) (float64, error) {
	if scale < 0 {
		return 0, errors.New("render: icon height scale must be >= 0")
	}
	return scale, nil
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

// BoxThicknessOverride reports whether explicit box thickness was configured.
func (o Options) BoxThicknessOverride() bool {
	return o.BoxThickness > 0
}
