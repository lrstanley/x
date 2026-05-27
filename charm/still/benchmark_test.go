// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/export"
)

const (
	benchmarkTermCols   = 120
	benchmarkTermRows   = 30
	benchmarkFontSizePt = Pt(40)
	renderExportBudget  = 10 * time.Millisecond
	benchmarkCursorCol  = benchmarkTermCols / 2
	benchmarkCursorRow  = benchmarkTermRows / 2
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
			style := uv.Style{Fg: fg, Bg: bg}
			if x&1 == 0 {
				style.Fg = color.NRGBA{R: 0x80, G: 0xc0, B: 0xff, A: 0xff}
			}
			if y&1 == 0 {
				style.Bg = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}
			}
			scr.SetCell(x, y, &uv.Cell{
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

func BenchmarkRenderAndExportFrame(b *testing.B) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithFontSizePt(benchmarkFontSizePt))
	rec := export.NewFrameRecorder()
	cursorVisible := false
	cursor := uv.Cell{
		Content: "_",
		Width:   1,
		Style:   uv.Style{Fg: color.NRGBA{R: 0xff, A: 0xff}, Bg: color.NRGBA{A: 0xff}},
	}
	blank := uv.EmptyCell.Clone()
	frame := image.NewNRGBA(d.Bounds(scr))

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
