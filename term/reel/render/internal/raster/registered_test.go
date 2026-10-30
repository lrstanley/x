// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"testing"

	"github.com/lrstanley/x/term/reel/render/internal/alpha"
	"github.com/lrstanley/x/term/reel/vt"
)

// registeredRect is a glyph whose outline is the rectangle x0,y0 to x1,y1,
// in a 1000 unit em.
func registeredRect(x0, y0, x1, y1 int32) vt.Glyph {
	return vt.Glyph{
		Rune: 0xe000,
		Contours: [][]vt.GlyphPoint{{
			{X: x0, Y: y0, OnCurve: true},
			{X: x0, Y: y1, OnCurve: true},
			{X: x1, Y: y1, OnCurve: true},
			{X: x1, Y: y0, OnCurve: true},
		}},
		UnitsPerEm: 1000,
		Advance:    1000,
		LineHeight: 1000,
		Cells:      1,
	}
}

// TestRegisteredGlyphMask checks registered glyphs are placed as Rio places
// them, in 10x20 cells: scaled so one em fits the span's shorter side and the
// outline fits the span, centered, whatever the sizing options say.
func TestRegisteredGlyphMask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		glyph vt.Glyph
		edit  func(g *vt.Glyph)
		cells int
		want  image.Rectangle
	}{
		{"an em is the cell width", registeredRect(0, 0, 1000, 1000), nil, 1, image.Rect(0, 5, 10, 15)},
		{"an off-center outline is centered", registeredRect(100, 600, 300, 900), nil, 1, image.Rect(4, 8, 6, 12)},
		{"wider than the em shrinks to fit", registeredRect(-1000, -1000, 2000, 2000), nil, 1, image.Rect(0, 5, 10, 15)},
		{"taller than the cell shrinks to fit", registeredRect(0, -1000, 100, 2000), nil, 1, image.Rect(4, 0, 6, 20)},
		{"sizing options are ignored", registeredRect(0, 0, 1000, 1000), func(g *vt.Glyph) {
			g.Size, g.AlignX, g.AlignY = vt.GlyphSizeStretch, vt.GlyphAlignStart, vt.GlyphAlignBaseline
			g.Advance, g.LineHeight, g.Pad = 500, 2000, [4]float64{0.25, 0.25, 0.25, 0.25}
		}, 1, image.Rect(0, 5, 10, 15)},
		{"two cells: an em is the cell height", registeredRect(0, 0, 1000, 1000), nil, 2, image.Rect(0, 0, 20, 20)},
		{"two cells, centered", registeredRect(0, 0, 1000, 1000), func(g *vt.Glyph) { g.UnitsPerEm = 2000 }, 2, image.Rect(5, 5, 15, 15)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := tt.glyph
			g.Cells = tt.cells
			if tt.edit != nil {
				tt.edit(&g)
			}
			area := image.Rect(30, 40, 40, 60)
			l := Layout{
				Kind:   KindRegistered,
				Glyph:  &g,
				Area:   area,
				Target: image.Rectangle{Min: area.Min, Max: area.Min.Add(image.Pt(10*tt.cells, 20))},
			}
			mask, ok := registeredGlyphMask(&l)
			if !ok {
				t.Fatal("no mask")
			}
			got, ok := alpha.InkBounds(mask.alpha)
			if !ok || got.Add(mask.offset) != tt.want {
				t.Fatalf("ink = %v (%v), want %v", got.Add(mask.offset), ok, tt.want)
			}
		})
	}
}

// TestRegisteredGlyphMaskDegenerate checks outlines with no points, or no
// area, draw nothing without failing.
func TestRegisteredGlyphMaskDegenerate(t *testing.T) {
	t.Parallel()

	area := image.Rect(0, 0, 10, 20)
	empty := registeredRect(0, 0, 0, 0)
	empty.Contours = nil
	if _, ok := registeredGlyphMask(&Layout{Kind: KindRegistered, Glyph: &empty, Area: area, Target: area}); ok {
		t.Error("an outline with no points drew a mask")
	}
	for _, g := range []vt.Glyph{registeredRect(500, 0, 500, 1000), registeredRect(0, 500, 1000, 500), registeredRect(500, 500, 500, 500)} {
		mask, ok := registeredGlyphMask(&Layout{Kind: KindRegistered, Glyph: &g, Area: area, Target: area})
		if !ok {
			t.Fatalf("%v: no mask", g.Contours)
		}
		if b, inked := alpha.InkBounds(mask.alpha); inked {
			t.Errorf("%v: ink at %v, want none", g.Contours, b)
		}
	}
}

// TestTraceContourOffCurve checks a contour of only off-curve points, which
// starts between its last and first points, draws a closed curve.
func TestTraceContourOffCurve(t *testing.T) {
	t.Parallel()

	g := vt.Glyph{
		Contours: [][]vt.GlyphPoint{{
			{X: 0, Y: -200}, {X: 0, Y: 800}, {X: 1000, Y: 800}, {X: 1000, Y: -200},
		}},
		UnitsPerEm: 1000, Cells: 1,
	}
	l := Layout{Kind: KindRegistered, Glyph: &g, Area: image.Rect(0, 0, 20, 20), Target: image.Rect(0, 0, 20, 20)}
	mask, ok := registeredGlyphMask(&l)
	if !ok {
		t.Fatal("no mask")
	}
	if a := mask.alpha.AlphaAt(10, 10).A; a != 0xff {
		t.Errorf("center alpha = %d, want filled", a)
	}
	for _, p := range []image.Point{{0, 0}, {19, 0}, {0, 19}, {19, 19}} {
		if a := mask.alpha.AlphaAt(p.X, p.Y).A; a != 0 {
			t.Errorf("corner %v alpha = %d, want the curve inside it", p, a)
		}
	}
}
