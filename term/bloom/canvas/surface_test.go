// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package canvas

import (
	"image"
	"strings"
	"testing"
)

// shown returns the text of s's shown cells, one line per row, with hidden
// cells as '_' and the tails of wide glyphs left out.
func shown(s *Surface) string {
	var b strings.Builder
	for y := range s.Height() {
		if y > 0 {
			b.WriteByte('\n')
		}
		for x := range s.Width() {
			c := s.Cell(x, y)
			switch {
			case c.Width == 0:
			case !s.Visible(x, y):
				b.WriteByte('_')
			default:
				b.WriteString(c.Content)
			}
		}
	}
	return b.String()
}

func layerOf(src string) *Surface {
	c := Parse(src, method)
	return c.Layer(c.Bounds().Add(image.Pt(2, 1)), image.Rect(0, 0, 40, 20), Parse("", method), DefaultEnv, &Mixer{})
}

func TestSub(t *testing.T) {
	t.Parallel()

	s := layerOf("abcd\nefgh\nijkl")
	s.Offset = image.Pt(1, 1)
	sub := s.Sub(image.Rect(1, 1, 3, 3))

	if sub.Width() != 2 || sub.Height() != 2 {
		t.Fatalf("size = %dx%d, want 2x2", sub.Width(), sub.Height())
	}
	if want := image.Rect(3, 2, 5, 4); sub.Rect() != want {
		t.Fatalf("rect = %v, want %v", sub.Rect(), want)
	}
	if sub.Offset != s.Offset || sub.Viewport != s.Viewport {
		t.Fatalf("offset %v viewport %v, want the parent's", sub.Offset, sub.Viewport)
	}
	if c := sub.Cell(0, 0); c == nil || c.Content != "f" {
		t.Fatalf("cell 0,0 = %v, want f", c)
	}
	if sub.Cell(2, 0) != nil || sub.Visible(0, 2) {
		t.Fatal("the sub surface reaches past its bounds")
	}

	// Hiding and offsets: shared cells and mask, but not the offset.
	sub.Hide(1, 1) // k
	sub.Offset = image.Pt(5, 5)
	if s.Visible(2, 2) || s.Offset != image.Pt(1, 1) {
		t.Fatalf("parent: visible(2,2)=%v offset=%v", s.Visible(2, 2), s.Offset)
	}
	sub.Cell(0, 0).Content = "F"
	if s.Cell(1, 1).Content != "F" {
		t.Fatal("the cells are not shared")
	}

	// Nested, and clipped to the parent.
	inner := sub.Sub(image.Rect(1, 0, 9, 9))
	if inner.Width() != 1 || inner.Height() != 2 || inner.Visible(0, 1) || !inner.Visible(0, 0) {
		t.Fatalf("inner = %dx%d, visible %v/%v", inner.Width(), inner.Height(), inner.Visible(0, 0), inner.Visible(0, 1))
	}
	if empty := s.Sub(image.Rect(9, 9, 12, 12)); empty.Width() != 0 || empty.Height() != 0 {
		t.Fatal("a sub surface outside the parent is not empty")
	}
}

func TestMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		hide []image.Point
		r    image.Rectangle
		d    image.Point
		want string
	}{
		{"right", "abcd\nefgh", nil, image.Rect(0, 0, 4, 2), image.Pt(1, 0), "_abc\n_efg"},
		{"left", "abcd\nefgh", nil, image.Rect(0, 0, 4, 2), image.Pt(-2, 0), "cd__\ngh__"},
		{"down", "abc\ndef\nghi", nil, image.Rect(0, 0, 3, 3), image.Pt(0, 1), "___\nabc\ndef"},
		{"up and left", "abc\ndef\nghi", nil, image.Rect(0, 0, 3, 3), image.Pt(-1, -1), "ef_\nhi_\n___"},
		{"only inside r", "abcd\nefgh", nil, image.Rect(1, 0, 3, 1), image.Pt(1, 0), "a_bd\nefgh"},
		{"all the way out", "abcd", nil, image.Rect(0, 0, 4, 1), image.Pt(-9, 0), "____"},
		{"out of the surface rect", "ab", nil, image.Rect(-2, -2, 9, 9), image.Pt(1, 0), "_a"},
		{"hidden moves along", "abcd", []image.Point{{1, 0}}, image.Rect(0, 0, 4, 1), image.Pt(1, 0), "_a_c"},
		{"wide glyph moves whole", "a世cd", nil, image.Rect(0, 0, 5, 1), image.Pt(1, 0), "_a世c"},
		{"wide glyph cut at the right", "a世cd", nil, image.Rect(0, 0, 5, 1), image.Pt(3, 0), "___a "},
		{"wide glyph cut at the left", "ab世d", nil, image.Rect(0, 0, 5, 1), image.Pt(-3, 0), " d___"},
		{"wide glyph cut by r", "ab世d", nil, image.Rect(0, 0, 3, 1), image.Pt(-1, 0), "b _ d"},
		{"wide glyph outside r loses its tail", "ab世d", nil, image.Rect(3, 0, 5, 1), image.Pt(1, 0), "ab _ "},
		{"wide glyphs move down", "世a\nb世", nil, image.Rect(0, 0, 3, 2), image.Pt(0, 1), "___\n世a"},
		{"no move", "abc", nil, image.Rect(0, 0, 3, 1), image.Point{}, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := layerOf(tt.src)
			for _, p := range tt.hide {
				s.Hide(p.X, p.Y)
			}
			s.Move(tt.r, tt.d)
			if got := shown(s); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			assertWhole(t, s.c)
		})
	}

	t.Run("on a region", func(t *testing.T) {
		t.Parallel()
		c := Parse("abcd\nefgh", method)
		s := c.Region(c.Bounds(), DefaultEnv, &Mixer{})
		s.Move(c.Bounds(), image.Pt(1, 1))
		if got, want := c.String(), "\n abc"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if !s.Visible(0, 0) {
			t.Fatal("a region cannot hide cells")
		}
	})

	t.Run("through a sub surface", func(t *testing.T) {
		t.Parallel()
		s := layerOf("abcd\nefgh")
		band := s.Sub(image.Rect(0, 1, 4, 2))
		band.Move(image.Rect(0, 0, 4, 1), image.Pt(2, 0))
		if got, want := shown(s), "abcd\n__ef"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestMoveInSubKeepsGlyphsWhole(t *testing.T) {
	t.Parallel()

	for _, d := range []image.Point{{1, 0}, {-1, 0}} {
		s := layerOf("世ab世")
		s.Sub(image.Rect(1, 0, 5, 1)).Move(image.Rect(0, 0, 4, 1), d)
		for x := 0; x < s.Width(); {
			w := s.Cell(x, 0).Width
			if w == 0 {
				t.Fatalf("move by %v: tail at %d without its glyph: %q", d, x, shown(s))
			}
			for i := 1; i < w; i++ {
				if x+i >= s.Width() || s.Cell(x+i, 0).Width != 0 {
					t.Fatalf("move by %v: glyph at %d lost its tail: %q", d, x, shown(s))
				}
			}
			x += w
		}
	}
}
