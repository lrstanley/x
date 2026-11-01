// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/canvas"
)

// litFrame returns a 20×7 region of white-on-blue text, whose top-left cell
// is at 3,2 of a larger frame.
func litFrame() *canvas.Surface {
	text := strings.TrimSuffix(strings.Repeat("\x1b[38;2;255;255;255;48;2;0;0;255m"+strings.Repeat("x", 20)+"\x1b[m\n", 7), "\n")
	frame := canvas.New(30, 12, ansi.GraphemeWidth)
	cells := canvas.Parse(text, ansi.GraphemeWidth)
	for y := range cells.Height() {
		for x := range cells.Width() {
			frame.SetCell(3+x, 2+y, cells.CellAt(x, y))
		}
	}
	return frame.Region(image.Rect(3, 2, 23, 9), env, &canvas.Mixer{})
}

func TestSpotlight(t *testing.T) {
	t.Parallel()

	lit := image.Rect(10, 4, 16, 6) // frame coordinates: local columns 7-12, rows 2-3
	dimmed := func(s *canvas.Surface, x, y int) bool { return rgba(s.Cell(x, y).Style.Fg) != white }

	t.Run("inside and outside", func(t *testing.T) {
		t.Parallel()
		s := litFrame()
		Spotlight{Rect: lit}.Apply(s, 1)
		for y := range s.Height() {
			for x := range s.Width() {
				inside := image.Pt(x+3, y+2).In(lit)
				if inside == dimmed(s, x, y) {
					t.Fatalf("cell %d,%d: inside=%v dimmed=%v", x, y, inside, dimmed(s, x, y))
				}
			}
		}
		// 0.7 of the way from white to black.
		if got := rgba(s.Cell(0, 0).Style.Fg).R; got < 70 || got > 85 {
			t.Fatalf("default amount left red at %d, want about 77", got)
		}
	})

	t.Run("amount and strength", func(t *testing.T) {
		t.Parallel()
		s := litFrame()
		Spotlight{Rect: lit, Amount: 1}.Apply(s, 1)
		if got := rgba(s.Cell(0, 0).Style.Fg); got != black {
			t.Fatalf("amount 1 left %v", got)
		}
		s = litFrame()
		Spotlight{Rect: lit, Amount: 1}.Apply(s, 0)
		if dimmed(s, 0, 0) {
			t.Fatal("t=0 dimmed")
		}
		s = litFrame()
		Spotlight{Rect: lit, Amount: 1}.Apply(s, 0.5)
		if got := rgba(s.Cell(0, 0).Style.Fg).R; got < 120 || got > 135 {
			t.Fatalf("t=0.5 left red at %d, want about 128", got)
		}
	})

	t.Run("soft edge ramps up", func(t *testing.T) {
		t.Parallel()
		s := litFrame()
		Spotlight{Rect: lit, Amount: 1, Soft: 6}.Apply(s, 1)
		prev := 256
		for x := 6; x >= 0; x-- { // moving away from the lit columns, on a lit row
			r := int(rgba(s.Cell(x, 2).Style.Fg).R)
			if r > prev {
				t.Fatalf("column %d is brighter (%d) than the one nearer the light (%d)", x, r, prev)
			}
			prev = r
		}
		if rgba(s.Cell(6, 2).Style.Fg).R == 255 || rgba(s.Cell(6, 2).Style.Fg).R == 0 {
			t.Fatalf("the cell next to the light is %v, want partly dimmed", s.Cell(6, 2).Style.Fg)
		}
		if a, b := rgba(s.Cell(0, 0).Style.Fg), rgba(s.Cell(6, 2).Style.Fg); a.R >= b.R {
			t.Fatalf("far corner is %v, not dimmer than the edge %v", a, b)
		}
		s = litFrame()
		Spotlight{Rect: lit, Amount: 1, Soft: 2}.Apply(s, 1)
		if got := rgba(s.Cell(0, 0).Style.Fg); got != black {
			t.Fatalf("beyond the soft edge is %v, want fully dimmed", got)
		}
		// Horizontal distances are half: two columns away is one row away.
		if a, b := rgba(s.Cell(5, 2).Style.Fg), rgba(s.Cell(7, 1).Style.Fg); a != b {
			t.Fatalf("2 columns away is %v, 1 row away is %v", a, b)
		}
	})

	t.Run("empty rect dims everything", func(t *testing.T) {
		t.Parallel()
		s := litFrame()
		Spotlight{Amount: 1}.Apply(s, 1)
		for y := range s.Height() {
			for x := range s.Width() {
				if rgba(s.Cell(x, y).Style.Fg) != black || rgba(s.Cell(x, y).Style.Bg) != black {
					t.Fatalf("cell %d,%d is %v on %v", x, y, s.Cell(x, y).Style.Fg, s.Cell(x, y).Style.Bg)
				}
			}
		}
	})
}

func BenchmarkSpotlightFullScreen(b *testing.B) {
	frame, _ := benchFrame()
	mixer := &canvas.Mixer{}
	e := Spotlight{Rect: image.Rect(60, 20, 120, 40), Soft: 8}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		// The frame is dimmed further each time, which costs the same.
		e.Apply(frame.Region(frame.Bounds(), env, mixer), float64(i%30)/30)
	}
}
