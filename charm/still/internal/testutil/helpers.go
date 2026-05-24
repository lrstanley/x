// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package testutil provides helpers for asserting glyph raster output and
// synthetic font face behavior in still tests.
package testutil

import (
	"image"
	"image/draw"
	"strings"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/alpha"
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
	alphaImg := image.NewAlpha(dr)
	draw.DrawMask(alphaImg, dr, image.Opaque, image.Point{}, mask, maskp, draw.Src)
	ink, ok := alpha.InkBounds(alphaImg)
	if !ok {
		t.Fatalf("empty glyph ink for %q", r)
	}
	return GlyphMask{Ink: ink, Signature: AlphaSignature(alphaImg, ink)}
}

// AlphaInkBounds returns the tight bounding box of non-zero alpha pixels.
func AlphaInkBounds(a *image.Alpha) (image.Rectangle, bool) {
	return alpha.InkBounds(a)
}

// AlphaSignature renders ink within bounds as a run-length-friendly ASCII grid.
func AlphaSignature(alphaImg *image.Alpha, bounds image.Rectangle) string {
	var out strings.Builder
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if alphaImg.AlphaAt(x, y).A > 0 {
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
