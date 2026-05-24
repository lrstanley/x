// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonttest

import (
	"image"
	"image/draw"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// GlyphMask captures rasterized glyph ink bounds and a compact signature for tests.
type GlyphMask struct {
	Ink       image.Rectangle
	Signature string
}

// MustGlyphAdvance returns the horizontal advance for r or fails the test.
func MustGlyphAdvance(t *testing.T, face font.Face, r rune) fixed.Int26_6 {
	t.Helper()
	advance, ok := face.GlyphAdvance(r)
	if !ok {
		t.Fatalf("missing glyph advance for %q", r)
	}
	return advance
}

// GlyphMaskForFace rasterizes r with face and returns ink bounds plus a signature.
func GlyphMaskForFace(t *testing.T, face font.Face, r rune) GlyphMask {
	t.Helper()
	dr, mask, maskp, _, ok := face.Glyph(fixed.Point26_6{}, r)
	if !ok {
		t.Fatalf("missing glyph for %q", r)
	}
	alpha := image.NewAlpha(dr)
	draw.DrawMask(alpha, dr, image.Opaque, image.Point{}, mask, maskp, draw.Src)
	ink, ok := AlphaInkBounds(alpha)
	if !ok {
		t.Fatalf("empty glyph ink for %q", r)
	}
	return GlyphMask{Ink: ink, Signature: AlphaSignature(alpha, ink)}
}

// AlphaInkBounds returns the tight bounding box of non-zero alpha pixels.
func AlphaInkBounds(alpha *image.Alpha) (image.Rectangle, bool) {
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

// AlphaSignature renders ink within bounds as a run-length-friendly ASCII grid.
func AlphaSignature(alpha *image.Alpha, bounds image.Rectangle) string {
	var out strings.Builder
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if alpha.AlphaAt(x, y).A > 0 {
				out.WriteByte('#')
			} else {
				out.WriteByte('.')
			}
		}
	}
	return out.String()
}

// FaceGlyphSignature returns a compact raster signature for r on face.
func FaceGlyphSignature(t *testing.T, face font.Face, r rune) string {
	t.Helper()
	return GlyphMaskForFace(t, face, r).Signature
}

// AssertSyntheticFaceDiff verifies styled keeps regular advance but differs in ink.
func AssertSyntheticFaceDiff(t *testing.T, regular, styled font.Face, r rune) {
	t.Helper()
	if got, want := MustGlyphAdvance(t, styled, r), MustGlyphAdvance(t, regular, r); got != want {
		t.Fatalf("styled advance = %v, want regular advance %v", got, want)
	}
	regularSig := FaceGlyphSignature(t, regular, r)
	styledSig := FaceGlyphSignature(t, styled, r)
	if styledSig == regularSig {
		t.Fatalf("styled glyph %q matched regular glyph", r)
	}
}
