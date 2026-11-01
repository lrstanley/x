// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/canvas"
)

var (
	red   = color.RGBA{R: 0xff, A: 0xff}
	blue  = color.RGBA{B: 0xff, A: 0xff}
	black = color.RGBA{A: 0xff}
	white = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	env   = canvas.Env{Fg: white, Bg: black}
)

// layer returns a 4×2 surface of red-on-blue text over an 8×4 frame with a
// white background, placed at 2,1.
func layer() *canvas.Surface {
	frame := canvas.Parse(strings.Repeat("\x1b[48;2;255;255;255m........\x1b[m\n", 3)+"\x1b[48;2;255;255;255m........\x1b[m", ansi.GraphemeWidth)
	cells := canvas.Parse("\x1b[38;2;255;0;0;48;2;0;0;255mabcd\nefgh\x1b[m", ansi.GraphemeWidth)
	return cells.Layer(image.Rect(2, 1, 6, 3), frame.Bounds(), frame, env, &canvas.Mixer{})
}

func hidden(s *canvas.Surface) int {
	n := 0
	for y := range s.Height() {
		for x := range s.Width() {
			if !s.Visible(x, y) {
				n++
			}
		}
	}
	return n
}

func rgba(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func TestRevealEffects(t *testing.T) {
	t.Parallel()

	effects := map[string]Effect{
		"wipe":       Wipe{From: Left},
		"dissolve":   Dissolve{Seed: 7},
		"typewriter": Typewriter{},
	}
	for name, e := range effects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := layer()
			e.Apply(s, 0)
			if n := hidden(s); n != 8 {
				t.Fatalf("t=0 hides %d of 8 cells", n)
			}
			s = layer()
			e.Apply(s, 0.5)
			if n := hidden(s); n == 0 || n == 8 {
				t.Fatalf("t=0.5 hides %d of 8 cells", n)
			}
			s = layer()
			e.Apply(s, 1)
			if n := hidden(s); n != 0 {
				t.Fatalf("t=1 hides %d cells", n)
			}
		})
	}
}

func TestSlide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from Edge
		want image.Point
	}{
		{Bottom, image.Pt(0, 3)}, // from frame bottom (4) to the layer's top (1)
		{Top, image.Pt(0, -3)},
		{Left, image.Pt(-6, 0)},
		{Right, image.Pt(6, 0)},
	}
	for _, tt := range tests {
		s := layer()
		Slide{From: tt.from}.Apply(s, 0)
		if s.Offset != tt.want {
			t.Errorf("Slide from %d at t=0: offset %v, want %v", tt.from, s.Offset, tt.want)
		}
		s = layer()
		Slide{From: tt.from}.Apply(s, 1)
		if s.Offset != (image.Point{}) {
			t.Errorf("Slide from %d at t=1: offset %v", tt.from, s.Offset)
		}
	}
}

func TestFade(t *testing.T) {
	t.Parallel()

	s := layer()
	Fade{}.Apply(s, 0)
	if c := s.Cell(0, 0); c.Content != "." || rgba(c.Style.Fg) != white || rgba(c.Style.Bg) != white {
		t.Fatalf("t=0 shows %q in %v on %v, want the cell beneath", c.Content, c.Style.Fg, c.Style.Bg)
	}
	s = layer()
	Fade{}.Apply(s, 0.75)
	if c := s.Cell(0, 0); c.Content != "a" {
		t.Fatalf("t=0.75 shows %q, want the node's own glyph", c.Content)
	}
	s = layer()
	Fade{}.Apply(s, 1)
	if c := s.Cell(0, 0); rgba(c.Style.Fg) != red || rgba(c.Style.Bg) != blue {
		t.Fatalf("t=1 changed colors to %v on %v", c.Style.Fg, c.Style.Bg)
	}
}

func TestScramble(t *testing.T) {
	t.Parallel()

	s := layer()
	Scramble{Seed: 1}.Apply(s, 0)
	changed := 0
	for x, want := range "abcd" {
		c := s.Cell(x, 0)
		if c.Width != 1 {
			t.Fatalf("width changed to %d", c.Width)
		}
		if c.Content != string(want) {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("t=0 scrambled nothing")
	}
}

func TestBackdrops(t *testing.T) {
	t.Parallel()

	region := func() *canvas.Surface {
		f := canvas.Parse("\x1b[38;2;255;0;0;48;2;0;0;255mab\x1b[mcd", ansi.GraphemeWidth)
		return f.Region(f.Bounds(), env, &canvas.Mixer{})
	}

	t.Run("recolor", func(t *testing.T) {
		t.Parallel()
		s := region()
		Recolor{Fg: white, Bg: black}.Apply(s, 1)
		for x := range 4 {
			if c := s.Cell(x, 0); rgba(c.Style.Fg) != white || rgba(c.Style.Bg) != black {
				t.Fatalf("cell %d is %v on %v", x, c.Style.Fg, c.Style.Bg)
			}
		}
	})

	t.Run("dim keeps unset backgrounds", func(t *testing.T) {
		t.Parallel()
		s := region()
		Dim{Amount: 0.5}.Apply(s, 1)
		if c := s.Cell(2, 0); c.Style.Bg != nil {
			t.Fatalf("unset background became %v", c.Style.Bg)
		}
		if c := s.Cell(0, 0); rgba(c.Style.Fg).R == 0xff || rgba(c.Style.Bg).B == 0xff {
			t.Fatalf("colors not dimmed: %v on %v", c.Style.Fg, c.Style.Bg)
		}
	})

	t.Run("strength follows t", func(t *testing.T) {
		t.Parallel()
		s := region()
		Dim{Amount: 1}.Apply(s, 0)
		Desaturate{Amount: 1}.Apply(s, 0)
		Tint{Color: white, Amount: 1}.Apply(s, 0)
		Recolor{Fg: white}.Apply(s, 0)
		if c := s.Cell(0, 0); rgba(c.Style.Fg) != red {
			t.Fatalf("t=0 changed the color to %v", c.Style.Fg)
		}
	})

	t.Run("desaturate", func(t *testing.T) {
		t.Parallel()
		s := region()
		Desaturate{Amount: 1}.Apply(s, 1)
		if c := rgba(s.Cell(0, 0).Style.Fg); c.R != c.G || c.G != c.B {
			t.Fatalf("not gray: %v", c)
		}
	})
}

func BenchmarkDimFullScreen(b *testing.B) {
	var sb strings.Builder
	for y := range 60 {
		for x := range 20 {
			sb.WriteString("\x1b[38;5;")
			sb.WriteString([]string{"33", "160", "70", "220"}[(x+y)%4])
			sb.WriteString("m0123456789\x1b[m")
		}
		if y < 59 {
			sb.WriteByte('\n')
		}
	}
	frame := canvas.Parse(sb.String(), ansi.GraphemeWidth)
	mixer := &canvas.Mixer{}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		Dim{Amount: 0.6}.Apply(frame.Region(frame.Bounds(), env, mixer), float64(i%30)/30)
	}
}
