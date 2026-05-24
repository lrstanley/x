// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"image"
	"io"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/testutil"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestSyntheticFace(t *testing.T) {
	t.Run("bold wider ink same advance", func(t *testing.T) {
		base := newSyntheticTestFace(t)
		bold := SyntheticFace(base, true, false, 24)

		if got, want := testutil.MustGlyphAdvance(t, bold, 'H'), testutil.MustGlyphAdvance(t, base, 'H'); got != want {
			t.Fatalf("bold advance = %v, want regular advance %v", got, want)
		}

		regularMask := testutil.GlyphMaskForFace(t, base, 'H')
		boldMask := testutil.GlyphMaskForFace(t, bold, 'H')
		if boldMask.Ink.Dx() <= regularMask.Ink.Dx() {
			t.Fatalf("bold ink width = %d, want wider than regular %d", boldMask.Ink.Dx(), regularMask.Ink.Dx())
		}
		if boldMask.Signature == regularMask.Signature {
			t.Fatal("bold ink matched regular ink")
		}
	})

	t.Run("italic shears ink same advance", func(t *testing.T) {
		base := newSyntheticTestFace(t)
		italic := SyntheticFace(base, false, true, 24)

		if got, want := testutil.MustGlyphAdvance(t, italic, 'H'), testutil.MustGlyphAdvance(t, base, 'H'); got != want {
			t.Fatalf("italic advance = %v, want regular advance %v", got, want)
		}

		regularMask := testutil.GlyphMaskForFace(t, base, 'H')
		italicMask := testutil.GlyphMaskForFace(t, italic, 'H')
		if italicMask.Ink.Max.X <= regularMask.Ink.Max.X {
			t.Fatalf("italic ink max x = %d, want greater than regular %d", italicMask.Ink.Max.X, regularMask.Ink.Max.X)
		}
		if italicMask.Signature == regularMask.Signature {
			t.Fatal("italic ink matched regular ink")
		}
	})

	t.Run("bold italic combines effects", func(t *testing.T) {
		base := newSyntheticTestFace(t)
		bold := SyntheticFace(base, true, false, 24)
		italic := SyntheticFace(base, false, true, 24)
		boldItalic := SyntheticFace(base, true, true, 24)

		if got, want := testutil.MustGlyphAdvance(t, boldItalic, 'H'), testutil.MustGlyphAdvance(t, base, 'H'); got != want {
			t.Fatalf("bold italic advance = %v, want regular advance %v", got, want)
		}

		boldMask := testutil.GlyphMaskForFace(t, bold, 'H')
		italicMask := testutil.GlyphMaskForFace(t, italic, 'H')
		boldItalicMask := testutil.GlyphMaskForFace(t, boldItalic, 'H')
		if boldItalicMask.Ink.Dx() <= italicMask.Ink.Dx() {
			t.Fatalf("bold italic ink width = %d, want wider than italic %d", boldItalicMask.Ink.Dx(), italicMask.Ink.Dx())
		}
		if boldItalicMask.Ink.Max.X <= boldMask.Ink.Max.X {
			t.Fatalf("bold italic ink max x = %d, want shifted beyond bold %d", boldItalicMask.Ink.Max.X, boldMask.Ink.Max.X)
		}
		for name, sig := range map[string]string{
			"bold":   boldMask.Signature,
			"italic": italicMask.Signature,
		} {
			if boldItalicMask.Signature == sig {
				t.Fatalf("bold italic ink matched %s ink", name)
			}
		}
	})

	t.Run("does not own base face", func(t *testing.T) {
		base := &closeCountingFace{}
		faces := map[string]font.Face{
			"bold":        SyntheticFace(base, true, false, 11),
			"italic":      SyntheticFace(base, false, true, 11),
			"bold italic": SyntheticFace(base, true, true, 11),
		}

		for name, face := range faces {
			closer, ok := face.(io.Closer)
			if !ok {
				t.Fatalf("%s synthetic face does not implement Close", name)
			}
			if _, advanceOK := face.GlyphAdvance('x'); !advanceOK {
				t.Fatalf("%s synthetic face did not delegate GlyphAdvance", name)
			}
			if err := closer.Close(); err != nil {
				t.Fatalf("%s synthetic face Close() error = %v", name, err)
			}
		}
		if base.closes != 0 {
			t.Fatalf("base face closes = %d, want 0", base.closes)
		}
		if err := base.Close(); err != nil {
			t.Fatal(err)
		}
		if base.closes != 1 {
			t.Fatalf("base face closes = %d, want 1", base.closes)
		}
	})
}

func newSyntheticTestFace(t *testing.T) font.Face {
	t.Helper()
	tf, err := Load(embeddedFixture)
	if err != nil {
		t.Fatal(err)
	}
	face, err := NewFace(tf, &opentype.FaceOptions{Size: 24, DPI: 96, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := face.(io.Closer); ok {
			_ = c.Close()
		}
	})
	return face
}

type closeCountingFace struct {
	closes int
}

func (f *closeCountingFace) Glyph(fixed.Point26_6, rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	alpha := image.NewAlpha(image.Rect(0, 0, 1, 1))
	alpha.Pix[0] = 0xff
	bounds := alpha.Bounds()
	return bounds, alpha, image.Point{}, fixed.I(1), true
}

func (f *closeCountingFace) GlyphBounds(rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return fixed.Rectangle26_6{Max: fixed.Point26_6{X: fixed.I(1), Y: fixed.I(1)}}, fixed.I(1), true
}

func (f *closeCountingFace) GlyphAdvance(rune) (fixed.Int26_6, bool) {
	return fixed.I(1), true
}

func (f *closeCountingFace) Kern(rune, rune) fixed.Int26_6 {
	return 0
}

func (f *closeCountingFace) Metrics() font.Metrics {
	return font.Metrics{Height: fixed.I(1)}
}

func (f *closeCountingFace) Close() error {
	f.closes++
	return nil
}
