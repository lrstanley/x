// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestBoxThicknessOverrideThickensHorizontalLine(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	scr := newTestScreen(1, 1)
	scr.SetCell(0, 0, &uv.Cell{
		Content: "─",
		Width:   1,
		Style:   uv.Style{Fg: fg, Bg: bg},
	})

	base := MustNew(WithFontSizePt(14))
	thick := MustNew(WithFontSizePt(14), WithBoxThickness(4))

	baseCell := base.contextLocked(image.Point{}, scr).CellBounds(0, 0)
	thickCell := thick.contextLocked(image.Point{}, scr).CellBounds(0, 0)
	baseInk, ok := inkBounds(drawNRGBA(t, base, scr), baseCell, bg)
	if !ok {
		t.Fatal("default horizontal box line produced no ink")
	}
	thickInk, ok := inkBounds(drawNRGBA(t, thick, scr), thickCell, bg)
	if !ok {
		t.Fatal("thickened horizontal box line produced no ink")
	}

	if thickInk.Dy() <= baseInk.Dy() {
		t.Fatalf("thickened ink height = %d, want greater than default %d", thickInk.Dy(), baseInk.Dy())
	}
	if thickInk.Dy() < 4 {
		t.Fatalf("thickened ink height = %d, want at least 4px", thickInk.Dy())
	}
}

func TestBoxGlyphDilateRadius(t *testing.T) {
	t.Parallel()

	ink := image.Rect(0, 0, 20, 1)
	if rx, ry := boxGlyphDilateRadius(boxStrokeHorizontal, ink, 4); rx != 0 || ry != 2 {
		t.Fatalf("horizontal radius = (%d,%d), want (0,2)", rx, ry)
	}

	ink = image.Rect(0, 0, 1, 20)
	if rx, ry := boxGlyphDilateRadius(boxStrokeVertical, ink, 4); rx != 2 || ry != 0 {
		t.Fatalf("vertical radius = (%d,%d), want (2,0)", rx, ry)
	}

	ink = image.Rect(0, 0, 2, 2)
	if rx, ry := boxGlyphDilateRadius(boxStrokeBoth, ink, 4); rx != 1 || ry != 1 {
		t.Fatalf("both radius = (%d,%d), want (1,1)", rx, ry)
	}
}
