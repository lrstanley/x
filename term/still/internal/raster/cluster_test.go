// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/term/still/fonts"
	"github.com/lrstanley/x/term/still/internal/alpha"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func clusterTestFace(t *testing.T) font.Face {
	t.Helper()

	family, err := fonts.DefaultFamily()
	if err != nil {
		t.Fatal(err)
	}
	face, err := fonts.NewFace(family.Regular, &opentype.FaceOptions{Size: 24, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = face.Close() })
	return face
}

func clusterTestLayout(face font.Face, cluster string) *Layout {
	c, _ := idraw.NewCluster(cluster)
	area := image.Rect(20, 20, 40, 50)
	return &Layout{
		Kind:    KindGrid,
		Face:    face,
		Cluster: c,
		Fg:      color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Area:    area,
		Dot:     fixed.P(area.Min.X+2, area.Max.Y-8),
	}
}

// drawCluster draws cluster the way the cursor cell does, outside the
// coverage pass.
func drawCluster(face font.Face, cluster string) *image.Alpha {
	img := image.NewAlpha(image.Rect(0, 0, 60, 70))
	DrawGlyphLayout(nil, img, clusterTestLayout(face, cluster))
	return img
}

func TestMarkedGlyphMaskKeepsBaseAndMark(t *testing.T) {
	t.Parallel()

	face := clusterTestFace(t)
	base, ok := buildGlyphLayoutMask(clusterTestLayout(face, "x"))
	if !ok {
		t.Fatal("no mask for x")
	}
	marked, ok := buildGlyphLayoutMask(clusterTestLayout(face, "x\u0301"))
	if !ok {
		t.Fatal("no mask for x + U+0301")
	}

	baseDr := base.alpha.Bounds().Add(base.offset)
	markedDr := marked.alpha.Bounds().Add(marked.offset)
	if markedDr.Min.Y >= baseDr.Min.Y || markedDr.Max.Y != baseDr.Max.Y {
		t.Fatalf("marked ink = %v, want the base's %v extended upward", markedDr, baseDr)
	}
	// Every pixel of the base survives in the combined mask.
	for y := baseDr.Min.Y; y < baseDr.Max.Y; y++ {
		for x := baseDr.Min.X; x < baseDr.Max.X; x++ {
			p := image.Pt(x, y)
			want := alpha.At(base.alpha, p.Sub(base.offset))
			if got := alpha.At(marked.alpha, p.Sub(marked.offset)); got < want {
				t.Fatalf("pixel %v: alpha %d, want at least the base's %d", p, got, want)
			}
		}
	}
}

func TestMarkedGlyphMaskSkipsMissingMark(t *testing.T) {
	t.Parallel()

	face := clusterTestFace(t)
	base, _ := buildGlyphLayoutMask(clusterTestLayout(face, "x"))

	// U+10A0F has no glyph in the font, which would draw .notdef.
	got, ok := buildGlyphLayoutMask(clusterTestLayout(face, "x\U00010a0f"))
	if !ok {
		t.Fatal("no mask for x + missing mark")
	}
	if got.alpha.Bounds() != base.alpha.Bounds() || got.offset != base.offset {
		t.Fatalf("missing mark changed the mask: %v at %v, want %v at %v",
			got.alpha.Bounds(), got.offset, base.alpha.Bounds(), base.offset)
	}
}

func TestMarkOnBlankBaseDrawsMark(t *testing.T) {
	t.Parallel()

	face := clusterTestFace(t)
	if _, ok := buildGlyphLayoutMask(clusterTestLayout(face, " ")); ok {
		t.Fatal("blank base has a mask")
	}
	if _, ok := buildGlyphLayoutMask(clusterTestLayout(face, " \u0301")); !ok {
		t.Fatal("mark over a blank base has no mask")
	}
}

func TestDrawGlyphLayoutDrawsOnlyBaseAndMarks(t *testing.T) {
	t.Parallel()

	face := clusterTestFace(t)
	plain := drawCluster(face, "x")

	// Selectors and joiners have no glyph: the cluster draws like its base.
	for _, cluster := range []string{"x\ufe0e", "x\ufe0f", "x\u200dy"} {
		if !bytes.Equal(drawCluster(face, cluster).Pix, plain.Pix) {
			t.Errorf("%q does not draw like x", cluster)
		}
	}

	accent, ok := alpha.InkBounds(drawCluster(face, "x\u0301"))
	if base, _ := alpha.InkBounds(plain); !ok || accent.Min.Y >= base.Min.Y || accent.Max.Y != base.Max.Y {
		t.Errorf("x + U+0301 ink = %v, want the base's %v extended upward", accent, base)
	}
}
