// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// gridText returns w×h cells where row y is made of the letter 'a'+y.
func gridText(w, h int) string {
	rows := make([]string, h)
	for y := range rows {
		rows[y] = strings.Repeat(string(rune('a'+y)), w)
	}
	return strings.Join(rows, "\n")
}

func TestStagger(t *testing.T) {
	t.Parallel()

	t.Run("wipe by rows", func(t *testing.T) {
		t.Parallel()
		// Part i of 4 starts at i/6 and runs for 0.5: at t=0.5 the first
		// row is done and the last has not started.
		s := textLayerAt(gridText(12, 4), image.Pt(2, 2), 20, 10)
		Stagger{Effect: Wipe{From: Left}, From: Top}.Apply(s, 0.5)
		got := rowsShown(s)
		if got[0] != 12 || got[3] != 0 || got[1] <= got[2] || got[2] <= 0 {
			t.Fatalf("shown per row = %v, want 12 > a > b > 0", got)
		}

		s = textLayerAt(gridText(12, 4), image.Pt(2, 2), 20, 10)
		Stagger{Effect: Wipe{From: Left}, From: Bottom}.Apply(s, 0.5)
		got = rowsShown(s)
		if got[3] != 12 || got[0] != 0 || got[2] <= got[1] {
			t.Fatalf("from the bottom, shown per row = %v", got)
		}
	})

	t.Run("wipe by columns", func(t *testing.T) {
		t.Parallel()
		for _, from := range []Edge{Left, Right} {
			s := textLayerAt(gridText(8, 6), image.Pt(2, 2), 20, 10)
			Stagger{Effect: Wipe{From: Top}, From: from, Spread: 0.8}.Apply(s, 0.5)
			var cols [8]int
			for x := range cols {
				for y := range 6 {
					if s.Visible(x, y) {
						cols[x]++
					}
				}
			}
			first, last := cols[0], cols[7]
			if from == Right {
				first, last = last, first
			}
			if first <= last || first == 0 || last != 0 {
				t.Fatalf("from %d: shown per column = %v", from, cols)
			}
		}
	})

	t.Run("spread", func(t *testing.T) {
		t.Parallel()
		// With the whole of t spent on starting parts, each one is a step.
		s := textLayerAt(gridText(10, 5), image.Pt(2, 2), 20, 10)
		Stagger{Effect: Wipe{From: Left}, From: Top, Spread: 1}.Apply(s, 0.5)
		for y, n := range rowsShown(s) {
			if n != 0 && n != 10 {
				t.Fatalf("row %d is %d wide, want a step", y, n)
			}
		}
		if got := rowsShown(s); got[0] != 10 || got[1] != 10 || got[2] != 10 || got[3] != 0 || got[4] != 0 {
			t.Fatalf("shown per row = %v", got)
		}
	})

	t.Run("one part", func(t *testing.T) {
		t.Parallel()
		s := textLayerAt(gridText(10, 1), image.Pt(2, 2), 20, 10)
		Stagger{Effect: Wipe{From: Left}, From: Top}.Apply(s, 0.25)
		if n := rowsShown(s)[0]; n != 5 {
			t.Fatalf("shown = %d, want 5", n)
		}
	})

	t.Run("slide", func(t *testing.T) {
		t.Parallel()
		// A list at x=10 in a viewport 30 wide; each row has its own letter,
		// so a row's cells can be told from the others'.
		e := Stagger{Effect: Slide{From: Left}, From: Top}
		want := make([]int, 6)
		for i, at := range []float64{0, 0.25, 0.5, 0.75, 0.95} {
			s := textLayerAt(gridText(20, 6), image.Pt(10, 1), 30, 8)
			e.Apply(s, at)
			if s.Offset != (image.Point{}) {
				t.Fatalf("t=%.2f: offset of the whole surface changed to %v", at, s.Offset)
			}
			shown := rowsShown(s)
			if at == 0.25 && shown[0] != 10 {
				// The first row is halfway: slides start at the node's edge,
				// not the viewport's.
				t.Fatalf("t=0.25: the first row shows %d cells, want 10", shown[0])
			}
			for y := range s.Height() {
				row := visibleRow(s, y)
				if strings.Trim(strings.ReplaceAll(row, "_", ""), string(rune('a'+y))) != "" {
					t.Fatalf("t=%.2f: row %d holds %q, cells left their row", at, y, row)
				}
				// Slid cells are clipped to the node: they end at its right
				// edge only once the row is done.
				if shown[y] > 0 && shown[y] < 20 && !strings.HasPrefix(strings.TrimLeft(row, "_"), string(rune('a'+y))) {
					t.Fatalf("t=%.2f: row %d is %q", at, y, row)
				}
				if shown[y] > 0 && shown[y] < 20 && strings.HasPrefix(row, "_") && row[len(row)-1] == '_' {
					t.Fatalf("t=%.2f: row %d is %q, a slide leaves the left side empty", at, y, row)
				}
				if shown[y] < want[y] {
					t.Fatalf("t=%.2f: row %d shows %d cells, fewer than %d before", at, y, shown[y], want[y])
				}
				want[y] = shown[y]
			}
			// Rows go in from the top: an earlier row is never further behind.
			for y := 1; y < len(shown); y++ {
				if shown[y] > shown[y-1] {
					t.Fatalf("t=%.2f: row %d shows %d cells, more than the row above (%d)", at, y, shown[y], shown[y-1])
				}
			}
			if i == 0 && hidden(s) != s.Width()*s.Height() {
				t.Fatalf("t=0 hides %d cells", hidden(s))
			}
		}
		if want[0] != 20 {
			t.Fatalf("the first row shows %d cells at t=0.95", want[0])
		}
	})

	t.Run("identity at the end", func(t *testing.T) {
		t.Parallel()
		s := textLayerAt(gridText(10, 4), image.Pt(2, 2), 20, 10)
		Stagger{Effect: Slide{From: Left}, From: Top}.Apply(s, 1)
		if hidden(s) != 0 || visibleRow(s, 3) != "dddddddddd" {
			t.Fatal("t=1 changed the node")
		}
	})

	t.Run("the effect owns its end", func(t *testing.T) {
		t.Parallel()
		s := boxLayer(10, 4)
		Stagger{Effect: Reverse{Effect: Wipe{From: Left}}, From: Top}.Apply(s, 1)
		if hidden(s) != 40 {
			t.Fatalf("t=1 hides %d, want all, as the reversed wipe does", hidden(s))
		}
	})

	t.Run("wide glyphs", func(t *testing.T) {
		t.Parallel()
		for _, from := range []Edge{Top, Left} {
			for i := range 21 {
				s := textLayerAt("世界世界\nab世cd\n世a世界", image.Pt(2, 2), 20, 10)
				Stagger{Effect: Slide{From: Left}, From: from}.Apply(s, float64(i)/20)
				for y := range s.Height() {
					assertRowWhole(t, s, y)
				}
			}
		}
	})

	t.Run("nil effect", func(t *testing.T) {
		t.Parallel()
		s := boxLayer(6, 4)
		Stagger{}.Apply(s, 0.5)
		Delay{}.Apply(s, 0.5)
		Reverse{}.Apply(s, 0.5)
		Ease{}.Apply(s, 0.5)
		if hidden(s) != 0 {
			t.Fatal("effects without an inner effect hid cells")
		}
	})
}

func TestDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		e    Delay
		t    float64
		want float64 // what the inner effect should see
	}{
		{"before", Delay{From: 0.25, To: 0.75}, 0.1, 0},
		{"at the start", Delay{From: 0.25, To: 0.75}, 0.25, 0},
		{"middle", Delay{From: 0.25, To: 0.75}, 0.5, 0.5},
		{"after", Delay{From: 0.25, To: 0.75}, 0.9, 1},
		{"zero to is one", Delay{From: 0.5}, 0.75, 0.5},
		{"zero value passes t", Delay{}, 0.3, 0.3},
		{"no length", Delay{From: 0.5, To: 0.5}, 0.4, 0},
		{"no length, after", Delay{From: 0.5, To: 0.5}, 0.5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var seen float64 = -1
			tt.e.Effect = Func(func(_ *canvas.Surface, at float64) { seen = at })
			tt.e.Apply(boxLayer(4, 3), tt.t)
			if diff := seen - tt.want; diff > 1e-9 || diff < -1e-9 {
				t.Fatalf("inner effect saw %v, want %v", seen, tt.want)
			}
		})
	}

	t.Run("in sequence", func(t *testing.T) {
		t.Parallel()
		// A wipe over the first half, then a typewriter over the second,
		// which hides what the wipe showed until it gets its turn.
		first, second := Delay{Effect: Wipe{From: Left}, To: 0.5}, Delay{Effect: Typewriter{}, From: 0.5}
		for at, want := range map[float64]int{0.25: 40, 0.5: 40, 0.75: 20, 1: 0} {
			s := boxLayer(10, 4)
			first.Apply(s, at)
			second.Apply(s, at)
			if n := hidden(s); n != want {
				t.Fatalf("t=%.2f: %d hidden, want %d", at, n, want)
			}
		}
	})
}

func TestReverse(t *testing.T) {
	t.Parallel()

	s := boxLayer(10, 4)
	Reverse{Effect: Wipe{From: Left}}.Apply(s, 1)
	if hidden(s) != 40 {
		t.Fatalf("t=1 hides %d, want all", hidden(s))
	}
	s = boxLayer(10, 4)
	Reverse{Effect: Wipe{From: Left}}.Apply(s, 0)
	if hidden(s) != 0 {
		t.Fatalf("t=0 hides %d, want none", hidden(s))
	}
	s = boxLayer(10, 4)
	Reverse{Effect: Wipe{From: Left}}.Apply(s, 0.7)
	direct := boxLayer(10, 4)
	Wipe{From: Left}.Apply(direct, 0.3)
	if hidden(s) != hidden(direct) {
		t.Fatalf("t=0.7 hides %d, want %d", hidden(s), hidden(direct))
	}
}

func TestEase(t *testing.T) {
	t.Parallel()

	var seen []float64
	record := Func(func(_ *canvas.Surface, t float64) { seen = append(seen, t) })
	overshoot := func(t float64) float64 { return t * 1.1 }
	s := boxLayer(4, 3)
	for _, at := range []float64{0, 0.5, 1} {
		Ease{Effect: record, Easing: overshoot}.Apply(s, at)
	}
	Ease{Effect: record}.Apply(s, 0.5)
	want := []float64{0, 0.55, 1, 0.5}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("saw %v, want %v", seen, want)
	}
}

func TestCombinatorsComparable(t *testing.T) {
	t.Parallel()

	same := []Effect{
		Stagger{Effect: Slide{From: Left}, From: Top},
		Delay{Effect: Fade{}, From: 0.5},
		Reverse{Effect: Dissolve{Seed: 1}},
		Delay{Effect: Reverse{Effect: Stagger{Effect: Wipe{}}}},
		Unfold{},
		Iris{At: image.Pt(3, 4)},
		Shade{Seed: 2},
		Blinds{From: Top, Size: 2},
		Wave{From: Left},
	}
	for _, e := range same {
		if !reflect.TypeOf(e).Comparable() {
			t.Errorf("%T is not comparable", e)
		}
	}
	if (Stagger{Effect: Wipe{From: Left}}) == (Stagger{Effect: Wipe{From: Right}}) {
		t.Error("different staggers compare equal")
	}
	if reflect.TypeFor[Ease]().Comparable() {
		t.Error("Ease holds a func and cannot be comparable")
	}
}

func BenchmarkStaggerFullScreen(b *testing.B) {
	benchReveal(b, Stagger{Effect: Wipe{From: Left}, From: Top})
}

func BenchmarkStaggerSlideFullScreen(b *testing.B) {
	benchReveal(b, Stagger{Effect: Slide{From: Left}, From: Top})
}

func BenchmarkStaggerColumnsFullScreen(b *testing.B) {
	benchReveal(b, Stagger{Effect: Wipe{From: Top}, From: Left})
}
