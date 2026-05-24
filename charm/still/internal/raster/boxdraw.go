// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"image/color"
	"image/draw"
	"unicode/utf8"

	"github.com/lrstanley/x/charm/still/internal/alpha"
	"github.com/lrstanley/x/charm/still/internal/config"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type boxGlyphMask struct {
	dr    image.Rectangle
	alpha *image.Alpha
}

func layoutBoxGlyphMask(ctx GlyphContext, layout Layout) (boxGlyphMask, bool) {
	cfg := ctx.Snapshot()
	if !cfg.BoxThicknessOverride || layout.Kind != KindGrid {
		return boxGlyphMask{}, false
	}
	r, n := utf8.DecodeRuneInString(layout.Glyph)
	if n == 0 || r == utf8.RuneError || !isBoxDrawingRune(r) {
		return boxGlyphMask{}, false
	}
	return thickenBoxGlyphMask(layout.Face, layout.Dot, r, cfg)
}

func drawBoxGlyphMask(img draw.Image, fg color.Color, m boxGlyphMask) {
	draw.DrawMask(img, m.dr, image.NewUniform(fg), image.Point{}, m.alpha, image.Point{}, draw.Over)
}

func isBoxDrawingRune(r rune) bool {
	return (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F)
}

// BoxStrokeAxis classifies box-drawing ink orientation for dilation.
type BoxStrokeAxis uint8

const (
	BoxStrokeHorizontal BoxStrokeAxis = iota
	BoxStrokeVertical
	BoxStrokeBoth
)

func boxStrokeAxisFromInk(ink image.Rectangle) BoxStrokeAxis {
	w, h := ink.Dx(), ink.Dy()
	if w >= h*2 {
		return BoxStrokeHorizontal
	}
	if h >= w*2 {
		return BoxStrokeVertical
	}
	return BoxStrokeBoth
}

// BoxGlyphDilateRadius returns horizontal and vertical dilation radii for box ink.
func BoxGlyphDilateRadius(axis BoxStrokeAxis, ink image.Rectangle, thickness int) (rx, ry int) {
	if thickness <= 0 {
		return 0, 0
	}
	w, h := ink.Dx(), ink.Dy()
	switch axis {
	case BoxStrokeHorizontal:
		return 0, (max(0, thickness-h) + 1) / 2
	case BoxStrokeVertical:
		return (max(0, thickness-w) + 1) / 2, 0
	case BoxStrokeBoth:
		return (max(0, thickness-w) + 1) / 2, (max(0, thickness-h) + 1) / 2
	}
	return 0, 0
}

func boxGlyphDilateRadius(axis BoxStrokeAxis, ink image.Rectangle, thickness int) (rx, ry int) {
	return BoxGlyphDilateRadius(axis, ink, thickness)
}

func thickenBoxGlyphMask(face font.Face, dot fixed.Point26_6, r rune, cfg config.Snapshot) (boxGlyphMask, bool) {
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() || mask == nil {
		return boxGlyphMask{}, false
	}
	alphaImg := alpha.RasterGlyph(dr, mask, maskp)
	ink, ok := alpha.InkBounds(alphaImg)
	if !ok {
		return boxGlyphMask{}, false
	}
	thickness := cfg.Metrics.BoxThickness.Int()
	rx, ry := boxGlyphDilateRadius(boxStrokeAxisFromInk(ink), ink, thickness)
	if rx == 0 && ry == 0 {
		return boxGlyphMask{dr: dr, alpha: alphaImg}, true
	}
	dilated := alpha.Dilate(alphaImg, rx, ry)
	out := image.Rect(dr.Min.X-rx, dr.Min.Y-ry, dr.Max.X+rx, dr.Max.Y+ry)
	return boxGlyphMask{dr: out, alpha: dilated}, true
}
