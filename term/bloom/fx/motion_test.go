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

// textLayer returns a surface over cells parsed from text, placed at the
// frame's origin over a blank frame of the same size.
func textLayer(text string) *canvas.Surface {
	cells := canvas.Parse(text, ansi.GraphemeWidth)
	frame := canvas.New(cells.Width(), cells.Height(), ansi.GraphemeWidth)
	return cells.Layer(cells.Bounds(), cells.Bounds(), frame, env, &canvas.Mixer{})
}

// textRegion returns a region over all of cells parsed from text.
func textRegion(text string) *canvas.Surface {
	cells := canvas.Parse(text, ansi.GraphemeWidth)
	return cells.Region(cells.Bounds(), env, &canvas.Mixer{})
}

// rowText returns what row y of s shows, with hidden cells as a "~".
func rowText(s *canvas.Surface, y int) string {
	var b strings.Builder
	for x := range s.Width() {
		c := s.Cell(x, y)
		switch {
		case c.Width == 0:
		case !s.Visible(x, y):
			b.WriteByte('~')
		default:
			b.WriteString(c.Content)
		}
	}
	return b.String()
}

// rows returns every row of s, as with [rowText].
func rows(s *canvas.Surface) string {
	out := make([]string, s.Height())
	for y := range out {
		out[y] = rowText(s, y)
	}
	return strings.Join(out, "\n")
}

// block returns h rows of w letters, a pattern with no blanks.
func block(w, h int) string {
	rows := make([]string, h)
	for y := range rows {
		rows[y] = strings.Repeat("abcdefghij", w/10+1)[:w]
	}
	return strings.Join(rows, "\n")
}

func TestDecode(t *testing.T) {
	t.Parallel()

	text := block(20, 8)
	apply := func(seed uint32, at float64) string {
		s := textLayer(text)
		Decode{Seed: seed}.Apply(s, at)
		return rows(s)
	}

	if got := apply(1, 1); got != text {
		t.Fatalf("t=1 changed the node:\n%s", got)
	}
	if again := apply(1, 0.4); apply(1, 0.4) != again {
		t.Fatal("same seed, different result")
	}
	if apply(1, 0.4) == apply(2, 0.4) {
		t.Fatal("different seeds, same result")
	}

	// Each column has at most one leading glyph and nothing but its own
	// content above it, and a revealed cell stays revealed.
	s := textLayer(text)
	Decode{Seed: 3}.Apply(s, 0.5)
	var hiddenCells, heads int
	for x := range s.Width() {
		seenHidden := false
		for y := range s.Height() {
			c := s.Cell(x, y)
			switch {
			case !s.Visible(x, y):
				hiddenCells++
				seenHidden = true
			case seenHidden:
				t.Fatalf("column %d shows a cell below a hidden one", x)
			case c.Content != string(text[x]):
				heads++
			}
		}
	}
	if hiddenCells == 0 || heads == 0 {
		t.Fatalf("t=0.5 hid %d cells and has %d leading glyphs", hiddenCells, heads)
	}
	if heads > s.Width() {
		t.Fatalf("%d leading glyphs in %d columns", heads, s.Width())
	}

	// Once the whole of a cell is shown it stays as it is.
	early, late := textLayer(text), textLayer(text)
	Decode{Seed: 3}.Apply(early, 0.5)
	Decode{Seed: 3}.Apply(late, 0.8)
	for y := range early.Height() {
		for x := range early.Width() {
			if early.Visible(x, y) && early.Cell(x, y).Content == string(text[x]) &&
				(!late.Visible(x, y) || late.Cell(x, y).Content != string(text[x])) {
				t.Fatalf("cell %d,%d shown at 0.5 but not at 0.8", x, y)
			}
		}
	}

	// Wide glyphs and blanks are not replaced.
	wide := textLayer("漢字 ab")
	Decode{}.Apply(wide, 0.01)
	for x := range wide.Width() {
		if c := wide.Cell(x, 0); c.Width == 2 && c.Content != "漢" && c.Content != "字" {
			t.Fatalf("wide glyph replaced by %q", c.Content)
		}
	}
}

func TestGlitch(t *testing.T) {
	t.Parallel()

	text := block(40, 20)
	apply := func(seed uint32, at float64) string {
		s := textLayer(text)
		Glitch{Seed: seed}.Apply(s, at)
		return rows(s)
	}

	if got := apply(1, 1); got != text {
		t.Fatalf("t=1 changed the node:\n%s", got)
	}
	if again := apply(1, 0.1); apply(1, 0.1) != again {
		t.Fatal("same seed, different result")
	}
	if apply(1, 0.1) == apply(2, 0.1) {
		t.Fatal("different seeds, same result")
	}
	if apply(1, 0.1) == text {
		t.Fatal("t=0.1 changed nothing")
	}
	// It changes in steps, not on every frame.
	if apply(1, 0.30) != apply(1, 0.31) {
		t.Fatal("pattern changed within a step")
	}
	if apply(1, 0.30) == apply(1, 0.40) {
		t.Fatal("pattern did not change between steps")
	}

	t.Run("shifted rows are split in color", func(t *testing.T) {
		t.Parallel()
		s := textLayer(text)
		Glitch{Seed: 1}.Apply(s, 0)
		recolored := 0
		for y := range s.Height() {
			for x := range s.Width() {
				if c := s.Cell(x, y); s.Visible(x, y) && c.Style.Fg != nil {
					recolored++
				}
			}
		}
		if recolored == 0 {
			t.Fatal("no colors were split")
		}
	})

	t.Run("wide glyphs stay whole", func(t *testing.T) {
		t.Parallel()
		wide := strings.Repeat("漢字漢字ab\n", 20)
		for step := range 16 {
			s := textLayer(strings.TrimSuffix(wide, "\n"))
			Glitch{Seed: 4}.Apply(s, float64(step)/16)
			for y := range s.Height() {
				for x := 0; x < s.Width(); {
					w := s.Cell(x, y).Width
					if w == 0 || (w == 2 && (x+1 >= s.Width() || s.Cell(x+1, y).Width != 0)) {
						t.Fatalf("step %d: wide glyph at %d,%d split", step, x, y)
					}
					x += w
				}
			}
		}
	})
}

func TestShake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		e    Shake
		t    float64
		want int
	}{
		{Shake{}, 0, 0},
		{Shake{}, 1, 0},
		{Shake{}, 0.125, 0},     // a swing crosses zero
		{Shake{}, 1.0 / 16, 2},  // 2·sin(π/2)·(1−1/16) = 1.875
		{Shake{}, 3.0 / 16, -2}, // 2·(−1)·(1−3/16) = −1.625
		{Shake{Amount: 10}, 1.0 / 16, 9},
		{Shake{Amount: -10}, 1.0 / 16, -9},
		{Shake{Amount: 10, Times: 1}, 0.25, 8},
		{Shake{Amount: 10, Times: 1}, 0.75, -3},
		{Shake{Times: 4}, 1.0 / 16, 2},
	}
	for _, tt := range tests {
		s := textLayer("ab")
		tt.e.Apply(s, tt.t)
		if s.Offset != image.Pt(tt.want, 0) {
			t.Errorf("%+v at t=%v: offset %v, want %d", tt.e, tt.t, s.Offset, tt.want)
		}
	}

	s := textLayer("ab")
	s.Offset = image.Pt(1, 1)
	Shake{}.Apply(s, 1.0/16)
	if s.Offset != image.Pt(3, 1) {
		t.Errorf("did not add to the existing offset: %v", s.Offset)
	}
}

func TestShimmer(t *testing.T) {
	t.Parallel()

	text := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 40)+"\n", 6), "\n")
	count := func(e Shimmer, at float64) (changed int, s *canvas.Surface) {
		s = textLayer(text)
		e.Apply(s, at)
		for y := range s.Height() {
			for x := range s.Width() {
				if c := s.Cell(x, y); c.Style.Fg != nil {
					changed++
				}
			}
		}
		return changed, s
	}

	for _, at := range []float64{0, 1} {
		if n, _ := count(Shimmer{}, at); n != 0 {
			t.Errorf("t=%v changed %d cells, want the band off the node", at, n)
		}
	}
	n, s := count(Shimmer{}, 0.5)
	if n == 0 || n == 40*6 {
		t.Fatalf("t=0.5 changed %d cells", n)
	}
	// The band is diagonal: it is further right on higher rows.
	first := func(y int) int {
		for x := range s.Width() {
			if s.Cell(x, y).Style.Fg != nil {
				return x
			}
		}
		return -1
	}
	if a, b := first(0), first(5); a < 0 || b < 0 || a <= b {
		t.Fatalf("band starts at column %d on the top row and %d on the bottom, want it leaning right", a, b)
	}

	// The middle of the band is mixed toward the color, not past it, and the
	// terminal foreground is the default.
	mid, _ := count(Shimmer{Color: red, Width: 20}, 0.5)
	if narrow, _ := count(Shimmer{Color: red, Width: 2}, 0.5); narrow >= mid {
		t.Fatalf("width 2 changed %d cells, width 20 %d", narrow, mid)
	}
	_, s = count(Shimmer{Color: red, Width: 6}, 0.5)
	found := false
	for x := range s.Width() {
		if c := s.Cell(x, 3); c.Style.Fg != nil && rgba(c.Style.Fg).G < rgba(env.Fg).G {
			found = true
		}
	}
	if !found {
		t.Fatal("no cell moved toward red")
	}
	_, s = count(Shimmer{}, 0.5)
	for x := range s.Width() {
		if c := s.Cell(x, 3); c.Style.Fg != nil && rgba(c.Style.Fg) != rgba(env.Fg) {
			t.Fatalf("default color mixed %v, want the terminal foreground only", c.Style.Fg)
		}
	}
}

// benchFrame returns a 200×60 frame of colored text, and a function that
// returns it to how it started, since effects change cells in place.
func benchFrame() (frame *canvas.Canvas, reset func()) {
	var sb strings.Builder
	for y := range 60 {
		for x := range 20 {
			fmt.Fprintf(&sb, "\x1b[38;5;%dm0123456789\x1b[m", []int{33, 160, 70, 220}[(x+y)%4])
		}
		if y < 59 {
			sb.WriteByte('\n')
		}
	}
	frame = canvas.Parse(sb.String(), ansi.GraphemeWidth)
	pristine := canvas.Parse(sb.String(), ansi.GraphemeWidth)
	return frame, func() {
		for y := range frame.Height() {
			for x := range frame.Width() {
				frame.SetCell(x, y, pristine.CellAt(x, y))
			}
		}
	}
}

// benchmarkLayer measures e on a 200×60 node, including putting the cells
// back, which BenchmarkReset measures alone.
func benchmarkLayer(b *testing.B, e Effect) {
	b.Helper()
	cells, reset := benchFrame()
	under := canvas.New(200, 60, ansi.GraphemeWidth)
	mixer := &canvas.Mixer{}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		reset()
		e.Apply(cells.Layer(cells.Bounds(), cells.Bounds(), under, env, mixer), float64(i%30)/30)
	}
}

func BenchmarkReset(b *testing.B) {
	_, reset := benchFrame()
	b.ReportAllocs()
	for b.Loop() {
		reset()
	}
}

func BenchmarkDecodeFullScreen(b *testing.B)  { benchmarkLayer(b, Decode{Seed: 1}) }
func BenchmarkGlitchFullScreen(b *testing.B)  { benchmarkLayer(b, Glitch{Seed: 1}) }
func BenchmarkShakeFullScreen(b *testing.B)   { benchmarkLayer(b, Shake{}) }
func BenchmarkShimmerFullScreen(b *testing.B) { benchmarkLayer(b, Shimmer{Color: red}) }
