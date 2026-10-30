// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	"github.com/lrstanley/x/term/reel/vt"
)

var (
	promptFg = color.NRGBA{R: 0xe0, G: 0xe0, B: 0xe0, A: 0xff}
	promptBg = color.NRGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff}
)

// promptState has two rows of scrollback, a prompt in it, and three on the
// screen: one whose command exited with 0, one with 2, and one still
// running.
func promptState() vt.State {
	st := focused
	st.Foreground, st.Background = promptFg, promptBg
	st.ScrollbackLen = 2
	st.PromptMarks = []vt.PromptMark{
		{Row: 0, Ended: true, HasExitCode: true, ExitCode: 1},
		{Row: 2, Ended: true, HasExitCode: true},
		{Row: 3, Ended: true, HasExitCode: true, ExitCode: 2},
		{Row: 4},
	}
	return st
}

func TestPromptMarksOffByDefault(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(12, 4)
	d := newRenderer(t)
	plain := focused
	plain.Foreground, plain.Background = promptFg, promptBg
	if !bytes.Equal(drawFresh(d, scr, promptState(), 0).Pix, drawFresh(d, scr, plain, 0).Pix) {
		t.Fatal("prompt marks changed the image without WithPromptMarks")
	}
}

// TestPromptMarks checks that the marks leave the layout alone, and that only
// the row of the prompt whose command failed gets a badge.
func TestPromptMarks(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(12, 4)
	plain, d := newRenderer(t), newRenderer(t, WithPromptMarks(true))
	if l, was := d.Layout(12, 4), plain.Layout(12, 4); l != was {
		t.Fatalf("layout = %+v, want %+v", l, was)
	}

	l := d.Layout(12, 4)
	img := drawFresh(d, scr, promptState(), 0)
	badge := func(y int) color.NRGBA {
		r := l.CellRect(11, y)
		return img.NRGBAAt(r.Max.X-2, r.Min.Y+2)
	}
	if got, want := badge(1), icol.BlendNRGBA(promptBg, defaultProgressError, promptBadgeTint); got != want {
		t.Errorf("failed row: badge = %v, want %v", got, want)
	}
	for _, y := range []int{0, 2, 3} {
		if got := badge(y); got != promptBg {
			t.Errorf("row %d: badge = %v, want none", y, got)
		}
	}
}

// TestPromptMarkBadgeCoversText checks that the badge is drawn over text at
// the end of the prompt row, such as a right prompt.
func TestPromptMarkBadgeCoversText(t *testing.T) {
	t.Parallel()

	d := newRenderer(t, WithPromptMarks(true))
	empty, text := newTestScreen(12, 4), newTestScreen(12, 4)
	putText(text, 6, 1, "RPROMPT", vt.Style{})
	l := d.Layout(12, 4)
	row := image.Rect(l.CellRect(0, 1).Min.X, l.CellRect(0, 1).Min.Y, l.Grid.Max.X, l.CellRect(0, 1).Max.Y)
	a, b := drawFresh(d, empty, promptState(), 0), drawFresh(d, text, promptState(), 0)
	for x := row.Max.X - 2*l.Cell.X; x < row.Max.X; x++ {
		for y := row.Min.Y; y < row.Max.Y; y++ {
			if a.NRGBAAt(x, y) != b.NRGBAAt(x, y) {
				t.Fatalf("text shows through the badge at %d,%d", x, y)
			}
		}
	}
}

// TestPromptMarkTicks checks that with a scrollbar, every prompt gets a tick
// in one color, and that those on the screen shown land in the thumb.
func TestPromptMarkTicks(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(12, 4)
	d := newRenderer(t, WithPromptMarks(true), WithScrollbar(true), WithScrollbarWidth(6))
	st := focused
	st.Foreground, st.Background = promptFg, promptBg
	// The screen shows rows 30 to 33.
	st.ScrollbackLen, st.ScrollbackOffset = 40, 10
	st.PromptMarks = []vt.PromptMark{{Row: 5}, {Row: 31, Ended: true, HasExitCode: true, ExitCode: 1}, {Row: 33}, {Row: 42}}
	img := drawFresh(d, scr, st, 0)
	bar := d.layoutSnapshot(t, image.Point{}, scr).scrollbarBounds
	x := bar.Min.X + bar.Dx()/2
	tick := icol.BlendNRGBA(promptBg, promptFg, promptTickTint)
	thumb := icol.BlendNRGBA(promptBg, promptFg, 0.35)

	var ticks []int
	thumbTop, thumbBottom := bar.Max.Y, bar.Min.Y
	for y := bar.Min.Y; y < bar.Max.Y; y++ {
		switch img.NRGBAAt(x, y) {
		case tick:
			if len(ticks) == 0 || ticks[len(ticks)-1] < y-1 {
				ticks = append(ticks, y)
			}
		case thumb:
			thumbTop, thumbBottom = min(thumbTop, y), max(thumbBottom, y)
		}
	}
	if len(ticks) != len(st.PromptMarks) {
		t.Fatalf("ticks at %v, want %d", ticks, len(st.PromptMarks))
	}
	for _, i := range []int{1, 2} {
		if ticks[i] < thumbTop-1 || ticks[i] > thumbBottom+1 {
			t.Errorf("tick of row %d at %d, outside the thumb %d..%d", st.PromptMarks[i].Row, ticks[i], thumbTop, thumbBottom)
		}
	}
	if ticks[0] >= thumbTop || ticks[3] <= thumbBottom {
		t.Errorf("ticks %v of rows off the screen are in the thumb %d..%d", ticks, thumbTop, thumbBottom)
	}
}

// TestPromptMarksRepaint checks that one renderer, drawing the same screen
// again, repaints when only the marks change.
func TestPromptMarksRepaint(t *testing.T) {
	t.Parallel()

	scr := vt.NewBuffer(12, 4)
	d := newRenderer(t, WithPromptMarks(true))
	st := promptState()
	drawAt(t, d, scr, st, 0)
	scr.ClearDirty()
	st.PromptMarks = append([]vt.PromptMark(nil), st.PromptMarks...)
	st.PromptMarks[3] = vt.PromptMark{Row: 4, Ended: true, HasExitCode: true, ExitCode: 1}
	if !bytes.Equal(drawAt(t, d, scr, st, 0).Pix, drawFresh(d, scr, st, 0).Pix) {
		t.Fatal("the frame cache kept the old marks")
	}
}
