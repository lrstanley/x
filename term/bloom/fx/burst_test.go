// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/canvas"
)

// particles counts the cells of s that show a burst glyph, in a surface that
// started out blank.
func particles(s *canvas.Surface) int {
	n := 0
	for y := range s.Height() {
		for x := range s.Width() {
			if c := s.Cell(x, y); slices.Contains(burstGlyphs[:], c.Content) {
				n++
			}
		}
	}
	return n
}

func blankText(w, h int) string {
	return strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", w)+"\n", h), "\n")
}

func TestBurst(t *testing.T) {
	t.Parallel()

	at := image.Pt(40, 20)
	apply := func(e Burst, at float64) *canvas.Surface {
		s := textRegion(blankText(80, 40))
		e.Apply(s, at)
		return s
	}

	t.Run("draws while it is young", func(t *testing.T) {
		t.Parallel()
		for _, age := range []float64{0, 0.1, 0.5, 0.9} {
			if n := particles(apply(Burst{At: at}, age)); n == 0 {
				t.Errorf("t=%v drew nothing", age)
			}
		}
		for _, age := range []float64{1, 1.5, -0.1} {
			if n := particles(apply(Burst{At: at}, age)); n != 0 {
				t.Errorf("t=%v drew %d particles", age, n)
			}
		}
	})

	t.Run("starts at a point and spreads", func(t *testing.T) {
		t.Parallel()
		s := apply(Burst{At: at, Count: 20}, 0)
		if s.Cell(40, 20).Content == " " {
			t.Fatal("t=0 left the origin blank")
		}
		if n := particles(s); n != 1 {
			t.Fatalf("t=0 drew %d cells, want all particles on one", n)
		}
		if n := particles(apply(Burst{At: at, Count: 20}, 0.4)); n < 10 {
			t.Fatalf("t=0.4 drew %d cells for 20 particles", n)
		}
	})

	t.Run("count", func(t *testing.T) {
		t.Parallel()
		few := particles(apply(Burst{At: at, Count: 5}, 0.3))
		def := particles(apply(Burst{At: at}, 0.3))
		many := particles(apply(Burst{At: at, Count: 200}, 0.3))
		if few > 5 || def <= few || many <= def {
			t.Fatalf("5, default, and 200 particles drew %d, %d, %d cells", few, def, many)
		}
		if def != particles(apply(Burst{At: at, Count: 40}, 0.3)) {
			t.Fatal("zero count is not 40")
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		a, b := apply(Burst{At: at, Seed: 1}, 0.4), apply(Burst{At: at, Seed: 1}, 0.4)
		if rows(a) != rows(b) {
			t.Fatal("same seed, different bursts")
		}
		if rows(a) == rows(apply(Burst{At: at, Seed: 2}, 0.4)) {
			t.Fatal("different seeds, same burst")
		}
	})

	t.Run("fades", func(t *testing.T) {
		t.Parallel()
		s := apply(Burst{At: at}, 0.95)
		for y := range s.Height() {
			for x := range s.Width() {
				if c := s.Cell(x, y); c.Content == burstFaded && rgba(c.Style.Fg) == rgba(burstPalette[0]) {
					t.Fatalf("particle at %d,%d has not faded", x, y)
				}
			}
		}
		if particles(s) == 0 {
			t.Fatal("t=0.95 drew nothing")
		}
	})

	t.Run("keeps wide glyphs and is relative to the surface", func(t *testing.T) {
		t.Parallel()
		wide := strings.TrimSuffix(strings.Repeat(strings.Repeat("漢", 40)+"\n", 40), "\n")
		s := textRegion(wide)
		Burst{At: image.Pt(40, 20)}.Apply(s, 0.3)
		if rows(s) != wide {
			t.Fatal("a particle replaced half of a wide glyph")
		}

		// A node at 10,5 of the frame, with the burst starting inside it.
		cells := canvas.Parse(blankText(30, 15), ansi.GraphemeWidth)
		frame := canvas.New(80, 40, ansi.GraphemeWidth)
		n := cells.Layer(image.Rect(10, 5, 40, 20), frame.Bounds(), frame, env, &canvas.Mixer{})
		Burst{At: image.Pt(20, 12), Count: 1}.Apply(n, 0)
		if n.Cell(10, 7).Content == " " {
			t.Fatal("the origin was not translated into the node's cells")
		}
		if particles(n) != 1 {
			t.Fatalf("drew %d particles", particles(n))
		}
	})
}

func BenchmarkBurstFullScreen(b *testing.B) {
	frame, _ := benchFrame()
	mixer := &canvas.Mixer{}
	e := Burst{At: image.Pt(100, 40), Count: 120}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		e.Apply(frame.Region(frame.Bounds(), env, mixer), float64(i%30)/30)
	}
}
