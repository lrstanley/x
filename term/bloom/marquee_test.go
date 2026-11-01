// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMarquee(t *testing.T) {
	t.Parallel()

	// A loop of the text is 13 cells with the default gap, 10 without. at
	// returns the phase for a cell of it, in the middle of the cell so that
	// rounding cannot move it.
	const text = "0123456789"
	at := func(cell, period float64) float64 { return (cell + 0.5) / period }
	tests := []struct {
		name  string
		node  MarqueeNode
		width int
		want  string
	}{
		{"fits", Marquee("abc", 0), 10, "abc"},
		{"fits scrolling", Marquee("abc", 0.5), 10, "abc"},
		{"fits exactly", Marquee(text, 0.5), 10, text},
		{"truncated at phase 0", Marquee(text, 0), 6, "01234…"},
		{"newlines are spaces", Marquee("ab\ncd\r\nef", 0), 20, "ab cd ef"},
		{"start of the loop", Marquee(text, 1), 6, "012345"},
		{"wraps to the start", Marquee(text, 2), 6, "012345"},
		{"a few cells in", Marquee(text, at(3, 13)), 6, "345678"},
		{"into the gap", Marquee(text, at(8, 13)), 6, "89   0"},
		{"in the gap", Marquee(text, at(11, 13)), 6, "  0123"},
		{"back around", Marquee(text, at(12, 13)), 6, " 01234"},
		{"negative phase wraps", Marquee(text, at(12, 13)-1), 6, " 01234"},
		{"custom gap", Marquee(text, at(10, 13)).Gap(" | "), 6, " | 012"},
		{"no gap", Marquee(text, at(7, 10)).Gap(""), 6, "789012"},
		{"nan", Marquee(text, math.NaN()), 6, "01234…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ansi.Strip(Render(tt.node, tt.width, 1))
			if want := padTo(tt.want, tt.width); got != want {
				t.Fatalf("rendered %q, want %q", got, want)
			}
		})
	}

	if got := Marquee(text, 0.5).Layout(Context{}); got != nil {
		t.Fatalf("laid out %v with no room", got)
	}
}

func TestMarqueeNaturalSize(t *testing.T) {
	t.Parallel()

	n := Marquee("hello world", 0.3)
	if got := Render(n, 0, 0); ansi.Strip(got) != "hello world" {
		t.Fatalf("natural render = %q", got)
	}
	if v := n.View(); v != Render(n, 0, 0) {
		t.Fatalf("View = %q", v)
	}

	// It shrinks in a slot, instead of pushing its neighbours out.
	got := ansi.Strip(Render(Row(Fixed(5, Text("ab")), Fill(Marquee("hello world", 0)), Fixed(1, Text("|"))), 20, 1))
	if want := "ab   hello world   |"; got != want {
		t.Fatalf("in a row: %q, want %q", got, want)
	}
	got = ansi.Strip(Render(Row(Fixed(5, Text("ab")), Fill(Marquee("hello world", 0)), Fixed(1, Text("|"))), 12, 1))
	if got != "ab   hello…|" {
		t.Fatalf("shrunk in a row: %q", got)
	}
}

func TestMarqueeWide(t *testing.T) {
	t.Parallel()

	// Each glyph is two cells wide: a loop is 10+3 cells. Whichever cell the
	// cut lands on, the line is as wide as asked, and a glyph is never half
	// shown.
	const text = "世界你好啊"
	n := 13
	for i := range n {
		for _, width := range []int{4, 5, 6, 7} {
			phase := (float64(i) + 0.5) / float64(n)
			got := ansi.Strip(Render(Marquee(text, phase), width, 1))
			if w := ansi.StringWidth(got); w != width {
				t.Fatalf("phase %d/%d width %d: %q is %d cells", i, n, width, got, w)
			}
			for _, r := range got {
				if r != ' ' && !strings.ContainsRune(text, r) {
					t.Fatalf("phase %d/%d width %d: %q holds %q", i, n, width, got, r)
				}
			}
		}
	}

	if got := ansi.Strip(Render(Marquee(text, 0), 5, 1)); got != "世界…" {
		t.Fatalf("truncated at a wide glyph: %q", got)
	}
	// The cut lands in the middle of the second glyph, at cell 3.
	if got := ansi.Strip(Render(Marquee(text, 3.5/13), 5, 1)); got != " 你好" {
		t.Fatalf("scrolled half a glyph: %q", got)
	}
	if got := ansi.Strip(Render(Marquee(text, 4.5/13), 5, 1)); got != "你好 " {
		t.Fatalf("scrolled whole glyphs: %q", got)
	}
}

func TestMarqueeStyle(t *testing.T) {
	t.Parallel()

	st := lipgloss.NewStyle().Padding(0, 1).Foreground(color.RGBA{R: 255, A: 255})
	tests := []struct {
		phase float64
		want  string
	}{
		{0, " 01234… "},
		{3.5 / 13, " 345678 "},
		{0.5, " 6789   "},
	}
	for _, tt := range tests {
		got := Render(Marquee("0123456789", tt.phase).Style(st), 8, 1)
		if ansi.Strip(got) != tt.want {
			t.Errorf("phase %v: %q, want %q", tt.phase, ansi.Strip(got), tt.want)
		}
		if !strings.Contains(got, "\x1b[") {
			t.Errorf("phase %v: %q is not styled", tt.phase, got)
		}
	}

	// Padding is part of the natural width, and counts against a fit.
	if got := ansi.Strip(Render(Marquee("abc", 0).Style(st), 0, 0)); got != " abc " {
		t.Fatalf("natural width with padding = %q", got)
	}
	if got := ansi.Strip(Render(Marquee("abcdef", 0).Style(st), 7, 1)); got != " abcd… " {
		t.Fatalf("padding against the width = %q", got)
	}
}

// BenchmarkMarqueeFrame lays out and draws a frame of 30 scrolling marquees
// and a selection of truncated ones, as in a list, on a 200×60 terminal.
func BenchmarkMarqueeFrame(b *testing.B) {
	r := New(WithColors(color.White, color.Black))
	st := benchItem
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		items := make([]Child, 0, benchH)
		for j := range benchH {
			phase := 0.0
			if j == i%benchH {
				phase = float64(i%240+1) / 240
			}
			items = append(items, Marquee("item with a very long name that has to be cut off or scrolled across the row", phase).Style(st))
		}
		r.View(benchW, benchH, Row(Fixed(40, Column(items...))))
	}
}
