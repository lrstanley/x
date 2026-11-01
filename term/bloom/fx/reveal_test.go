// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/canvas"
)

// boxText returns the text of a w×h box with a border, white on blue, with a
// row number in each row of its middle.
func boxText(w, h int) string {
	var b strings.Builder
	b.WriteString("\x1b[38;2;255;255;255;48;2;0;0;255m")
	for y := range h {
		if y > 0 {
			b.WriteByte('\n')
		}
		switch y {
		case 0:
			b.WriteString("┌" + strings.Repeat("─", w-2) + "┐")
		case h - 1:
			b.WriteString("└" + strings.Repeat("─", w-2) + "┘")
		default:
			fmt.Fprintf(&b, "│%-*d│", w-2, y)
		}
	}
	b.WriteString("\x1b[m")
	return b.String()
}

// boxLayer returns a w×h box as a surface placed at 4,2 on a larger frame
// of red cells.
func boxLayer(w, h int) *canvas.Surface {
	return textLayerAt(boxText(w, h), image.Pt(4, 2), w+12, h+8)
}

func textLayerAt(text string, at image.Point, fw, fh int) *canvas.Surface {
	cells := canvas.Parse(text, ansi.GraphemeWidth)
	frame := canvas.New(fw, fh, ansi.GraphemeWidth)
	for y := range fh {
		for x := range fw {
			frame.CellAt(x, y).Content = "."
			frame.CellAt(x, y).Style.Bg = red
		}
	}
	rect := image.Rectangle{Min: at, Max: at.Add(image.Pt(cells.Width(), cells.Height()))}
	return cells.Layer(rect, frame.Bounds(), frame, env, &canvas.Mixer{})
}

// visibleRow returns the glyphs of row y that are shown, with '_' for those
// that are hidden.
func visibleRow(s *canvas.Surface, y int) string {
	var b strings.Builder
	for x := range s.Width() {
		switch c := s.Cell(x, y); {
		case c.Width == 0:
		case !s.Visible(x, y):
			b.WriteByte('_')
		default:
			b.WriteString(c.Content)
		}
	}
	return b.String()
}

// rowsShown returns how many cells of each row are shown.
func rowsShown(s *canvas.Surface) []int {
	n := make([]int, s.Height())
	for y := range n {
		for x := range s.Width() {
			if s.Visible(x, y) {
				n[y]++
			}
		}
	}
	return n
}

// snapshot returns the content and style of every cell of s.
func snapshot(s *canvas.Surface) []string {
	var out []string
	for y := range s.Height() {
		for x := range s.Width() {
			c := s.Cell(x, y)
			out = append(out, fmt.Sprintf("%q %d %v", c.Content, c.Width, c.Style))
		}
	}
	return out
}

var revealEffects = map[string]Effect{
	"unfold":       Unfold{},
	"iris":         Iris{},
	"iris at":      Iris{At: image.Pt(7, 4)},
	"shade":        Shade{Seed: 3},
	"blinds top":   Blinds{From: Top},
	"blinds right": Blinds{From: Right, Size: 2},
	"wave bottom":  Wave{From: Bottom},
	"wave left":    Wave{From: Left},
	"wave top":     Wave{From: Top},
	"wave right":   Wave{From: Right},
}

func TestReveals(t *testing.T) {
	t.Parallel()

	for name, e := range revealEffects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const w, h = 24, 9

			s := boxLayer(w, h)
			e.Apply(s, 0)
			if n := hidden(s); n != w*h {
				t.Fatalf("t=0 hides %d of %d cells", n, w*h)
			}

			s = boxLayer(w, h)
			e.Apply(s, 0.5)
			if n := hidden(s); n == 0 || n == w*h {
				t.Fatalf("t=0.5 hides %d of %d cells", n, w*h)
			}

			s = boxLayer(w, h)
			before := snapshot(s)
			e.Apply(s, 1)
			if n := hidden(s); n != 0 {
				t.Fatalf("t=1 hides %d cells", n)
			}
			if after := snapshot(s); strings.Join(after, "\n") != strings.Join(before, "\n") {
				t.Fatal("t=1 changed cells")
			}

			// The more progress, the more is shown.
			last := w*h + 1
			for i := range 41 {
				s = boxLayer(w, h)
				e.Apply(s, float64(i)/40)
				if n := hidden(s); n > last {
					t.Fatalf("t=%.3f hides %d cells, more than %d at the step before", float64(i)/40, n, last)
				} else {
					last = n
				}
			}
		})
	}
}

func TestRevealsKeepWideGlyphs(t *testing.T) {
	t.Parallel()

	text := "\x1b[44mab世界cd\x1b[m\n\x1b[44m世界世界世界\x1b[m\n\x1b[44mx世y世z世\x1b[m"
	for name, e := range revealEffects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for i := range 21 {
				at := float64(i) / 20
				s := textLayerAt(text, image.Pt(3, 1), 20, 8)
				before := snapshot(s)
				e.Apply(s, at)
				for y := range s.Height() {
					assertRowWhole(t, s, y)
				}
				if _, unfold := e.(Unfold); unfold {
					continue // moves whole rows, which may have glyphs elsewhere
				}
				for j, c := range snapshot(s) {
					// Content and style may change, but never the width.
					if got, want := strings.Fields(c)[1], strings.Fields(before[j])[1]; got != want {
						t.Fatalf("t=%.2f cell %d has width %s, want %s", at, j, got, want)
					}
				}
			}
		})
	}
}

// assertRowWhole fails if row y of s holds part of a wide glyph.
func assertRowWhole(t *testing.T, s *canvas.Surface, y int) {
	t.Helper()
	for x := 0; x < s.Width(); {
		w := s.Cell(x, y).Width
		if w == 0 {
			t.Fatalf("row %d: tail of a wide glyph without its lead at %d", y, x)
		}
		for i := 1; i < w; i++ {
			if x+i >= s.Width() || s.Cell(x+i, y).Width != 0 {
				t.Fatalf("row %d: wide glyph at %d is missing its tail", y, x)
			}
		}
		x += w
	}
}

func TestUnfold(t *testing.T) {
	t.Parallel()

	const w, h = 6, 9
	tests := []struct {
		t    float64
		rows []int // the rows that are shown
	}{
		{0.01, []int{4}},
		{0.25, []int{3, 4, 5}},
		{0.5, []int{2, 3, 4, 5, 6}},
		{0.99, []int{0, 1, 2, 3, 4, 5, 6, 7}},
	}
	for _, tt := range tests {
		s := boxLayer(w, h)
		Unfold{}.Apply(s, tt.t)
		var got []int
		for y, n := range rowsShown(s) {
			switch n {
			case 0:
			case w:
				got = append(got, y)
			default:
				t.Fatalf("t=%.2f: row %d is partly shown", tt.t, y)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.rows) {
			t.Fatalf("t=%.2f shows rows %v, want %v", tt.t, got, tt.rows)
		}
		if len(got) > 1 {
			// The borders are intact from the start, the middle is the
			// node's middle.
			if top, bottom := visibleRow(s, got[0]), visibleRow(s, got[len(got)-1]); top != "┌────┐" || bottom != "└────┘" {
				t.Fatalf("t=%.2f: band is %q ... %q", tt.t, top, bottom)
			}
			for _, y := range got[1 : len(got)-1] {
				if want := fmt.Sprintf("│%-4d│", y); visibleRow(s, y) != want {
					t.Fatalf("t=%.2f: row %d is %q, want %q", tt.t, y, visibleRow(s, y), want)
				}
			}
		}
	}
}

func TestIris(t *testing.T) {
	t.Parallel()

	t.Run("round", func(t *testing.T) {
		t.Parallel()
		s := boxLayer(41, 21)
		Iris{}.Apply(s, 0.5)
		across, down := rowsShown(s)[10], 0
		for _, n := range rowsShown(s) {
			if n > 0 {
				down++
			}
		}
		// Cells are twice as tall as wide, so the circle is twice as wide
		// as it is tall, in cells.
		if across < 2*down-3 || across > 2*down+3 {
			t.Fatalf("%d cells across, %d rows down", across, down)
		}
	})

	t.Run("at", func(t *testing.T) {
		t.Parallel()
		// The button, in frame coordinates: a cell near the node's top
		// right corner, which is at 4,2.
		at := image.Pt(4+18, 2+1)
		s := boxLayer(20, 9)
		Iris{At: at}.Apply(s, 0.15)
		if !s.Visible(18, 1) || s.Visible(0, 8) || s.Visible(0, 0) {
			t.Fatalf("the circle is not at %v", at)
		}
		s = boxLayer(20, 9)
		Iris{At: at}.Apply(s, 0.99)
		if !s.Visible(0, 8) {
			t.Fatal("the farthest corner is not shown by t=0.99")
		}
	})

	t.Run("center", func(t *testing.T) {
		t.Parallel()
		s := boxLayer(21, 9)
		Iris{}.Apply(s, 0.1)
		if !s.Visible(10, 4) || s.Visible(0, 0) || s.Visible(20, 8) {
			t.Fatal("the circle is not at the center")
		}
	})
}

func TestShade(t *testing.T) {
	t.Parallel()

	s := boxLayer(24, 9)
	Shade{Seed: 1}.Apply(s, 0.5)
	seen := map[string]int{}
	for y := range s.Height() {
		for x := range s.Width() {
			c := s.Cell(x, y)
			if !s.Visible(x, y) {
				continue
			}
			if strings.ContainsAny(c.Content, "░▒▓") {
				seen[c.Content]++
				if rgba(c.Style.Bg) != red || rgba(c.Style.Fg) != rgba(blue) {
					t.Fatalf("shade at %d,%d is %v on %v, want the node's blue over the frame's red", x, y, c.Style.Fg, c.Style.Bg)
				}
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("shades at t=0.5: %v, want all three", seen)
	}

	same := boxLayer(24, 9)
	Shade{Seed: 1}.Apply(same, 0.5)
	other := boxLayer(24, 9)
	Shade{Seed: 2}.Apply(other, 0.5)
	if strings.Join(snapshot(s), "") != strings.Join(snapshot(same), "") || hidden(s) != hidden(same) {
		t.Fatal("the same seed gives different results")
	}
	if hidden(s) == hidden(other) && strings.Join(snapshot(s), "") == strings.Join(snapshot(other), "") {
		t.Fatal("different seeds give the same result")
	}

	// Wide glyphs and blanks without a background only appear whole.
	for i := range 20 {
		wide := textLayerAt("世界x\n   ", image.Pt(0, 0), 8, 4)
		Shade{Seed: 4}.Apply(wide, float64(i)/20)
		for _, p := range []image.Point{{0, 0}, {2, 0}, {0, 1}, {1, 1}} {
			if c := wide.Cell(p.X, p.Y); wide.Visible(p.X, p.Y) && strings.ContainsAny(c.Content, "░▒▓") {
				t.Fatalf("t=%.2f: %v is shaded as %q", float64(i)/20, p, c.Content)
			}
		}
	}
}

func TestBlinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		e    Blinds
		t    float64
		rows string // the rows of a 7×7 node that are shown, as 0/1, or columns
		cols string
	}{
		{"top", Blinds{From: Top, Size: 3}, 0.5, "1001001", "1111111"},
		{"bottom", Blinds{From: Bottom, Size: 3}, 0.5, "1001001", "1111111"},
		{"bottom, two thirds", Blinds{From: Bottom, Size: 3}, 0.7, "1011011", "1111111"},
		{"default size", Blinds{From: Top}, 0.5, "1001001", "1111111"},
		{"left", Blinds{From: Left, Size: 2}, 0.5, "1111111", "1010101"},
		{"right", Blinds{From: Right, Size: 2}, 0.5, "1111111", "1010101"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := boxLayer(7, 7)
			tt.e.Apply(s, tt.t)
			var rows, cols strings.Builder
			for y := range 7 {
				rows.WriteByte(map[bool]byte{true: '1', false: '0'}[s.Visible(4, y)])
			}
			for x := range 7 {
				cols.WriteByte(map[bool]byte{true: '1', false: '0'}[s.Visible(x, 3)])
			}
			if rows.String() != tt.rows || cols.String() != tt.cols {
				t.Fatalf("rows %s cols %s, want %s %s", rows.String(), cols.String(), tt.rows, tt.cols)
			}
		})
	}
}

func TestWave(t *testing.T) {
	t.Parallel()

	for _, from := range []Edge{Bottom, Top, Left, Right} {
		s := boxLayer(40, 12)
		Wave{From: from}.Apply(s, 0.5)

		// The front comes from the edge: cells at the start are shown, those
		// at the far side are not.
		near, far := image.Pt(20, 11), image.Pt(20, 0)
		switch from {
		case Bottom:
		case Top:
			near, far = far, near
		case Left:
			near, far = image.Pt(0, 6), image.Pt(39, 6)
		case Right:
			near, far = image.Pt(39, 6), image.Pt(0, 6)
		}
		if !s.Visible(near.X, near.Y) || s.Visible(far.X, far.Y) {
			t.Fatalf("from %d: near visible=%v, far visible=%v", from, s.Visible(near.X, near.Y), s.Visible(far.X, far.Y))
		}

		// A crest of brightened cells trails the front.
		crest, plain := 0, 0
		for y := range 12 {
			for x := range 40 {
				if !s.Visible(x, y) {
					continue
				}
				if c := s.Cell(x, y); rgba(c.Style.Bg) != rgba(blue) {
					crest++
					if rgba(c.Style.Fg).G < 0xff && rgba(c.Style.Fg) != rgba(white) {
						t.Fatalf("crest cell %d,%d has fg %v, not moved toward the foreground", x, y, c.Style.Fg)
					}
				} else {
					plain++
				}
			}
		}
		if crest == 0 || plain == 0 {
			t.Fatalf("from %d: %d crest and %d plain cells at t=0.5", from, crest, plain)
		}
	}
}

func TestWaveSettles(t *testing.T) {
	t.Parallel()

	s := boxLayer(40, 12)
	Wave{From: Left}.Apply(s, 0.99)
	if n := hidden(s); n != 0 {
		t.Fatalf("t=0.99 hides %d cells", n)
	}
	for y := range 12 {
		for x := range 40 {
			if c := s.Cell(x, y); rgba(c.Style.Bg) != rgba(blue) {
				t.Fatalf("cell %d,%d still has background %v at t=0.99", x, y, c.Style.Bg)
			}
		}
	}
}

// benchGrid returns a 200×60 grid of colored text and a function that puts
// its cells back as they were, since effects change cells in place.
func benchGrid() (cells *canvas.Canvas, reset func()) {
	var sb strings.Builder
	for y := range 60 {
		for x := range 20 {
			fmt.Fprintf(&sb, "\x1b[38;5;%d;48;5;17m0123456789\x1b[m", []int{33, 160, 70, 220}[(x+y)%4])
		}
		if y < 59 {
			sb.WriteByte('\n')
		}
	}
	cells = canvas.Parse(sb.String(), ansi.GraphemeWidth)
	pristine := canvas.Parse(sb.String(), ansi.GraphemeWidth)
	return cells, func() {
		for y := range cells.Height() {
			for x := range cells.Width() {
				*cells.CellAt(x, y) = *pristine.CellAt(x, y)
			}
		}
	}
}

// benchReveal measures e on a 200×60 node, including making the surface and
// putting the cells back, which BenchmarkRevealReset measures alone.
func benchReveal(b *testing.B, e Effect) {
	b.Helper()
	cells, reset := benchGrid()
	under := canvas.New(200, 60, ansi.GraphemeWidth)
	mixer := &canvas.Mixer{}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		reset()
		e.Apply(cells.Layer(cells.Bounds(), cells.Bounds(), under, env, mixer), float64(i%30)/30)
	}
}

func BenchmarkRevealReset(b *testing.B) {
	_, reset := benchGrid()
	b.ReportAllocs()
	for b.Loop() {
		reset()
	}
}

func BenchmarkUnfoldFullScreen(b *testing.B) { benchReveal(b, Unfold{}) }
func BenchmarkIrisFullScreen(b *testing.B)   { benchReveal(b, Iris{}) }
func BenchmarkShadeFullScreen(b *testing.B)  { benchReveal(b, Shade{Seed: 1}) }
func BenchmarkBlindsFullScreen(b *testing.B) { benchReveal(b, Blinds{From: Top}) }
func BenchmarkWaveFullScreen(b *testing.B)   { benchReveal(b, Wave{From: Left}) }
func BenchmarkWaveTopFullScreen(b *testing.B) {
	benchReveal(b, Wave{From: Top})
}

func TestUnfoldInColumnsKeepsGlyphsWhole(t *testing.T) {
	t.Parallel()

	for _, at := range []float64{0.3, 0.6, 0.9} {
		s := textLayer("世a\nbcd\nef世")
		Stagger{Effect: Unfold{}, From: Left}.Apply(s, at)
		for y := range s.Height() {
			for x := 0; x < s.Width(); {
				w := s.Cell(x, y).Width
				if w == 0 || (w == 2 && (x+1 >= s.Width() || s.Cell(x+1, y).Width != 0)) {
					t.Fatalf("t=%v: glyph at %d,%d split", at, x, y)
				}
				x += w
			}
		}
	}
}
