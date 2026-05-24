// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// isBoxDrawingRune reports whether r is a box-drawing or block-element glyph
// rendered on the primary grid face (Ghostty adjust-box-thickness scope).
func isBoxDrawingRune(r rune) bool {
	return (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F)
}

type boxStrokeAxis uint8

const (
	boxStrokeHorizontal boxStrokeAxis = iota
	boxStrokeVertical
	boxStrokeBoth
)

// boxStrokeAxisFromInk classifies a rasterized box glyph as predominantly
// horizontal, vertical, or corner/junction ink from its tight alpha bounds.
func boxStrokeAxisFromInk(ink image.Rectangle) boxStrokeAxis {
	w, h := ink.Dx(), ink.Dy()
	if w >= h*2 {
		return boxStrokeHorizontal
	}
	if h >= w*2 {
		return boxStrokeVertical
	}
	return boxStrokeBoth
}

// boxGlyphDilateRadius returns symmetric dilation radii so stroke extent along
// the dominant axis reaches thickness pixels (Ghostty box_thickness semantics).
func boxGlyphDilateRadius(axis boxStrokeAxis, ink image.Rectangle, thickness int) (rx, ry int) {
	if thickness <= 0 {
		return 0, 0
	}
	w, h := ink.Dx(), ink.Dy()
	switch axis {
	case boxStrokeHorizontal:
		return 0, (max(0, thickness-h) + 1) / 2
	case boxStrokeVertical:
		return (max(0, thickness-w) + 1) / 2, 0
	default:
		return (max(0, thickness-w) + 1) / 2, (max(0, thickness-h) + 1) / 2
	}
}

// rasterGlyphAlpha copies a glyph mask into a local alpha image aligned to dr.
func rasterGlyphAlpha(dr image.Rectangle, mask image.Image, maskp image.Point) *image.Alpha {
	alpha := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	draw.DrawMask(alpha, alpha.Bounds(), image.Opaque, image.Point{}, mask, maskp, draw.Src)
	return alpha
}

// alphaInkBounds returns the tight bounding box of non-zero alpha in alpha.
func alphaInkBounds(alpha *image.Alpha) (image.Rectangle, bool) {
	bounds := image.Rectangle{
		Min: image.Pt(alpha.Bounds().Max.X, alpha.Bounds().Max.Y),
		Max: image.Pt(alpha.Bounds().Min.X, alpha.Bounds().Min.Y),
	}
	for y := alpha.Bounds().Min.Y; y < alpha.Bounds().Max.Y; y++ {
		for x := alpha.Bounds().Min.X; x < alpha.Bounds().Max.X; x++ {
			if alpha.AlphaAt(x, y).A == 0 {
				continue
			}
			if x < bounds.Min.X {
				bounds.Min.X = x
			}
			if y < bounds.Min.Y {
				bounds.Min.Y = y
			}
			if x+1 > bounds.Max.X {
				bounds.Max.X = x + 1
			}
			if y+1 > bounds.Max.Y {
				bounds.Max.Y = y + 1
			}
		}
	}
	return bounds, bounds.Min.X < bounds.Max.X && bounds.Min.Y < bounds.Max.Y
}

// dilateAlpha expands non-zero pixels by radiusX/radiusY using max-alpha union
// (Ghostty box_thickness dilation).
func dilateAlpha(alpha *image.Alpha, radiusX, radiusY int) *image.Alpha {
	if radiusX <= 0 && radiusY <= 0 {
		return alpha
	}
	b := alpha.Bounds()
	out := image.NewAlpha(image.Rect(0, 0, b.Dx()+2*radiusX, b.Dy()+2*radiusY))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := alpha.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			for dy := -radiusY; dy <= radiusY; dy++ {
				for dx := -radiusX; dx <= radiusX; dx++ {
					setMaxAlpha(out, x-b.Min.X+radiusX+dx, y-b.Min.Y+radiusY+dy, a)
				}
			}
		}
	}
	return out
}

// setMaxAlpha writes a to dst at (x,y) only when a exceeds the current value.
func setMaxAlpha(dst *image.Alpha, x, y int, a uint8) {
	if !image.Pt(x, y).In(dst.Bounds()) {
		return
	}
	if cur := dst.AlphaAt(x, y).A; a > cur {
		dst.SetAlpha(x, y, color.Alpha{A: a})
	}
}

type boxGlyphMask struct {
	dr    image.Rectangle
	alpha *image.Alpha
}

// layoutBoxGlyphMask builds a thickened box mask when box thickness override
// is enabled and layout is a grid box-drawing glyph.
func layoutBoxGlyphMask(ctx Context, layout glyphLayout) (boxGlyphMask, bool) {
	if !ctx.cfg.boxThicknessOverride || layout.kind != glyphGrid {
		return boxGlyphMask{}, false
	}
	r, n := utf8.DecodeRuneInString(layout.glyph)
	if n == 0 || r == utf8.RuneError || !isBoxDrawingRune(r) {
		return boxGlyphMask{}, false
	}
	return thickenBoxGlyphMask(layout.face, layout.dot, r, ctx.Metrics().BoxThickness.Int())
}

// thickenBoxGlyphMask rasterizes r at dot and dilates ink to match thickness.
func thickenBoxGlyphMask(face font.Face, dot fixed.Point26_6, r rune, thickness int) (boxGlyphMask, bool) {
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() || mask == nil {
		return boxGlyphMask{}, false
	}
	alpha := rasterGlyphAlpha(dr, mask, maskp)
	ink, ok := alphaInkBounds(alpha)
	if !ok {
		return boxGlyphMask{}, false
	}
	rx, ry := boxGlyphDilateRadius(boxStrokeAxisFromInk(ink), ink, thickness)
	if rx == 0 && ry == 0 {
		return boxGlyphMask{dr: dr, alpha: alpha}, true
	}
	dilated := dilateAlpha(alpha, rx, ry)
	out := image.Rect(dr.Min.X-rx, dr.Min.Y-ry, dr.Max.X+rx, dr.Max.Y+ry)
	return boxGlyphMask{dr: out, alpha: dilated}, true
}

// drawBoxGlyphMask composites a thickened box glyph over img using fg.
func drawBoxGlyphMask(img draw.Image, fg color.Color, m boxGlyphMask) {
	draw.DrawMask(img, m.dr, image.NewUniform(fg), image.Point{}, m.alpha, image.Point{}, draw.Over)
}
