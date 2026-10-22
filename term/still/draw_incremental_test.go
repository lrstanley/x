// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/lrstanley/x/term/vt/screen"
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
		state   EmulatorState
		minIncr int
	}{
		{"plain", nil, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"margin-padding", []Option{WithMargin(7, color.NRGBA{R: 200, A: 255}), WithPadding(5)}, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"radius", []Option{WithMargin(6, color.NRGBA{B: 90, A: 255}), WithPadding(3), WithBorderRadius(14)}, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"radius-large", []Option{WithBorderRadius(60)}, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"scrollbar", []Option{WithScrollbar(true), WithPadding(2)}, EmulatorState{Focused: true, CursorVisible: true, ScrollbackCount: 40}, 30},
		{"opacity", []Option{WithBackgroundOpacity(0.5)}, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"opacity-cells", []Option{WithBackgroundOpacity(0.5), WithBackgroundOpacityCells(true), WithMargin(4, nil)}, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"unfocused", []Option{WithFocusDimming(0.4), WithMargin(5, color.NRGBA{G: 120, A: 255}), WithPadding(4)}, EmulatorState{CursorVisible: true}, 30},
		{"big-font", []Option{WithFontSizePt(26), WithPadding(3)}, EmulatorState{Focused: true, CursorVisible: true}, 30},
		{"everything", []Option{
			WithMargin(6, color.NRGBA{R: 30, G: 60, B: 90, A: 200}), WithPadding(4), WithBorderRadius(12),
			WithScrollbar(true), WithBackgroundOpacity(0.5), WithFocusDimming(0.35), WithFaintFactor(0.4),
		}, EmulatorState{CursorVisible: true, ScrollbackCount: 25}, 30},
	}

	const cols, rows, steps = 24, 12, 80

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := append([]Option{WithFontSizePt(9)}, tc.opts...)
			inc, ref := MustNew(opts...), MustNew(opts...)
			t.Cleanup(func() { _ = inc.Close(); _ = ref.Close() })

			rng := rand.New(rand.NewPCG(7, uint64(len(tc.name))))
			buf := screen.NewBuffer(cols, rows)
			state := tc.state
			state.CursorStyle = screen.CursorBlock
			incremental := 0

			for step := range steps {
				switch step % 8 {
				case 0: // typing: a run of cells on one row
					y, x := rng.IntN(rows), rng.IntN(cols-6)
					for i := range 1 + rng.IntN(5) {
						buf.SetCell(x+i, y, randCell(rng))
					}
					state.CursorX, state.CursorY = x+5, y
				case 1: // delete a cell
					buf.SetCell(rng.IntN(cols), rng.IntN(rows), nil)
				case 2: // cursor-only move (sometimes off the grid) and style change
					state.CursorX, state.CursorY = rng.IntN(cols), rng.IntN(rows)
					if rng.IntN(4) == 0 {
						state.CursorY = []int{-3, rows + 5, math.MaxInt}[rng.IntN(3)]
					}
					state.CursorStyle = screen.CursorShape(rng.IntN(4))
				case 3: // colour change on one cell
					buf.SetCell(rng.IntN(cols), rng.IntN(rows), randCell(rng))
				case 4: // edits at the first and last rows (margin, padding, corners)
					buf.SetCell(rng.IntN(cols), 0, randCell(rng))
					if rng.IntN(2) == 0 {
						buf.SetCell(rng.IntN(cols), rows-1, randCell(rng))
					}
				case 5: // nothing changes
				case 6: // cursor visibility toggle
					state.CursorVisible = !state.CursorVisible
				case 7: // a few scattered edits
					for range 1 + rng.IntN(4) {
						buf.SetCell(rng.IntN(cols), rng.IntN(rows), randCell(rng))
					}
				}
				inc.UpdateEmulatorState(&state)
				ref.UpdateEmulatorState(&state)

				inc.mu.Lock()
				used := inc.drawIncremental(buf)
				inc.mu.Unlock()
				if used {
					incremental++
				}
				got := drawNRGBA(t, inc, buf)

				ref.mu.Lock()
				ref.frameReady = false
				ref.mu.Unlock()
				want := image.NewNRGBA(ref.Bounds(buf))
				ref.DrawInto(want, want.Bounds(), buf)

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
	d := MustNew(WithFontSizePt(9))
	t.Cleanup(func() { _ = d.Close() })
	buf := screen.NewBuffer(cols, rows)
	d.UpdateEmulatorState(&EmulatorState{Focused: true})
	img := drawNRGBA(t, d, buf)

	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	cell := d.CellSize()
	far := image.Rect(0, 8*cell.Y, img.Bounds().Dx(), 9*cell.Y)
	for y := far.Min.Y; y < far.Max.Y; y++ {
		for x := far.Min.X; x < far.Max.X; x++ {
			img.SetNRGBA(x, y, poison)
		}
	}

	buf.SetCell(3, 1, &screen.Cell{Content: "x", Width: 1})
	if got := drawNRGBA(t, d, buf); got != img || got.NRGBAAt(far.Min.X, far.Min.Y) != poison {
		t.Fatal("a change on row 1 repainted row 8")
	}

	buf.SetCell(3, 8, &screen.Cell{Content: "y", Width: 1})
	if got := drawNRGBA(t, d, buf); got.NRGBAAt(far.Min.X, far.Min.Y) == poison {
		t.Fatal("a change on row 8 did not repaint row 8")
	}

	// A rebuild clears frameReady, which forces a full repaint.
	d.mu.Lock()
	d.frameReady = false
	d.mu.Unlock()
	img.SetNRGBA(0, 0, poison)
	buf.SetCell(3, 1, &screen.Cell{Content: "z", Width: 1})
	if drawNRGBA(t, d, buf).NRGBAAt(0, 0) == poison {
		t.Fatal("full repaint did not repaint the margin")
	}
}

// TestDrawIncrementalNonComparableColor checks that cells holding a color
// type that Cell == panics on fall back to a full repaint.
func TestDrawIncrementalNonComparableColor(t *testing.T) {
	t.Parallel()

	d, ref := MustNew(WithFontSizePt(9)), MustNew(WithFontSizePt(9))
	t.Cleanup(func() { _ = d.Close(); _ = ref.Close() })
	buf := screen.NewBuffer(10, 4)
	buf.SetCell(1, 1, &screen.Cell{Content: "a", Width: 1, Style: screen.Style{Fg: sliceColor{200}}})
	drawNRGBA(t, d, buf)
	buf.SetCell(5, 2, &screen.Cell{Content: "b", Width: 1})

	got := drawNRGBA(t, d, buf)
	want := image.NewNRGBA(ref.Bounds(buf))
	ref.DrawInto(want, want.Bounds(), buf)
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
		state EmulatorState
		cell  screen.Cell
	}{
		"cursor-blink": {[]Option{WithCursorBlinkSpeed(500 * time.Millisecond)}, EmulatorState{CursorVisible: true, CursorBlink: true}, screen.Cell{Content: "a", Width: 1}},
		"cell-blink":   {nil, EmulatorState{}, screen.Cell{Content: "a", Width: 1, Style: screen.Style{Attrs: screen.AttrBlink}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := MustNew(append([]Option{WithFontSizePt(9)}, tc.opts...)...)
			t.Cleanup(func() { _ = d.Close() })
			buf := screen.NewBuffer(10, 4)
			d.UpdateEmulatorState(&tc.state)
			drawNRGBA(t, d, buf)
			buf.SetCell(2, 2, &tc.cell)

			d.mu.Lock()
			defer d.mu.Unlock()
			if d.drawIncremental(buf) {
				t.Fatal("drawIncremental should refuse")
			}
		})
	}
}
