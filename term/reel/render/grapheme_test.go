// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"testing"

	"github.com/lrstanley/x/term/reel/vt/screen"
)

// clusterRow draws one cell per cluster and returns the ink signature and ink
// bounds of each cell.
func clusterRow(t *testing.T, clusters ...string) (sigs []string, ink []image.Rectangle) {
	t.Helper()

	fg, bg, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(24)), WithSystemFontFallback(false), palette)
	scr := newTestScreen(len(clusters), 1)
	for x, c := range clusters {
		scr.SetCell(x, 0, &screen.Cell{Content: c, Width: 1, Style: screen.Style{Fg: fg, Bg: bg}})
	}
	img := drawNRGBA(t, d, scr)
	layout := d.layoutSnapshot(t, image.Point{}, scr)
	for x := range clusters {
		cell := layout.cellBounds(x, 0)
		sigs = append(sigs, cellInkSignature(img, cell, bg))
		b, _ := inkBounds(img, cell, bg)
		ink = append(ink, b)
	}
	return sigs, ink
}

func TestCombiningMarkIsDrawn(t *testing.T) {
	t.Parallel()

	// No font has a precomposed x with an acute, so the mark is drawn from its
	// own glyph over the base. The middle cell sits between two plain ones to
	// show the mask cache keeps the clusters apart and the mark stays in its
	// cell.
	sigs, ink := clusterRow(t, "x", "x\u0301", "x", "x\u0301\u0323")
	if sigs[0] != sigs[2] {
		t.Fatal("same cluster drew differently in two cells")
	}
	if sigs[0] == sigs[1] {
		t.Fatal("x + U+0301 drew like x: the mark was dropped")
	}
	if ink[1].Min.Y >= ink[0].Min.Y {
		t.Fatalf("x + U+0301 ink top = %d, want above the bare x at %d", ink[1].Min.Y, ink[0].Min.Y)
	}
	if ink[1].Max.Y != ink[0].Max.Y {
		t.Fatalf("x + U+0301 ink bottom = %d, want the base's %d", ink[1].Max.Y, ink[0].Max.Y)
	}
	if sigs[3] == sigs[1] {
		t.Fatal("a second mark did not change the cell")
	}
}

func TestDecomposedAccentMatchesPrecomposed(t *testing.T) {
	t.Parallel()

	sigs, ink := clusterRow(t, "e", "e\u0301", "é")
	if sigs[1] != sigs[2] {
		t.Fatal("e + U+0301 does not render like é")
	}
	if ink[1].Min.Y >= ink[0].Min.Y {
		t.Fatalf("e + U+0301 lost its accent: ink top = %d, bare e = %d", ink[1].Min.Y, ink[0].Min.Y)
	}
}

func TestSelectorsAndJoinersAddNoGlyph(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster string
		want    string
	}{
		{"text presentation", "a\ufe0e", "a"},
		{"emoji presentation", "a\ufe0f", "a"},
		{"zwj sequence", "a\u200db\u200dc", "a"},
		{"emoji zwj sequence", "\U0001f468\u200d\U0001f469\u200d\U0001f467", "\U0001f468"},
		{"mark after selector", "x\ufe0f\u0301", "x\u0301"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sigs, _ := clusterRow(t, tt.cluster, tt.want)
			if sigs[0] != sigs[1] {
				t.Fatalf("%q does not render like %q", tt.cluster, tt.want)
			}
		})
	}
}

func TestMarkOnBlankBaseIsDrawn(t *testing.T) {
	t.Parallel()

	_, ink := clusterRow(t, " ", " \u0301")
	if !ink[0].Empty() || ink[1].Empty() {
		t.Fatalf("blank = %v, blank + mark = %v, want ink only from the mark", ink[0], ink[1])
	}
}
