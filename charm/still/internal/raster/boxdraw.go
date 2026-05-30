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
	"github.com/lrstanley/x/charm/still/types"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type boxGlyphMask struct {
	dr    image.Rectangle
	alpha *image.Alpha
}

func layoutBoxGlyphMask(ctx GlyphContext, layout *Layout) (boxGlyphMask, bool) {
	if layout.Kind != KindGrid {
		return boxGlyphMask{}, false
	}
	r, n := utf8.DecodeRuneInString(layout.Glyph)
	if n == 0 || r == utf8.RuneError || !isBoxDrawingRune(r) {
		return boxGlyphMask{}, false
	}
	return buildBoxGlyphMask(layout.Face, layout.Dot, layout.Area, r, ctx.Metrics())
}

func drawBoxGlyphMask(img draw.Image, fg color.NRGBA, m boxGlyphMask) {
	draw.DrawMask(img, m.dr, image.NewUniform(fg), image.Point{}, m.alpha, image.Point{}, draw.Over)
}

func isBoxDrawingRune(r rune) bool {
	return r >= 0x2500 && r <= 0x257F
}

// IsBoxJunctionRune reports corner, tee, and cross box-drawing runes whose
// continuity comes from adjacent straight strokes rather than edge extension.
func IsBoxJunctionRune(r rune) bool {
	_, ok := BoxStrokeExtentsFromRune(r)
	return ok
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

// BoxStrokeExtents names which cell edges a junction glyph would reach when
// extending ink-derived strokes. Used only for ambiguous non-junction glyphs.
type BoxStrokeExtents struct {
	Right, Left, Down, Up bool
}

// BoxStrokeExtentsFromRune returns edge extension directions for junction glyphs.
func BoxStrokeExtentsFromRune(r rune) (BoxStrokeExtents, bool) {
	switch {
	case r >= 0x250C && r <= 0x250F, r == 0x2554, r == 0x256D:
		return BoxStrokeExtents{Right: true, Down: true}, true // top-left
	case r >= 0x2510 && r <= 0x2513, r == 0x2557, r == 0x256E:
		return BoxStrokeExtents{Left: true, Down: true}, true // top-right
	case r >= 0x2514 && r <= 0x2517, r == 0x255A, r == 0x2570:
		return BoxStrokeExtents{Right: true, Up: true}, true // bottom-left
	case r >= 0x2518 && r <= 0x251B, r == 0x255D, r == 0x256F:
		return BoxStrokeExtents{Left: true, Up: true}, true // bottom-right
	case r >= 0x251C && r <= 0x251F, r == 0x2555, r == 0x2560, r == 0x2561:
		return BoxStrokeExtents{Right: true, Down: true, Up: true}, true // left tee
	case r >= 0x2524 && r <= 0x2527, r == 0x2558, r == 0x2562, r == 0x2563:
		return BoxStrokeExtents{Left: true, Down: true, Up: true}, true // right tee
	case r >= 0x252C && r <= 0x252F, r == 0x2559, r == 0x2564, r == 0x2565:
		return BoxStrokeExtents{Right: true, Left: true, Down: true}, true // top tee
	case r >= 0x2534 && r <= 0x2538, r == 0x255B, r == 0x2566, r == 0x2567:
		return BoxStrokeExtents{Right: true, Left: true, Up: true}, true // bottom tee
	case r >= 0x253C && r <= 0x253F, r == 0x254B, r == 0x2568, r == 0x2569, r == 0x256A, r == 0x256B:
		return BoxStrokeExtents{Right: true, Left: true, Down: true, Up: true}, true // cross
	default:
		return BoxStrokeExtents{}, false
	}
}

func boxStrokeExtentsFromInk(ref, ink image.Rectangle) BoxStrokeExtents {
	if ref.Empty() {
		ref = ink
	}
	cx := (ink.Min.X + ink.Max.X) / 2
	cy := (ink.Min.Y + ink.Max.Y) / 2
	midX := ref.Min.X + ref.Dx()/2
	midY := ref.Min.Y + ref.Dy()/2
	var ext BoxStrokeExtents
	if cx <= midX {
		ext.Right = true
	} else {
		ext.Left = true
	}
	if cy <= midY {
		ext.Down = true
	} else {
		ext.Up = true
	}
	return ext
}

// boxVerticalOverlapTop is the upward extension for straight vertical strokes so
// they meet font-only top-corner glyphs that stop slightly short of the cell bottom.
const boxVerticalOverlapTop = 1

func boxVerticalExtendMinY(area image.Rectangle) int {
	if area.Min.Y >= boxVerticalOverlapTop {
		return area.Min.Y - boxVerticalOverlapTop
	}
	return area.Min.Y
}

func extendBoxBothInk(area, ink image.Rectangle, ext BoxStrokeExtents) image.Rectangle {
	out := ink
	if ext.Right {
		out.Max.X = area.Max.X
	}
	if ext.Left {
		out.Min.X = area.Min.X
	}
	if ext.Down {
		out.Max.Y = area.Max.Y
	}
	if ext.Up {
		out.Min.Y = area.Min.Y
	}
	return out
}

// ExtendBoxInkToEdges stretches box ink bounds to the cell edges for the
// given stroke axis. r identifies junction glyphs; ref is the glyph draw
// bounds used when rune semantics are unavailable.
func ExtendBoxInkToEdges(area, ink image.Rectangle, axis BoxStrokeAxis, r rune, ref image.Rectangle) image.Rectangle {
	switch axis {
	case BoxStrokeVertical:
		return image.Rect(ink.Min.X, boxVerticalExtendMinY(area), ink.Max.X, area.Max.Y)
	case BoxStrokeHorizontal:
		return image.Rect(area.Min.X, ink.Min.Y, area.Max.X, ink.Max.Y)
	case BoxStrokeBoth:
		ext, ok := BoxStrokeExtentsFromRune(r)
		if !ok {
			ext = boxStrokeExtentsFromInk(ref, ink)
		}
		return extendBoxBothInk(area, ink, ext)
	default:
		return ink
	}
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

func buildBoxGlyphMask(face font.Face, dot fixed.Point26_6, area image.Rectangle, r rune, metrics types.Metrics) (boxGlyphMask, bool) {
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() || mask == nil {
		return boxGlyphMask{}, false
	}
	alphaImg := alpha.RasterGlyph(dr, mask, maskp)
	ink, ok := alpha.InkBounds(alphaImg)
	if !ok {
		return boxGlyphMask{}, false
	}

	thickness := metrics.BoxThickness.Int()
	axis := boxStrokeAxisFromInk(ink)

	// Junction glyphs render the font shape only; adjacent straight strokes
	// extend into this cell for continuity.
	if IsBoxJunctionRune(r) {
		rx, ry := boxGlyphDilateRadius(axis, ink, thickness)
		var out boxGlyphMask
		if rx == 0 && ry == 0 {
			out = boxGlyphMask{dr: dr, alpha: alphaImg}
		} else {
			dilated := alpha.Dilate(alphaImg, rx, ry)
			out = boxGlyphMask{
				dr:    image.Rect(dr.Min.X-rx, dr.Min.Y-ry, dr.Max.X+rx, dr.Max.Y+ry),
				alpha: dilated,
			}
		}
		return out, true
	}

	absInk := ink.Add(dr.Min)
	extendedInk := ExtendBoxInkToEdges(area, absInk, axis, r, dr)
	extendedAlpha := extendBoxAlphaToEdges(area, dr, alphaImg, ink, axis, extendedInk)

	localInk := extendedInk.Sub(extendedInk.Min)
	rx, ry := boxGlyphDilateRadius(axis, localInk, thickness)
	var out boxGlyphMask
	if rx == 0 && ry == 0 {
		out = boxGlyphMask{dr: extendedInk, alpha: extendedAlpha}
	} else {
		dilated := alpha.Dilate(extendedAlpha, rx, ry)
		out = boxGlyphMask{
			dr:    image.Rect(extendedInk.Min.X-rx, extendedInk.Min.Y-ry, extendedInk.Max.X+rx, extendedInk.Max.Y+ry),
			alpha: dilated,
		}
	}
	return out, true
}

func extendBoxAlphaToEdges(area, dr image.Rectangle, alphaImg *image.Alpha, ink image.Rectangle, axis BoxStrokeAxis, extendedInk image.Rectangle) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, extendedInk.Dx(), extendedInk.Dy()))
	absInk := ink.Add(dr.Min)

	switch axis {
	case BoxStrokeVertical:
		for x := absInk.Min.X; x < absInk.Max.X; x++ {
			a := boxColumnMaxAlpha(alphaImg, ink, x-dr.Min.X)
			for y := boxVerticalExtendMinY(area); y < area.Max.Y; y++ {
				boxSetAlpha(out, extendedInk, x, y, a)
			}
		}
	case BoxStrokeHorizontal:
		for y := absInk.Min.Y; y < absInk.Max.Y; y++ {
			a := boxRowMaxAlpha(alphaImg, ink, y-dr.Min.Y)
			for x := area.Min.X; x < area.Max.X; x++ {
				boxSetAlpha(out, extendedInk, x, y, a)
			}
		}
	case BoxStrokeBoth:
		for y := ink.Min.Y; y < ink.Max.Y; y++ {
			for x := ink.Min.X; x < ink.Max.X; x++ {
				boxSetAlpha(out, extendedInk, x+dr.Min.X, y+dr.Min.Y, alphaImg.AlphaAt(x, y).A)
			}
		}
		ext := boxStrokeExtentsFromInk(dr, absInk)
		if ext.Right {
			for y := absInk.Min.Y; y < absInk.Max.Y; y++ {
				a := boxRowMaxAlpha(alphaImg, ink, y-dr.Min.Y)
				for x := absInk.Max.X; x < area.Max.X; x++ {
					boxSetAlpha(out, extendedInk, x, y, a)
				}
			}
		}
		if ext.Left {
			for y := absInk.Min.Y; y < absInk.Max.Y; y++ {
				a := boxRowMaxAlpha(alphaImg, ink, y-dr.Min.Y)
				for x := area.Min.X; x < absInk.Min.X; x++ {
					boxSetAlpha(out, extendedInk, x, y, a)
				}
			}
		}
		if ext.Down {
			for x := absInk.Min.X; x < absInk.Max.X; x++ {
				a := boxColumnMaxAlpha(alphaImg, ink, x-dr.Min.X)
				for y := absInk.Max.Y; y < area.Max.Y; y++ {
					boxSetAlpha(out, extendedInk, x, y, a)
				}
			}
		}
		if ext.Up {
			for x := absInk.Min.X; x < absInk.Max.X; x++ {
				a := boxColumnMaxAlpha(alphaImg, ink, x-dr.Min.X)
				for y := area.Min.Y; y < absInk.Min.Y; y++ {
					boxSetAlpha(out, extendedInk, x, y, a)
				}
			}
		}
	}
	return out
}

func boxSetAlpha(out *image.Alpha, bounds image.Rectangle, x, y int, a uint8) {
	alpha.SetMax(out, x-bounds.Min.X, y-bounds.Min.Y, a)
}

func boxRowMaxAlpha(alphaImg *image.Alpha, ink image.Rectangle, y int) uint8 {
	var maxA uint8
	for x := ink.Min.X; x < ink.Max.X; x++ {
		if a := alphaImg.AlphaAt(x, y).A; a > maxA {
			maxA = a
		}
	}
	return maxA
}

func boxColumnMaxAlpha(alphaImg *image.Alpha, ink image.Rectangle, x int) uint8 {
	var maxA uint8
	for y := ink.Min.Y; y < ink.Max.Y; y++ {
		if a := alphaImg.AlphaAt(x, y).A; a > maxA {
			maxA = a
		}
	}
	return maxA
}
