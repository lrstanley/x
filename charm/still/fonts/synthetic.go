// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Synthetic font faces wrap a [font.Face] to approximate bold and italic when a
// loaded [FontFamily] omits variants. Glyph advances and metrics match the base
// face; only rasterized masks are transformed.

package fonts

import (
	"image"
	"math"

	"github.com/lrstanley/x/charm/still/internal/alpha"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// syntheticItalicAngle is the oblique shear used when synthesizing italic glyphs.
const syntheticItalicAngle = 11 * math.Pi / 180

// SyntheticFace returns a non-owning face wrapper with optional bold and/or italic synthesis.
//
// Metrics, advances, and kerning are delegated to base. Close on the returned
// face does not close or otherwise own base.
func SyntheticFace(base font.Face, bold, italic bool, points float64) font.Face {
	if !bold && !italic {
		return base
	}
	radius := 0
	if bold {
		radius = syntheticBoldRadius(points)
	}
	return syntheticFace{base: base, boldRadius: radius, italic: italic}
}

type syntheticFace struct {
	base       font.Face
	boldRadius int
	italic     bool
}

func (f syntheticFace) Close() error {
	return nil
}

func (f syntheticFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	dr, mask, maskp, advance, ok := f.base.Glyph(dot, r)
	if !ok || mask == nil || dr.Empty() || (!f.italic && f.boldRadius <= 0) {
		return dr, mask, maskp, advance, ok
	}

	dr, mask = transformSyntheticMask(dr, mask, maskp, f.boldRadius, f.italic)
	return dr, mask, image.Point{}, advance, true
}

func (f syntheticFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	bounds, advance, ok := f.base.GlyphBounds(r)
	if !ok {
		return bounds, advance, false
	}

	expand := fixed.I(f.boldRadius)
	if f.italic {
		height := int(math.Ceil(float64(bounds.Max.Y-bounds.Min.Y) / 64))
		expand += fixed.I(syntheticItalicShift(height))
	}
	bounds.Max.X += expand
	return bounds, advance, true
}

func (f syntheticFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return f.base.GlyphAdvance(r)
}

func (f syntheticFace) Kern(r0, r1 rune) fixed.Int26_6 {
	return f.base.Kern(r0, r1)
}

func (f syntheticFace) Metrics() font.Metrics {
	return f.base.Metrics()
}

func syntheticBoldRadius(points float64) int {
	return max(1, int(math.Round(points/14)))
}

func syntheticItalicShift(height int) int {
	if height <= 1 {
		return 0
	}
	return int(math.Ceil(math.Tan(syntheticItalicAngle) * float64(height-1)))
}

func transformSyntheticMask(dr image.Rectangle, mask image.Image, maskp image.Point, boldRadius int, italic bool) (image.Rectangle, *image.Alpha) {
	width, height := dr.Dx(), dr.Dy()
	shearMax := 0
	if italic {
		shearMax = syntheticItalicShift(height)
	}

	out := image.NewAlpha(image.Rect(0, 0, width+shearMax, height))
	if italic {
		shearAlphaBilinear(out, mask, maskp, width, height, math.Tan(syntheticItalicAngle))
	} else {
		for y := range height {
			for x := range width {
				a := alpha.At(mask, image.Pt(maskp.X+x, maskp.Y+y))
				if a == 0 {
					continue
				}
				alpha.SetMax(out, x, y, a)
			}
		}
	}

	if boldRadius > 0 {
		out = alpha.DilateX(out, boldRadius)
	}

	bounds := image.Rect(dr.Min.X, dr.Min.Y, dr.Min.X+out.Bounds().Dx(), dr.Min.Y+out.Bounds().Dy())
	return bounds, out
}

func shearAlphaBilinear(dst *image.Alpha, src image.Image, srcp image.Point, width, height int, tanAngle float64) {
	b := dst.Bounds()
	for y := range height {
		rowShift := tanAngle * float64(height-1-y)
		srcY := srcp.Y + y
		for x := b.Min.X; x < b.Max.X; x++ {
			a := alphaAtLinearX(src, srcp.X, srcY, float64(x)-rowShift, width)
			if a == 0 {
				continue
			}
			dst.Pix[dst.PixOffset(x, y)] = a
		}
	}
}

func alphaAtLinearX(img image.Image, originX, y int, x float64, width int) uint8 {
	x0 := int(math.Floor(x))
	frac := x - float64(x0)
	x1 := x0 + 1

	a0 := uint8(0)
	if x0 >= 0 && x0 < width {
		a0 = alpha.At(img, image.Pt(originX+x0, y))
	}
	a1 := uint8(0)
	if x1 >= 0 && x1 < width {
		a1 = alpha.At(img, image.Pt(originX+x1, y))
	}
	if a0 == 0 && a1 == 0 {
		return 0
	}
	return uint8(math.Round(float64(a0)*(1-frac) + float64(a1)*frac))
}
