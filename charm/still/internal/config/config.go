// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package config holds merged renderer [Options], defaults, and render-context
// interfaces shared across the still render pipeline.
package config

import (
	"errors"
	"fmt"
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

// DefaultOptions returns baseline renderer options.
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

// ParsePositivePx validates a non-negative pixel count.
func ParsePositivePx(px int, name string) (units.Px, error) {
	if px < 0 {
		return 0, fmt.Errorf("still: %s must be >= 0", name)
	}
	return units.Px(px), nil
}

// ParseRequiredPx validates a positive pixel count.
func ParseRequiredPx(px int, name string) (units.Px, error) {
	if px <= 0 {
		return 0, fmt.Errorf("still: %s must be > 0", name)
	}
	return units.Px(px), nil
}

// ParseIconHeightScale validates icon height scale.
func ParseIconHeightScale(scale float64) (float64, error) {
	if scale < 0 {
		return 0, errors.New("still: icon height scale must be >= 0")
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
