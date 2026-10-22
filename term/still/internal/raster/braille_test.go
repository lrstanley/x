// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"bytes"
	"image"
	"testing"

	"github.com/lrstanley/x/term/still/internal/alpha"
)

// brailleSizes are cell sizes from small to large, including odd ones.
var brailleSizes = [][2]int{{5, 10}, {6, 13}, {7, 15}, {8, 17}, {9, 19}, {11, 23}, {12, 26}, {14, 29}, {17, 35}, {20, 42}}

func inkCount(a *image.Alpha) int {
	return len(a.Pix) - bytes.Count(a.Pix, []byte{0})
}

func TestBrailleGridFitsCell(t *testing.T) {
	t.Parallel()

	for w := 4; w <= 40; w++ {
		for h := 2 * w; h <= 2*w+w/2+2; h++ {
			dot, xs, ys := brailleGrid(w, h)
			if dot < 1 {
				t.Fatalf("%dx%d: dot size %d", w, h, dot)
			}
			if xs[0] < 0 || xs[1]+dot > w || xs[1] < xs[0]+dot {
				t.Fatalf("%dx%d: columns %v (dot %d) do not fit", w, h, xs, dot)
			}
			if ys[0] < 0 || ys[3]+dot > h {
				t.Fatalf("%dx%d: rows %v (dot %d) do not fit", w, h, ys, dot)
			}
		}
	}
}

func TestBrailleDots(t *testing.T) {
	t.Parallel()

	for _, sz := range brailleSizes {
		w, h := sz[0], sz[1]
		dot, xs, ys := brailleGrid(w, h)

		if n := inkCount(spriteMask(t, 0x2800, w, h)); n != 0 {
			t.Errorf("%dx%d: U+2800 has %d ink pixels, want none", w, h, n)
		}
		if n := inkCount(spriteMask(t, 0x28FF, w, h)); n != 8*dot*dot {
			t.Errorf("%dx%d: ⣿ has %d ink pixels, want %d", w, h, n, 8*dot*dot)
		}

		// Each single dot is one dot-sized square at its grid position, in its
		// column and in the matching vertical half of the cell.
		for i, pos := range brailleDots {
			m := spriteMask(t, 0x2800+rune(1<<i), w, h)
			ink, ok := alpha.InkBounds(m)
			want := image.Rect(xs[pos[0]], ys[pos[1]], xs[pos[0]]+dot, ys[pos[1]]+dot)
			if !ok || ink != want || inkCount(m) != dot*dot {
				t.Errorf("%dx%d: dot %d ink %v, want %v", w, h, i+1, ink, want)
				continue
			}
			if left := ink.Max.X <= w/2; left != (pos[0] == 0) {
				t.Errorf("%dx%d: dot %d at x %v is in the wrong column", w, h, i+1, ink)
			}
			if top := ink.Max.Y <= h/2; top != (pos[1] < 2) {
				t.Errorf("%dx%d: dot %d at y %v is in the wrong half", w, h, i+1, ink)
			}
		}
		for i := 1; i < len(ys); i++ {
			if ys[i] < ys[i-1]+dot {
				t.Errorf("%dx%d: dot rows %v (dot %d) overlap or are out of order", w, h, ys, dot)
			}
		}

		cases := []struct {
			name string
			r    rune
			want image.Rectangle
			dots int
		}{
			{"⡇ left column", 0x2847, image.Rect(xs[0], ys[0], xs[0]+dot, ys[3]+dot), 4},
			{"⢸ right column", 0x28B8, image.Rect(xs[1], ys[0], xs[1]+dot, ys[3]+dot), 4},
			{"⣉ corners", 0x28C9, image.Rect(xs[0], ys[0], xs[1]+dot, ys[3]+dot), 4},
			{"⣿ full", 0x28FF, image.Rect(xs[0], ys[0], xs[1]+dot, ys[3]+dot), 8},
		}
		for _, tc := range cases {
			m := spriteMask(t, tc.r, w, h)
			ink, _ := alpha.InkBounds(m)
			if ink != tc.want || inkCount(m) != tc.dots*dot*dot {
				t.Errorf("%dx%d: %s ink %v (%d px), want %v (%d px)", w, h, tc.name, ink, inkCount(m), tc.want, tc.dots*dot*dot)
			}
		}
	}
}
