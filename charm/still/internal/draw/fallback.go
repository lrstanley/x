// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/units"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// FallbackGlyphTarget returns the vertical band for nerd-icon layout.
func FallbackGlyphTarget(ctx config.CellFrameSource, area image.Rectangle, cell *uv.Cell) image.Rectangle {
	if area.Empty() {
		return area
	}

	metrics := ctx.Metrics()
	height := metrics.IconHeightSingle.Int()
	if cell != nil && cell.Width > 1 {
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
	r, n := utf8.DecodeRuneInString(glyph)
	if n == 0 || r == utf8.RuneError {
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
	r, n := utf8.DecodeRuneInString(glyph)
	if n == 0 || r == utf8.RuneError {
		return
	}
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() || mask == nil || dr.Dx() <= 0 || dr.Dy() <= 0 || target.Empty() {
		return
	}
	srcAlpha := image.NewAlpha(dr)
	draw.DrawMask(srcAlpha, dr, image.Opaque, image.Point{}, mask, maskp, draw.Src)

	var sx, sy float64
	var x0, y0 int
	switch mode {
	case ScaleDefault:
		sx = min(1, float64(target.Dx())/float64(dr.Dx()))
		sy = min(1, float64(target.Dy())/float64(dr.Dy()))
	case ScaleStretch:
		const padX = 0.03
		const padY = 0.005
		tw := float64(target.Dx()) * (1 + 2*padX)
		th := float64(target.Dy()) * (1 + 2*padY)
		sx = tw / float64(dr.Dx())
		sy = th / float64(dr.Dy())
		dw := max(1, int(math.Ceil(float64(dr.Dx())*sx)))
		dh := max(1, int(math.Ceil(float64(dr.Dy())*sy)))
		scaledAlpha := image.NewAlpha(image.Rect(0, 0, dw, dh))
		xdraw.ApproxBiLinear.Scale(scaledAlpha, scaledAlpha.Bounds(), srcAlpha, dr, draw.Src, nil)

		bleedX := max(1, int(math.Round(float64(target.Dx())*padX)))
		bleedY := max(1, int(math.Round(float64(target.Dy())*padY)))
		y0 = target.Min.Y - bleedY + (target.Dy()+2*bleedY-dh)/2
		if alignEnd {
			x0 = target.Max.X + bleedX - dw
		} else {
			x0 = target.Min.X - bleedX
		}
		out := image.Rect(x0, y0, x0+dw, y0+dh)
		draw.DrawMask(dst, out, image.NewUniform(fg), image.Point{}, scaledAlpha, image.Point{}, draw.Over)
		return
	case ScaleFitCover1:
		sx = min(float64(target.Dx())/float64(dr.Dx()), float64(target.Dy())/float64(dr.Dy()))
		sy = sx
	}
	dw := max(1, int(math.Round(float64(dr.Dx())*sx)))
	dh := max(1, int(math.Round(float64(dr.Dy())*sy)))
	scaledAlpha := image.NewAlpha(image.Rect(0, 0, dw, dh))
	xdraw.ApproxBiLinear.Scale(scaledAlpha, scaledAlpha.Bounds(), srcAlpha, dr, draw.Src, nil)

	x0 = target.Min.X + (target.Dx()-dw)/2
	y0 = target.Min.Y + (target.Dy()-dh)/2
	out := image.Rect(x0, y0, x0+dw, y0+dh)
	draw.DrawMask(dst, out, image.NewUniform(fg), image.Point{}, scaledAlpha, image.Point{}, draw.Over)
}

// GlyphRasterBounds returns the device rectangle for the first rune of glyph at dot.
func GlyphRasterBounds(face font.Face, dot fixed.Point26_6, glyph string) (image.Rectangle, bool) {
	r, n := utf8.DecodeRuneInString(glyph)
	if n == 0 || r == utf8.RuneError {
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
