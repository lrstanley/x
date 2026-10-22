// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/lrstanley/x/term/still/export"
	"github.com/lrstanley/x/term/vt/screen"
)

const (
	benchmarkTermCols    = 120
	benchmarkTermRows    = 30
	benchmarkFontSizePt  = Pt(40)
	renderExportBudget   = 10 * time.Millisecond
	benchmarkCursorCol   = benchmarkTermCols / 2
	benchmarkCursorRow   = benchmarkTermRows / 2
	benchmarkWarmupIters = 3 // stabilizes sync.Pool and coverage buffer capacity
)

// benchmarkTerminalScreen returns a 120×30 screen with ~52% of cells filled
// with varied glyphs and styles, simulating a content-heavy TUI while staying
// within the combined render+export budget.
func benchmarkTerminalScreen() testScreen {
	scr := newTestScreen(benchmarkTermCols, benchmarkTermRows)
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	for y := range benchmarkTermRows {
		for x := range benchmarkTermCols {
			if (x*31+y*17)%25 >= 13 { // 13/25 = 52% fill
				continue
			}
			style := screen.Style{Fg: fg, Bg: bg}
			if x&1 == 0 {
				style.Fg = color.NRGBA{R: 0x80, G: 0xc0, B: 0xff, A: 0xff}
			}
			if y&1 == 0 {
				style.Bg = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}
			}
			scr.SetCell(x, y, &screen.Cell{
				Content: string(rune('a' + (x+y)%26)),
				Width:   1,
				Style:   style,
			})
		}
	}
	return scr
}

// assertBenchmarkBudget fails b when average elapsed/op exceeds budget unless
// -short is set.
func assertBenchmarkBudget(b *testing.B, budget time.Duration) {
	b.Helper()
	if testing.Short() {
		return
	}
	perOp := b.Elapsed() / time.Duration(b.N)
	if perOp > budget {
		b.Fatalf("elapsed/op %v exceeds budget %v (total %v for %d ops)", perOp, budget, b.Elapsed(), b.N)
	}
}

func TestBenchmarkDrawAllocsPerOp(t *testing.T) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithFontSizePt(benchmarkFontSizePt))
	frame := image.NewNRGBA(d.Bounds(scr))
	for range benchmarkWarmupIters {
		d.DrawInto(frame, frame.Bounds(), scr)
	}
	n := testing.AllocsPerRun(20, func() {
		d.DrawInto(frame, frame.Bounds(), scr)
	})
	t.Logf("DrawInto allocs/op = %v", n)
}

func BenchmarkDraw(b *testing.B) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithFontSizePt(benchmarkFontSizePt))
	frame := image.NewNRGBA(d.Bounds(scr))
	for range benchmarkWarmupIters {
		d.DrawInto(frame, frame.Bounds(), scr)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.DrawInto(frame, frame.Bounds(), scr)
	}
}

func BenchmarkDrawInto(b *testing.B) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithFontSizePt(benchmarkFontSizePt))
	frame := image.NewNRGBA(d.Bounds(scr))
	area := frame.Bounds()
	for range benchmarkWarmupIters {
		d.DrawInto(frame, area, scr)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.DrawInto(frame, area, scr)
	}
}

func BenchmarkRenderAndExportFrame(b *testing.B) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithFontSizePt(benchmarkFontSizePt))
	rec := export.NewPalettizedRecorder()
	cursorVisible := false
	cursor := screen.Cell{
		Content: "_",
		Width:   1,
		Style:   screen.Style{Fg: color.NRGBA{R: 0xff, A: 0xff}, Bg: color.NRGBA{A: 0xff}},
	}
	blank := screen.EmptyCell.Clone()
	frame := image.NewNRGBA(d.Bounds(scr))
	for range benchmarkWarmupIters {
		if cursorVisible {
			scr.SetCell(benchmarkCursorCol, benchmarkCursorRow, &cursor)
		} else {
			scr.SetCell(benchmarkCursorCol, benchmarkCursorRow, blank)
		}
		cursorVisible = !cursorVisible

		d.DrawInto(frame, frame.Bounds(), scr)
		if err := rec.AddFrame(frame, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if cursorVisible {
			scr.SetCell(benchmarkCursorCol, benchmarkCursorRow, &cursor)
		} else {
			scr.SetCell(benchmarkCursorCol, benchmarkCursorRow, blank)
		}
		cursorVisible = !cursorVisible

		d.DrawInto(frame, frame.Bounds(), scr)
		if err := rec.AddFrame(frame, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	assertBenchmarkBudget(b, renderExportBudget)
}

// BenchmarkDrawSymbolGlyphs draws a screen of symbols from the bundled
// fallback font and the Symbols Nerd Font, which take the fitted and scaled
// fallback path rather than the grid path.
func BenchmarkDrawSymbolGlyphs(b *testing.B) {
	const cols, rows = 40, 16
	glyphs := []rune("★✔☀♪⚙◐◢⇄➤☐")
	scr := newTestScreen(cols, rows)
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	for y := range rows {
		for x := range cols {
			scr.SetCell(x, y, &screen.Cell{
				Content: string(glyphs[(y*cols+x)%len(glyphs)]),
				Width:   1,
				Style:   screen.Style{Fg: fg, Bg: bg},
			})
		}
	}

	d := MustNew(WithFontSizePt(benchmarkFontSizePt), WithSystemFontFallback(false))
	frame := image.NewNRGBA(d.Bounds(scr))
	for range benchmarkWarmupIters {
		d.DrawInto(frame, frame.Bounds(), scr)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.DrawInto(frame, frame.Bounds(), scr)
	}
}

// BenchmarkDrawManyGlyphs draws a screen with more unique graphemes than the
// old 128-entry mask cache could hold, which is the case the atlas budget is
// sized for.
func BenchmarkDrawManyGlyphs(b *testing.B) {
	const cols, rows = 40, 16
	scr := newTestScreen(cols, rows)
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	for y := range rows {
		for x := range cols {
			i := y*cols + x
			scr.SetCell(x, y, &screen.Cell{
				Content: fmt.Sprintf("%c%d", rune(0x21+i%90), i),
				Width:   1,
				Style:   screen.Style{Fg: fg, Bg: bg},
			})
		}
	}

	d := MustNew(WithFontSizePt(benchmarkFontSizePt))
	frame := image.NewNRGBA(d.Bounds(scr))
	for range benchmarkWarmupIters {
		d.DrawInto(frame, frame.Bounds(), scr)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.DrawInto(frame, frame.Bounds(), scr)
	}
}
