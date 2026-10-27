// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"testing"
)

func TestBoxHalfLinesReachOnlyTheirEdges(t *testing.T) {
	t.Parallel()

	type edges struct{ left, right, up, down bool }
	tests := map[string]edges{
		"╴": {left: true}, "╸": {left: true},
		"╶": {right: true}, "╺": {right: true},
		"╵": {up: true}, "╹": {up: true},
		"╷": {down: true}, "╻": {down: true},
		"╼": {left: true, right: true}, "╾": {left: true, right: true},
		"╽": {up: true, down: true}, "╿": {up: true, down: true},
	}
	fg, bg, palette := blockTestPalette()
	for glyph, want := range tests {
		for _, size := range []int{12, 14, 20} {
			d := MustNew(WithScale(scaleFor(size)), palette)
			scr := blockRow(glyph, 1, fg, bg)
			img := drawNRGBA(t, d, scr)
			cell := d.layoutSnapshot(t, image.Point{}, scr).cellBounds(0, 0)

			ink, ok := inkBounds(img, cell, bg)
			if !ok {
				t.Fatalf("size=%d %s: no ink", size, glyph)
			}
			got := edges{
				left:  ink.Min.X == cell.Min.X,
				right: ink.Max.X == cell.Max.X,
				up:    ink.Min.Y == cell.Min.Y,
				down:  ink.Max.Y == cell.Max.Y,
			}
			if got != want {
				t.Errorf("size=%d %s: ink %v in cell %v reaches %+v, want %+v", size, glyph, ink, cell, got, want)
			}
			// A stroke is thin across its axis: no stub grows off it.
			if horizontal := want.left || want.right; horizontal && ink.Dy() > cell.Dy()/3 {
				t.Errorf("size=%d %s: ink %v is %d px tall in a %d px cell", size, glyph, ink, ink.Dy(), cell.Dy())
			} else if !horizontal && ink.Dx() > cell.Dx()/2 {
				t.Errorf("size=%d %s: ink %v is %d px wide in a %d px cell", size, glyph, ink, ink.Dx(), cell.Dx())
			}
		}
	}
}

func TestBoxDashedLinesKeepGaps(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	for _, glyph := range []string{"┄", "┈", "╌", "┅"} {
		d := MustNew(WithScale(scaleFor(20)), palette)
		scr := blockRow(glyph, 1, fg, bg)
		img := drawNRGBA(t, d, scr)
		cell := d.layoutSnapshot(t, image.Point{}, scr).cellBounds(0, 0)

		// Some row crosses ink, then a gap, then ink again. Gaps between small
		// dashes are antialiased, so ink is anything past halfway to fg.
		dashed := false
		for y := cell.Min.Y; y < cell.Max.Y && !dashed; y++ {
			runs := fgRuns(cell.Dx(), func(i int) bool {
				return int(img.NRGBAAt(cell.Min.X+i, y).R) > (int(fg.R)+int(bg.R))/2
			})
			dashed = len(runs) > 1
		}
		if !dashed {
			t.Errorf("%s: no row has more than one dash", glyph)
		}
	}
}
