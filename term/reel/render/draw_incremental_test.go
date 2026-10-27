// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/vt/screen"
)

// sliceColor is a color.Color whose dynamic type is not comparable, so
// comparing cells that hold it panics.
type sliceColor []uint8

func (c sliceColor) RGBA() (r, g, b, a uint32) { return uint32(c[0]) * 0x101, 0, 0, 0xffff }

var diffGlyphs = []string{
	"a", "b", "g", "Q", "j", "y", "W", "#", " ", "é", // text, descenders, ascenders
	"漢", "字", // wide
	"─", "│", "┌", "┼", "╬", "█", "▀", "▄", "▒", "⠿", "⣿", // box, blocks, braille
	"★", "✔", "☀", "\ue0b0", "\ue0b2", "\uf015", // fallback, powerline, nerd icons
}

// diffStatuses are program status record sets the incremental test moves
// between: none, still and animated drivers, and trees with dots.
var diffStatuses = [][]screen.ProgramStatus{
	nil,
	{{State: screen.ProgramWorking, Title: "build", Message: "compiling", Progress: -1}},
	{{State: screen.ProgramBlocked, Kind: screen.ProgramKindPermission, Title: "Edit?", Progress: -1}, {ID: "a", State: screen.ProgramDone, Progress: -1}},
	{{State: screen.ProgramWorking, Title: "tests", Progress: 42}},
	{{State: screen.ProgramDone, Title: "done", Progress: -1}, {ID: "a", State: screen.ProgramError, Title: "lint", Progress: -1}, {ID: "b", State: screen.ProgramIdle, Progress: -1}},
}

func randColor(r *rand.Rand) color.Color {
	switch r.IntN(4) {
	case 0:
		return nil
	case 1:
		return screen.Indexed(r.IntN(256))
	default:
		return color.NRGBA{uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(128 + r.IntN(128))}
	}
}

func randCell(r *rand.Rand) *screen.Cell {
	content := diffGlyphs[r.IntN(len(diffGlyphs))]
	c := &screen.Cell{Content: content, Width: 1}
	if content == "漢" || content == "字" {
		c.Width = 2
	}
	if r.IntN(3) > 0 {
		c.Style = screen.Style{
			Fg:        randColor(r),
			Bg:        randColor(r),
			Underline: screen.Underline(r.IntN(3)),
			Attrs:     screen.Attr(r.IntN(2))*screen.AttrBold | screen.Attr(r.IntN(2))*screen.AttrItalic | screen.Attr(r.IntN(4)/3)*screen.AttrFaint | screen.Attr(r.IntN(4)/3)*screen.AttrStrikethrough,
		}
	}
	return c
}

// TestDrawIncrementalMatchesFull runs a random edit/cursor sequence and
// requires every incremental Draw to equal a full repaint byte for byte.
func TestDrawIncrementalMatchesFull(t *testing.T) {
	t.Parallel()

	configs := []struct {
		name    string
		opts    []Option
		state   screen.State
		minIncr int
	}{
		{"plain", nil, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"margin-padding", []Option{WithMargin(7, color.NRGBA{R: 200, A: 255}), WithPadding(5)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"radius", []Option{WithMargin(6, color.NRGBA{B: 90, A: 255}), WithPadding(3), WithBorderRadius(14)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"radius-large", []Option{WithBorderRadius(60)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"scrollbar", []Option{WithScrollbar(true), WithPadding(2)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}, ScrollbackLen: 40}, 30},
		{"opacity", []Option{WithBackgroundOpacity(0.5)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"opacity-cells", []Option{WithBackgroundOpacity(0.5), WithBackgroundOpacityCells(true), WithMargin(4, nil)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"unfocused", []Option{WithFocusDimming(0.4), WithMargin(5, color.NRGBA{G: 120, A: 255}), WithPadding(4)}, screen.State{Cursor: screen.Cursor{Visible: true}}, 30},
		{"chrome", []Option{WithWindowChrome(true), WithPadding(3)}, screen.State{Title: "vim — main.go", Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"chrome-everything", []Option{
			WithWindowChrome(true), WithMargin(6, color.NRGBA{R: 30, G: 60, B: 90, A: 200}), WithPadding(4),
			WithBorderRadius(12), WithScrollbar(true), WithBackgroundOpacity(0.5), WithFocusDimming(0.35),
		}, screen.State{Title: "漢字  a very long title that has to be truncated to fit", Cursor: screen.Cursor{Visible: true}, ScrollbackLen: 25}, 30},
		{"progress", []Option{WithPadding(3), WithBorderRadius(8)}, screen.State{Progress: screen.Progress{State: screen.ProgressIndeterminate, Percent: -1}, Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"chrome-progress", []Option{WithWindowChrome(true), WithMargin(4, nil), WithBackgroundOpacity(0.5), WithProgressBarHeight(3)}, screen.State{Title: "build", Progress: screen.Progress{State: screen.ProgressSet, Percent: 30}, Cursor: screen.Cursor{Visible: true}}, 30},
		{"chrome-status", []Option{WithWindowChrome(true), WithMargin(8, color.NRGBA{R: 21, G: 22, B: 28, A: 255}), WithBorderRadius(8)}, screen.State{Title: "agent", ProgramStatuses: diffStatuses[1], Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"chrome-status-inset", []Option{WithWindowChrome(true), WithBorderRadius(12), WithBackgroundOpacity(0.5), WithFocusDimming(0.35)}, screen.State{Title: "agent", ProgramStatuses: diffStatuses[2], Cursor: screen.Cursor{Visible: true}}, 30},
		{"big-font", []Option{WithScale(scaleFor(26)), WithPadding(3)}, screen.State{Focused: true, Cursor: screen.Cursor{Visible: true}}, 30},
		{"everything", []Option{
			WithMargin(6, color.NRGBA{R: 30, G: 60, B: 90, A: 200}), WithPadding(4), WithBorderRadius(12),
			WithScrollbar(true), WithBackgroundOpacity(0.5), WithFocusDimming(0.35), WithFaintFactor(0.4),
		}, screen.State{Cursor: screen.Cursor{Visible: true}, ScrollbackLen: 25}, 30},
	}

	const cols, rows, steps = 24, 12, 80

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := append([]Option{WithScale(scaleFor(9))}, tc.opts...)
			inc, ref := MustNew(opts...), MustNew(opts...)
			t.Cleanup(func() { _ = inc.Close(); _ = ref.Close() })

			rng := rand.New(rand.NewPCG(7, uint64(len(tc.name))))
			buf := screen.NewBuffer(cols, rows)
			state := tc.state
			state.Cursor.Shape = screen.CursorBlock
			incremental := 0
			// The offset moves between steps, so the progress sweep and
			// blinks move too, and both draws of a step are at the same one.
			var at time.Duration

			for step := range steps {
				at += 37 * time.Millisecond
				switch step % 10 {
				case 0: // typing: a run of cells on one row
					y, x := rng.IntN(rows), rng.IntN(cols-6)
					for i := range 1 + rng.IntN(5) {
						buf.SetCell(x+i, y, randCell(rng))
					}
					state.Cursor.X, state.Cursor.Y = x+5, y
				case 1: // delete a cell
					buf.SetCell(rng.IntN(cols), rng.IntN(rows), nil)
				case 2: // cursor-only move (sometimes off the grid) and style change
					state.Cursor.X, state.Cursor.Y = rng.IntN(cols), rng.IntN(rows)
					if rng.IntN(4) == 0 {
						state.Cursor.Y = []int{-3, rows + 5, math.MaxInt}[rng.IntN(3)]
					}
					state.Cursor.Shape = screen.CursorShape(rng.IntN(4))
				case 3: // colour change on one cell
					buf.SetCell(rng.IntN(cols), rng.IntN(rows), randCell(rng))
				case 4: // edits at the first and last rows (margin, padding, corners)
					buf.SetCell(rng.IntN(cols), 0, randCell(rng))
					if rng.IntN(2) == 0 {
						buf.SetCell(rng.IntN(cols), rows-1, randCell(rng))
					}
				case 5: // nothing changes
				case 6: // cursor visibility toggle
					state.Cursor.Visible = !state.Cursor.Visible
				case 7: // a few scattered edits
					for range 1 + rng.IntN(4) {
						buf.SetCell(rng.IntN(cols), rng.IntN(rows), randCell(rng))
					}
				case 8: // title change, alone or with an edit
					state.Title = []string{"", "x", "a longer title — ünï ", "漢字", state.Title + "!"}[rng.IntN(5)]
					if rng.IntN(2) == 0 {
						buf.SetCell(rng.IntN(cols), rng.IntN(rows), randCell(rng))
					}
				case 9: // progress or program status change, alone or with an edit
					if rng.IntN(2) == 0 {
						state.Progress = screen.Progress{State: screen.ProgressState(rng.IntN(5)), Percent: rng.IntN(102) - 1}
					} else {
						state.ProgramStatuses = diffStatuses[rng.IntN(len(diffStatuses))]
					}
					if rng.IntN(2) == 0 {
						buf.SetCell(rng.IntN(cols), rng.IntN(rows), randCell(rng))
					}
				}
				inc.mu.Lock()
				used := inc.drawIncremental(buf, state, at)
				inc.mu.Unlock()
				if used {
					incremental++
				}
				got := drawAt(t, inc, buf, state, at)
				want := drawFresh(ref, buf, state, at)

				if got.Rect != want.Rect || !bytes.Equal(got.Pix, want.Pix) {
					diff := 0
					for i := range want.Pix {
						if i >= len(got.Pix) || got.Pix[i] != want.Pix[i] {
							diff++
						}
					}
					t.Fatalf("step %d: incremental draw differs from full repaint in %d bytes", step, diff)
				}
			}
			if incremental < tc.minIncr {
				t.Fatalf("only %d of %d draws were incremental, want at least %d", incremental, steps, tc.minIncr)
			}
		})
	}
}

// TestDrawIncrementalOnlyBand checks that Draw leaves rows far from the
// change alone, and falls back to a full repaint when it must.
func TestDrawIncrementalOnlyBand(t *testing.T) {
	t.Parallel()

	const cols, rows = 20, 12
	d := MustNew(WithScale(scaleFor(9)))
	t.Cleanup(func() { _ = d.Close() })
	buf := screen.NewBuffer(cols, rows)
	img := drawNRGBA(t, d, buf)

	// Poison in the renderer's own frame shows which rows Draw painted again.
	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	cell := d.Layout(cols, rows).Cell
	far := image.Rect(0, 8*cell.Y, img.Bounds().Dx(), 9*cell.Y)
	for y := far.Min.Y; y < far.Max.Y; y++ {
		for x := far.Min.X; x < far.Max.X; x++ {
			d.frame.SetNRGBA(x, y, poison)
		}
	}

	buf.SetCell(3, 1, &screen.Cell{Content: "x", Width: 1})
	if got := drawNRGBA(t, d, buf); got.NRGBAAt(far.Min.X, far.Min.Y) != poison {
		t.Fatal("a change on row 1 repainted row 8")
	}

	buf.SetCell(3, 8, &screen.Cell{Content: "y", Width: 1})
	if got := drawNRGBA(t, d, buf); got.NRGBAAt(far.Min.X, far.Min.Y) == poison {
		t.Fatal("a change on row 8 did not repaint row 8")
	}

	// A rebuild clears frameReady, which forces a full repaint.
	d.mu.Lock()
	d.frameReady = false
	d.frame.SetNRGBA(0, 0, poison)
	d.mu.Unlock()
	buf.SetCell(3, 1, &screen.Cell{Content: "z", Width: 1})
	if drawNRGBA(t, d, buf).NRGBAAt(0, 0) == poison {
		t.Fatal("full repaint did not repaint the margin")
	}
}

// TestDrawIncrementalNonComparableColor checks that cells holding a color
// type that Cell == panics on fall back to a full repaint.
func TestDrawIncrementalNonComparableColor(t *testing.T) {
	t.Parallel()

	d, ref := MustNew(WithScale(scaleFor(9))), MustNew(WithScale(scaleFor(9)))
	t.Cleanup(func() { _ = d.Close(); _ = ref.Close() })
	buf := screen.NewBuffer(10, 4)
	buf.SetCell(1, 1, &screen.Cell{Content: "a", Width: 1, Style: screen.Style{Fg: sliceColor{200}}})
	drawNRGBA(t, d, buf)
	buf.SetCell(5, 2, &screen.Cell{Content: "b", Width: 1})

	got := drawNRGBA(t, d, buf)
	want := drawFresh(ref, buf, focused, 0)
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("draw with non-comparable colors differs from full repaint")
	}
}

// TestDrawIncrementalFallsBack checks the cases that depend on time or on
// state outside the cells, which always take a full repaint.
func TestDrawIncrementalFallsBack(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		opts  []Option
		state screen.State
		cell  screen.Cell
	}{
		"cursor-blink": {[]Option{WithCursorBlinkSpeed(500 * time.Millisecond)}, screen.State{Cursor: screen.Cursor{Visible: true, Blink: true}}, screen.Cell{Content: "a", Width: 1}},
		"cell-blink":   {nil, screen.State{}, screen.Cell{Content: "a", Width: 1, Style: screen.Style{Attrs: screen.AttrBlink}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := MustNew(append([]Option{WithScale(scaleFor(9))}, tc.opts...)...)
			t.Cleanup(func() { _ = d.Close() })
			buf := screen.NewBuffer(10, 4)
			drawAt(t, d, buf, tc.state, 0)
			buf.SetCell(2, 2, &tc.cell)

			d.mu.Lock()
			defer d.mu.Unlock()
			if d.drawIncremental(buf, tc.state, 0) {
				t.Fatal("drawIncremental should refuse")
			}
		})
	}
}
