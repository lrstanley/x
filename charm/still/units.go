// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"cmp"
	"math"
)

func clamp[T cmp.Ordered](v, vmin, vmax T) T {
	if v < vmin {
		return vmin
	}
	if v > vmax {
		return vmax
	}
	return v
}

// Px is a whole device-pixel count along one axis (canvas pixels after DPI
// scaling), as used for final raster sizes and integer layout.
type Px int

// Int returns px as an int.
func (px Px) Int() int {
	return int(px)
}

// Pt is a typographic point (1/72 inch). May be fractional and is converted to
// pixels using DPI.
type Pt float64

// Pixels converts pt to whole pixels at DPI using [math.Round], with a minimum
// of one pixel. Picks the nearest integer pixel size from a (possibly fractional)
// point size at a given dots-per-inch relationship.
func (pt Pt) Pixels(dpi DPI) Px {
	return Px(max(1, int(math.Round(float64(pt)*float64(dpi)/DefaultPPI))))
}

// DPI is dots per inch for converting [Pt] to [Px]. The renderer defaults to
// [DefaultDPI], a common CSS/reference pixel baseline.
type DPI float64

// Percent is a fractional percentage scalar (for example 0.2 means +20%).
type Percent float64

// Float64 returns p as a float64.
func (p Percent) Float64() float64 {
	return float64(p)
}

// FractionalPx is a sub-pixel device-pixel measurement, typically an unrounded
// font metric before cell width/height rounding.
type FractionalPx float64

// Float64 returns px as a float64.
func (px FractionalPx) Float64() float64 {
	return float64(px)
}

// Adjustment is a signed fractional delta in [-1, 1] applied as a multiplier
// (1 + adjustment) to a positive pixel baseline, then rounded, with a minimum
// result of one pixel. Configure with [NewAdjustment].
type Adjustment float64

// NewAdjustment returns a clamped adjustment in [-1, 1]. Inputs outside that
// range are truncated; malformed option wiring should still be rejected at
// option-parse time where the plan requires panics.
func NewAdjustment(v float64) Adjustment {
	return Adjustment(clamp(v, -1.0, 1.0))
}

// Apply scales a positive pixel baseline by (1+a), rounds to int, and returns
// at least 1. Use for line-thickness and similar metrics that must not collapse
// to zero after adjustment.
func (a Adjustment) Apply(v int) int {
	return max(1, int(math.Round(float64(v)*(1+float64(a)))))
}
