// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"image"
	"image/color"
	"slices"
	"testing"
)

// TestMarkGlyphsAreDrawnOverTheBase covers the emoji Apple draws as two halves:
// glyphs that GDEF classes as marks sit over the glyph before them. Two glyphs
// that are not marks stay refused, since they would be drawn side by side.
func TestMarkGlyphsAreDrawnOverTheBase(t *testing.T) {
	t.Parallel()

	fx := standardFixture("cbdt1")
	fx.marks = []uint16{gidWoman}
	ix := fixtureIndex(t, fx)

	g, ok := lookupColor(ix, []rune("👨👩")) // Man, then woman as a mark.
	if !ok {
		t.Fatal("a base with a mark did not resolve")
	}
	if len(g.layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(g.layers))
	}
	// The mark is drawn last, over the base, and both fill the same box.
	img := g.Image(16, 16)
	if got := img.RGBAAt(8, 8); got != rgba(fxYellow) {
		t.Errorf("center = %v, want the mark %v over the base %v", got, rgba(fxYellow), rgba(fxBlue))
	}

	for _, cluster := range []string{
		"👩👨", // A mark with no base before it.
		"👩👩", // Only marks.
		"👨👍", // A second glyph that is no mark.
	} {
		if refused, found := lookupColor(ix, []rune(cluster)); found {
			t.Errorf("%+q resolved to %d layers, want it refused", cluster, len(refused.layers))
		}
	}

	// Without glyph classes no glyph is a mark.
	if _, found := lookupColor(fixtureIndex(t, standardFixture("cbdt1")), []rune("👨👩")); found {
		t.Error("two glyphs resolved without a font that classes either as a mark")
	}
}

func TestMirror(t *testing.T) {
	t.Parallel()

	// Odd and even widths, and an image that doesn't start at the origin.
	for _, w := range []int{1, 2, 5, 8} {
		img := image.NewRGBA(image.Rect(3, 4, 3+w, 6))
		for y := 4; y < 6; y++ {
			for x := range w {
				img.SetRGBA(3+x, y, color.RGBA{R: uint8(10*x + 1), G: uint8(y), A: 255}) //nolint:gosec // Small test values.
			}
		}
		want := image.NewRGBA(img.Bounds())
		for y := 4; y < 6; y++ {
			for x := range w {
				want.SetRGBA(3+x, y, img.RGBAAt(3+w-1-x, y))
			}
		}
		mirror(img)
		if !slices.Equal(img.Pix, want.Pix) {
			t.Errorf("width %d: mirrored pixels differ from the expected ones", w)
		}
	}
}

// TestSbixFlipIsMirrored makes sure a "flip" record is the glyph it names, to be
// drawn facing the other way.
func TestSbixFlipIsMirrored(t *testing.T) {
	t.Parallel()

	of := openColorReader(bytes.NewReader(standardFixture("sbix").build()), 0)
	if of == nil {
		t.Fatal("fixture did not open")
	}
	src, ok := of.sources[0].(*sbixSource)
	if !ok {
		t.Fatalf("source is %T, want sbix", of.sources[0])
	}
	st := src.strikes[0]
	plainType, plainAt, plainLen, flipped := src.locate(st, gidSmile)
	if flipped {
		t.Error("a plain glyph is flipped")
	}
	typ, at, length, mirrored := src.locate(st, gidSmile3)
	if typ != plainType || !mirrored || at != plainAt || length != plainLen {
		t.Errorf("flip record = %q at %d (%d bytes), flipped %v; want the smile's PNG at %d (%d bytes), flipped", typ, at, length, mirrored, plainAt, plainLen)
	}
}
