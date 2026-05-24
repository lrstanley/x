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

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// syntheticItalicAngle is the oblique shear used when synthesizing italic glyphs.
const syntheticItalicAngle = 11 * math.Pi / 180

// SyntheticBoldFace returns a non-owning face wrapper that renders bolder glyphs.
//
// Metrics, advances, and kerning are delegated to base. Close on the returned
// face does not close or otherwise own base. points is the face size in points;
// bold stroke width scales with points (roughly one pixel per 14pt).
func SyntheticBoldFace(base font.Face, points float64) font.Face {
	return syntheticFace{base: base, boldRadius: syntheticBoldRadius(points)}
}

// SyntheticItalicFace returns a non-owning face wrapper that renders italic glyphs.
//
// Metrics, advances, and kerning are delegated to base. Close on the returned
// face does not close or otherwise own base.
func SyntheticItalicFace(base font.Face) font.Face {
	return syntheticFace{base: base, italic: true}
}

// SyntheticBoldItalicFace returns a non-owning face wrapper that renders bold
// italic glyphs.
//
// Metrics, advances, and kerning are delegated to base. Close on the returned
// face does not close or otherwise own base. points is the face size in points;
// bold stroke width scales with points (roughly one pixel per 14pt).
func SyntheticBoldItalicFace(base font.Face, points float64) font.Face {
	return syntheticFace{base: base, boldRadius: syntheticBoldRadius(points), italic: true}
}

// syntheticFace implements [font.Face] by delegating metrics and advances to base
// while transforming glyph masks for synthetic bold and/or italic.
type syntheticFace struct {
	base       font.Face
	boldRadius int
	italic     bool
}

// Close does not close or release base, it's a no-op to satisfy [font.Face].
func (f syntheticFace) Close() error {
	return nil
}

// Glyph returns a transformed mask when bold or italic synthesis is enabled;
// otherwise it forwards to base unchanged.
func (f syntheticFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	dr, mask, maskp, advance, ok := f.base.Glyph(dot, r)
	if !ok || mask == nil || dr.Empty() || (!f.italic && f.boldRadius <= 0) {
		return dr, mask, maskp, advance, ok
	}

	dr, mask = transformSyntheticMask(dr, mask, maskp, f.boldRadius, f.italic)
	return dr, mask, image.Point{}, advance, true
}

// GlyphBounds expands horizontal bounds to cover synthesized ink.
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

// GlyphAdvance delegates to base so layout width matches the source face.
func (f syntheticFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return f.base.GlyphAdvance(r)
}

// Kern delegates to base, to satisfy [font.Face].
func (f syntheticFace) Kern(r0, r1 rune) fixed.Int26_6 {
	return f.base.Kern(r0, r1)
}

// Metrics delegates to base, to satisfy [font.Face].
func (f syntheticFace) Metrics() font.Metrics {
	return f.base.Metrics()
}

// syntheticBoldRadius maps face size in points to a horizontal dilation radius.
func syntheticBoldRadius(points float64) int {
	return max(1, int(math.Round(points/14)))
}

// syntheticItalicShift returns the top-row horizontal offset for a glyph of the
// given pixel height at [syntheticItalicAngle].
func syntheticItalicShift(height int) int {
	if height <= 1 {
		return 0
	}
	return int(math.Ceil(math.Tan(syntheticItalicAngle) * float64(height-1)))
}

// transformSyntheticMask shears mask for italic synthesis and dilates it for bold,
// returning the device-space bounds and alpha mask for drawing.
func transformSyntheticMask(dr image.Rectangle, mask image.Image, maskp image.Point, boldRadius int, italic bool) (image.Rectangle, *image.Alpha) {
	width, height := dr.Dx(), dr.Dy()
	shearMax := 0
	if italic {
		shearMax = syntheticItalicShift(height)
	}

	alpha := image.NewAlpha(image.Rect(0, 0, width+shearMax, height))
	if italic {
		shearAlphaBilinear(alpha, mask, maskp, width, height, math.Tan(syntheticItalicAngle))
	} else {
		for y := range height {
			for x := range width {
				a := alphaAt(mask, image.Pt(maskp.X+x, maskp.Y+y))
				if a == 0 {
					continue
				}
				setMaxAlpha(alpha, x, y, a)
			}
		}
	}

	if boldRadius > 0 {
		alpha = dilateAlphaX(alpha, boldRadius)
	}

	out := image.Rect(dr.Min.X, dr.Min.Y, dr.Min.X+alpha.Bounds().Dx(), dr.Min.Y+alpha.Bounds().Dy())
	return out, alpha
}

// dilateAlphaX expands ink to the right by up to radius pixels per column.
func dilateAlphaX(src *image.Alpha, radius int) *image.Alpha {
	b := src.Bounds()
	dst := image.NewAlpha(image.Rect(0, 0, b.Dx()+radius, b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := src.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			for dx := range radius + 1 {
				setMaxAlpha(dst, x-b.Min.X+dx, y-b.Min.Y, a)
			}
		}
	}
	return dst
}

// shearAlphaBilinear inverse-maps each destination pixel through the oblique shear
// and resamples the source mask with horizontal linear interpolation. That avoids
// the jagged nearest-neighbor edges produced by forward row shifting.
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

// alphaAtLinearX linearly interpolates alpha along x in source space. x is
// relative to the glyph mask origin; out-of-range samples contribute zero.
func alphaAtLinearX(img image.Image, originX, y int, x float64, width int) uint8 {
	x0 := int(math.Floor(x))
	frac := x - float64(x0)
	x1 := x0 + 1

	a0 := uint8(0)
	if x0 >= 0 && x0 < width {
		a0 = alphaAt(img, image.Pt(originX+x0, y))
	}
	a1 := uint8(0)
	if x1 >= 0 && x1 < width {
		a1 = alphaAt(img, image.Pt(originX+x1, y))
	}
	if a0 == 0 && a1 == 0 {
		return 0
	}
	return uint8(math.Round(float64(a0)*(1-frac) + float64(a1)*frac))
}

// alphaAt reads the alpha channel at p from any [image.Image].
func alphaAt(img image.Image, p image.Point) uint8 {
	_, _, _, a := img.At(p.X, p.Y).RGBA()
	a >>= 8
	if a > 0xff {
		return 0xff
	}
	return uint8(a)
}

// setMaxAlpha stores the greater of the existing and new alpha at (x, y).
func setMaxAlpha(img *image.Alpha, x, y int, a uint8) {
	if !(image.Pt(x, y).In(img.Bounds())) {
		return
	}
	i := img.PixOffset(x, y)
	if a > img.Pix[i] {
		img.Pix[i] = a
	}
}
