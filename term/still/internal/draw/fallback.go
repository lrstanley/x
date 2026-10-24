// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/lrstanley/x/term/still/internal/config"
	"github.com/lrstanley/x/term/still/units"
	"github.com/lrstanley/x/term/vt/screen"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// FallbackGlyphTarget returns the vertical band that fallback glyphs (Nerd Font
// icons, fallback fonts) are fitted into.
func FallbackGlyphTarget(ctx config.CellFrameSource, area image.Rectangle, cell *screen.Cell) image.Rectangle {
	return fallbackBand(ctx, area, cell != nil && cell.Width > 1)
}

// FallbackGlyphSpanTarget returns the band for a one-cell fallback glyph that
// may grow into the cell after it: the same vertical band as a wide cell, and
// the width of area plus the next cell.
func FallbackGlyphSpanTarget(ctx config.CellFrameSource, area image.Rectangle) image.Rectangle {
	target := fallbackBand(ctx, area, true)
	if !target.Empty() {
		target.Max.X += area.Dx()
	}
	return target
}

// fallbackBand centers the icon height for a wide or single cell in area.
func fallbackBand(ctx config.CellFrameSource, area image.Rectangle, wide bool) image.Rectangle {
	if area.Empty() {
		return area
	}

	metrics := ctx.Metrics()
	height := metrics.IconHeightSingle.Int()
	if wide {
		height = metrics.IconHeight.Int()
	}
	height = units.Clamp(height, 1, area.Dy())

	target := area
	target.Min.Y = area.Min.Y + (area.Dy()-height)/2
	target.Max.Y = target.Min.Y + height
	return target
}

// PowerlineFallbackScale selects Ghostty-style stretch/fit_cover1 for Powerline codepoints.
func PowerlineFallbackScale(glyph string) (ScaleMode, bool) {
	r, ok := FirstRune(glyph)
	if !ok {
		return ScaleDefault, false
	}
	if r >= '\ue0a0' && r <= '\ue0a3' || r == '\ue0cf' {
		return ScaleFitCover1, false
	}
	if r < '\ue0b0' || r > '\ue0d7' {
		return ScaleDefault, false
	}
	switch r {
	case '\ue0ce', '\ue0d0', '\ue0d1':
		return ScaleFitCover1, false
	case '\ue0d2', '\ue0d6':
		return ScaleStretch, false
	case '\ue0d4', '\ue0d7':
		return ScaleStretch, true
	}
	return ScaleStretch, (r-'\ue0b0')%4 >= 2
}

// FallbackGlyphNeedsScale is true when rasterized ink extends outside the target cell.
func FallbackGlyphNeedsScale(dr, area image.Rectangle) bool {
	if dr.Empty() || area.Empty() {
		return false
	}
	return dr.Max.X > area.Max.X || dr.Min.X < area.Min.X || dr.Max.Y > area.Max.Y || dr.Min.Y < area.Min.Y
}

// FallbackGlyphScaled draws a glyph from a symbol face into target.
func FallbackGlyphScaled(dst draw.Image, target image.Rectangle, face font.Face, fg color.Color, glyph string, dot fixed.Point26_6, mode ScaleMode, alignEnd bool) {
	if out, mask, ok := ScaledGlyphMask(target, face, glyph, dot, mode, alignEnd); ok {
		draw.DrawMask(dst, out, image.NewUniform(fg), image.Point{}, mask, image.Point{}, draw.Over)
	}
}

// ScaledGlyphMask rasterizes the first rune of glyph from face, scaled into
// target per mode, and returns the mask and the rectangle it covers.
func ScaledGlyphMask(target image.Rectangle, face font.Face, glyph string, dot fixed.Point26_6, mode ScaleMode, alignEnd bool) (image.Rectangle, *image.Alpha, bool) {
	r, ok := FirstRune(glyph)
	if !ok {
		return image.Rectangle{}, nil, false
	}
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() || mask == nil || target.Empty() {
		return image.Rectangle{}, nil, false
	}
	srcAlpha := image.NewAlpha(dr)
	draw.DrawMask(srcAlpha, dr, image.Opaque, image.Point{}, mask, maskp, draw.Src)

	var out image.Rectangle
	switch mode {
	case ScaleStretch:
		const padX = 0.03
		const padY = 0.005
		// The bleed is whole pixels, rounded from the fractional pad, so the
		// overdraw tracks the cell size. Horizontally it keeps a 1px floor: the
		// glyph's own edge column is only partly opaque, and without the bleed
		// that shows as a seam against the neighboring cell. Vertically there
		// is no floor, so small cells don't poke out above or below. The scaled
		// mask is exactly the bleed-extended target, so it stays centered.
		bleedX := max(1, int(math.Round(float64(target.Dx())*padX)))
		bleedY := int(math.Round(float64(target.Dy()) * padY))
		dw := max(1, target.Dx()+2*bleedX)
		dh := max(1, target.Dy()+2*bleedY)
		x0 := target.Min.X - bleedX
		if alignEnd {
			x0 = target.Max.X + bleedX - dw
		}
		out = image.Rect(x0, target.Min.Y-bleedY, x0+dw, target.Min.Y-bleedY+dh)
	case ScaleDefault, ScaleFitCover1:
		// One scale for both axes, so the glyph keeps its aspect ratio:
		// a round icon that is too wide for its cell shrinks into it
		// instead of being squashed into an oval. ScaleDefault only ever
		// shrinks; ScaleFitCover1 also grows to fit.
		s := min(float64(target.Dx())/float64(dr.Dx()), float64(target.Dy())/float64(dr.Dy()))
		if mode == ScaleDefault {
			s = min(1, s)
		}
		dw := max(1, int(math.Round(float64(dr.Dx())*s)))
		dh := max(1, int(math.Round(float64(dr.Dy())*s)))
		x0 := target.Min.X + (target.Dx()-dw)/2
		y0 := target.Min.Y + (target.Dy()-dh)/2
		out = image.Rect(x0, y0, x0+dw, y0+dh)
	}
	scaled := image.NewAlpha(image.Rect(0, 0, out.Dx(), out.Dy()))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), srcAlpha, dr, draw.Src, nil)
	return out, scaled, true
}

// GlyphRasterBounds returns the device rectangle for the first rune of glyph at dot.
func GlyphRasterBounds(face font.Face, dot fixed.Point26_6, glyph string) (image.Rectangle, bool) {
	r, ok := FirstRune(glyph)
	if !ok {
		return image.Rectangle{}, false
	}
	dr, _, _, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() {
		return image.Rectangle{}, false
	}
	return dr, true
}

// GlyphVerticalAdjust returns a delta to add to the font dot Y so glyph ink fits area.
func GlyphVerticalAdjust(face font.Face, dot fixed.Point26_6, glyph string, area image.Rectangle) int {
	if area.Empty() || glyph == "" {
		return 0
	}
	adj := 0
	for range 5 {
		cur := dot
		cur.Y += fixed.I(adj)
		dr, ok := GlyphRasterBounds(face, cur, glyph)
		if !ok {
			return adj
		}
		delta := 0
		if dr.Max.Y > area.Max.Y {
			delta -= dr.Max.Y - area.Max.Y
		}
		if dr.Min.Y+delta < area.Min.Y {
			delta += area.Min.Y - (dr.Min.Y + delta)
		}
		if delta == 0 {
			return adj
		}
		adj += delta
	}
	return adj
}
