// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"regexp"
	"slices"
	"testing"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// regionRuntime opens the terminals the region tests draw on.
var regionRuntime = vt.NewRuntime(0)

// cellFrame returns the terminal of a cols by rows terminal that out was
// written to.
func cellFrame(t *testing.T, cols, rows int, out string) *reel.CellFrame {
	t.Helper()
	term, err := regionRuntime.Open(t.Context(), cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if _, err = term.Write([]byte(out)); err != nil {
		t.Fatal(err)
	}
	return &reel.CellFrame{Screen: term.Screen(), State: term.State()}
}

func TestRegions(t *testing.T) {
	t.Parallel()
	// Row 0: "é" is one cell, a letter and a combining accent; "日" and "本"
	// are two cells each, and so are "👍" and its skin tone, which this
	// terminal shows apart without grapheme clustering (mode 2027).
	// Row 1: the same with mode 2027, where "👍🏽" is one wide cell.
	const screen = "éx日本👍🏽z\r\n\x1b[?2027hé👍🏽a ab ab"
	f := cellFrame(t, 14, 3, screen)
	rect := image.Rect
	for _, tc := range []struct {
		name   string
		region Region
		want   []image.Rectangle
	}{
		{"cells", Cells(rect(10, 0, 12, 2)), []image.Rectangle{rect(10, 0, 12, 2)}},
		{"cells reversed", Cells(rect(12, 2, 10, 0)), []image.Rectangle{rect(10, 0, 12, 2)}},
		// Half of a wide character is the whole of it.
		{"cells cut a wide character", Cells(rect(3, 0, 5, 1)), []image.Rectangle{rect(2, 0, 6, 1)}},
		{"cells off screen", Cells(rect(12, 2, 20, 9)), []image.Rectangle{rect(12, 2, 14, 3)}},
		{"cells all off screen", Cells(rect(20, 0, 30, 1)), nil},
		{"row", Row(1), []image.Rectangle{rect(0, 1, 14, 2)}},
		{"row off screen", Row(5), nil},
		{"rows", Rows(0, 2), []image.Rectangle{rect(0, 0, 14, 2)}},
		{"rows reversed", Rows(2, 0), []image.Rectangle{rect(0, 0, 14, 2)}},
		{"text", Text("x"), []image.Rectangle{rect(1, 0, 2, 1)}},
		{"text wide", Text("本"), []image.Rectangle{rect(4, 0, 6, 1)}},
		{"text across wide", Text("日本👍"), []image.Rectangle{rect(2, 0, 8, 1)}},
		// Split in two cells on row 0, one on row 1.
		{"text emoji with skin tone", Text("👍🏽"), []image.Rectangle{rect(6, 0, 10, 1), rect(1, 1, 3, 2)}},
		// Part of a grapheme cluster covers its cell.
		{"text part of a cluster", Text("e"), []image.Rectangle{rect(0, 0, 1, 2)}},
		{"text many per row", Text("ab"), []image.Rectangle{rect(5, 1, 7, 2), rect(8, 1, 10, 2)}},
		{"text with space", Text("a a"), []image.Rectangle{rect(3, 1, 6, 2)}},
		{"text empty", Text(""), nil},
		{"text missing", Text("nope"), nil},
		{"text over rows", Text("z\ne"), nil},
		{"regexp", Regexp(regexp.MustCompile(`a+b`)), []image.Rectangle{rect(5, 1, 7, 2), rect(8, 1, 10, 2)}},
		// "." is the accent of "é", which is one cell.
		{"regexp anchors a row", Regexp(regexp.MustCompile(`^e.x`)), []image.Rectangle{rect(0, 0, 2, 1)}},
		{"regexp wide", Regexp(regexp.MustCompile(`[日本]+`)), []image.Rectangle{rect(2, 0, 6, 1)}},
		// Empty matches, on every row, select nothing.
		{"regexp empty matches", Regexp(regexp.MustCompile(`q*`)), nil},
		{"regexp nil", Regexp(nil), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := regionCells(tc.region, f); !slices.Equal(got, tc.want) {
				t.Fatalf("cells = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegionEnv(t *testing.T) {
	f := cellFrame(t, 20, 3, "token=s3cr3t\r\nkey=line1\r\nline2 x")
	t.Setenv("REEL_TEST_TOKEN", "s3cr3t")
	t.Setenv("REEL_TEST_KEY", "line1\r\nline2\n")
	t.Setenv("REEL_TEST_EMPTY", "")
	t.Setenv("REEL_TEST_BLANK", " \t")
	for _, tc := range []struct {
		names []string
		want  []image.Rectangle
	}{
		{[]string{"REEL_TEST_TOKEN"}, []image.Rectangle{image.Rect(6, 0, 12, 1)}},
		// Each line of a value matches on its own.
		{[]string{"REEL_TEST_KEY"}, []image.Rectangle{image.Rect(4, 1, 9, 2), image.Rect(0, 2, 5, 3)}},
		{[]string{"REEL_TEST_TOKEN", "REEL_TEST_KEY"}, []image.Rectangle{
			image.Rect(6, 0, 12, 1), image.Rect(4, 1, 9, 2), image.Rect(0, 2, 5, 3),
		}},
		// Empty, blank, and unset variables match nothing, not every cell.
		{[]string{"REEL_TEST_EMPTY", "REEL_TEST_BLANK", "REEL_TEST_UNSET"}, nil},
		{nil, nil},
	} {
		if got := regionCells(Env(tc.names...), f); !slices.Equal(got, tc.want) {
			t.Errorf("Env(%q) = %v, want %v", tc.names, got, tc.want)
		}
	}
	// The value is read when the region is matched.
	t.Setenv("REEL_TEST_TOKEN", "line2")
	if got, want := regionCells(Env("REEL_TEST_TOKEN"), f), []image.Rectangle{image.Rect(0, 2, 5, 3)}; !slices.Equal(got, want) {
		t.Errorf("Env after a change = %v, want %v", got, want)
	}
}

// TestRegionWrapped checks that text regions match across the rows a line
// soft-wrapped over, but not across a line break.
func TestRegionWrapped(t *testing.T) {
	t.Setenv("REEL_TEST_WRAPPED", "abcdefghij")
	rect := image.Rect
	for _, tc := range []struct {
		name   string
		out    string
		region Region
		want   []image.Rectangle
	}{
		// Over two rows of 4 columns: "xxs3" and "cr3t".
		{"text over two rows", "xxs3cr3tyy", Text("s3cr3t"), []image.Rectangle{rect(2, 0, 4, 1), rect(0, 1, 4, 2)}},
		// Over three: "xabc", "defg", and "hij".
		{"text over three rows", "xabcdefghij", Text("abcdefghij"), []image.Rectangle{rect(1, 0, 4, 1), rect(0, 1, 4, 2), rect(0, 2, 3, 3)}},
		{"regexp over three rows", "xabcdefghij", Regexp(regexp.MustCompile(`c\w+i`)), []image.Rectangle{rect(3, 0, 4, 1), rect(0, 1, 4, 2), rect(0, 2, 2, 3)}},
		{"env over three rows", "xabcdefghij", Env("REEL_TEST_WRAPPED"), []image.Rectangle{rect(1, 0, 4, 1), rect(0, 1, 4, 2), rect(0, 2, 3, 3)}},
		// A wrapped row's line goes on, so ^ is not at its start.
		{"regexp anchor in a line", "xabcdefghij", Regexp(regexp.MustCompile(`^d`)), nil},
		{"regexp anchors a line", "xabcdefghij", Regexp(regexp.MustCompile(`^xa`)), []image.Rectangle{rect(0, 0, 2, 1)}},
		// A line break ends the line, after the blanks of its row.
		{"newline", "ab\r\ncd", Text("b  c"), nil},
		{"newline joined", "ab\r\ncd", Text("abcd"), nil},
		// A wide character that does not fit wraps whole, leaving a spacer
		// at the end of the row that reads as nothing.
		{"wide wrapped", "abc\u4e2dd", Text("c\u4e2d"), []image.Rectangle{rect(2, 0, 3, 1), rect(0, 1, 2, 2)}},
		{"wide spacer", "abc\u4e2dd", Text("c \u4e2d"), nil},
		// One that fits ends the row.
		{"wide at the end", "ab\u4e2dde", Text("\u4e2dd"), []image.Rectangle{rect(2, 0, 4, 1), rect(0, 1, 1, 2)}},
	} {
		f := cellFrame(t, 4, 3, tc.out)
		if got := regionCells(tc.region, f); !slices.Equal(got, tc.want) {
			t.Errorf("%s: cells = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRegionWiden checks that each row of a region covers the whole of the
// wide characters it has part of, rather than the rectangle growing on
// every row.
func TestRegionWiden(t *testing.T) {
	t.Parallel()
	// 日 and 本 are two cells each.
	f := cellFrame(t, 6, 2, "日本xx\r\na日bxx")
	got := regionCells(Cells(image.Rect(3, 0, 5, 2)), f)
	want := []image.Rectangle{image.Rect(2, 0, 5, 1), image.Rect(3, 1, 5, 2)}
	if !slices.Equal(got, want) {
		t.Fatalf("cells = %v, want %v", got, want)
	}
	// The left half of 日 on row 1, and the right half on row 0 of 本.
	got = regionCells(Cells(image.Rect(1, 0, 2, 2)), f)
	want = []image.Rectangle{image.Rect(0, 0, 2, 1), image.Rect(1, 1, 3, 2)}
	if !slices.Equal(got, want) {
		t.Fatalf("cells = %v, want %v", got, want)
	}
}

func TestCellRuns(t *testing.T) {
	t.Parallel()
	rect := image.Rect
	const w, h = 6, 4
	for _, tc := range []struct {
		in, want []image.Rectangle
	}{
		// Touching on one row, then the row below with the same columns.
		{[]image.Rectangle{rect(0, 0, 2, 1), rect(2, 0, 4, 1), rect(0, 1, 4, 2)}, []image.Rectangle{rect(0, 0, 4, 2)}},
		// Overlapping and contained.
		{[]image.Rectangle{rect(0, 0, 3, 1), rect(2, 0, 5, 1), rect(1, 0, 2, 1)}, []image.Rectangle{rect(0, 0, 5, 1)}},
		// Diagonal neighbors, and rows of different widths, stay apart.
		{[]image.Rectangle{rect(0, 0, 1, 1), rect(1, 1, 2, 2), rect(0, 2, 3, 3)}, []image.Rectangle{rect(0, 0, 1, 1), rect(1, 1, 2, 2), rect(0, 2, 3, 3)}},
		// An L is a run on top of a narrower block.
		{[]image.Rectangle{rect(0, 0, 4, 3), rect(0, 0, 6, 1)}, []image.Rectangle{rect(0, 0, 6, 1), rect(0, 1, 4, 3)}},
		// Two runs on a row, each grown down on its own.
		{[]image.Rectangle{rect(0, 0, 1, 4), rect(3, 1, 4, 3)}, []image.Rectangle{rect(0, 0, 1, 4), rect(3, 1, 4, 3)}},
	} {
		covered := make([]bool, w*h)
		for _, r := range tc.in {
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					covered[y*w+x] = true
				}
			}
		}
		if got := cellRuns(covered, w, h); !slices.Equal(got, tc.want) {
			t.Errorf("cellRuns(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
