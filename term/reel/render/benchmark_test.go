// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"fmt"
	"image/color"
	"io"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	benchmarkTermCols    = 120
	benchmarkTermRows    = 30
	benchmarkFontSizePt  = 40.0
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
			// Leave 12 of 25 cells blank, so 52% are filled.
			if (x*31+y*17)%25 >= 13 {
				continue
			}
			style := vt.Style{Fg: fg, Bg: bg}
			if x&1 == 0 {
				style.Fg = color.NRGBA{R: 0x80, G: 0xc0, B: 0xff, A: 0xff}
			}
			if y&1 == 0 {
				style.Bg = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}
			}
			scr.SetCell(x, y, &vt.Cell{
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

// BenchmarkDraw draws a whole frame: the screen is not a [vt.Buffer], so
// the renderer cannot tell what changed and paints it all.
func BenchmarkDraw(b *testing.B) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithScale(scaleFor(benchmarkFontSizePt)))
	frame := newImage(d, scr)
	for range benchmarkWarmupIters {
		d.Draw(frame, scr, focused, 0)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.Draw(frame, scr, focused, 0)
	}
}

func BenchmarkRenderAndExportFrame(b *testing.B) {
	scr := benchmarkTerminalScreen()
	d := MustNew(WithScale(scaleFor(benchmarkFontSizePt)))
	enc, err := sink.NewGIF(io.Discard, sink.WithOptimize(sink.OptimizeFrames))
	if err != nil {
		b.Fatal(err)
	}
	cursorVisible := false
	cursor := vt.Cell{
		Content: "_",
		Width:   1,
		Style:   vt.Style{Fg: color.NRGBA{R: 0xff, A: 0xff}, Bg: color.NRGBA{A: 0xff}},
	}
	blank := vt.EmptyCell.Clone()
	frame := newImage(d, scr)
	for range benchmarkWarmupIters {
		if cursorVisible {
			scr.SetCell(benchmarkCursorCol, benchmarkCursorRow, &cursor)
		} else {
			scr.SetCell(benchmarkCursorCol, benchmarkCursorRow, blank)
		}
		cursorVisible = !cursorVisible

		d.Draw(frame, scr, focused, 0)
		err = enc.WriteFrame(sink.Frame{Image: frame, Delay: time.Millisecond})
		if err != nil {
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

		d.Draw(frame, scr, focused, 0)
		err = enc.WriteFrame(sink.Frame{Image: frame, Delay: time.Millisecond})
		if err != nil {
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
			scr.SetCell(x, y, &vt.Cell{
				Content: string(glyphs[(y*cols+x)%len(glyphs)]),
				Width:   1,
				Style:   vt.Style{Fg: fg, Bg: bg},
			})
		}
	}

	d := MustNew(WithScale(scaleFor(benchmarkFontSizePt)), WithSystemFontFallback(false))
	frame := newImage(d, scr)
	for range benchmarkWarmupIters {
		d.Draw(frame, scr, focused, 0)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.Draw(frame, scr, focused, 0)
	}
}

// BenchmarkDrawIconSpans draws rows of Nerd Font icons, alternately followed
// by a space, which grows the icon into two cells, and by a letter.
func BenchmarkDrawIconSpans(b *testing.B) {
	const cols, rows = 40, 16
	icons := []rune("\uf059\uf084\uf0ac\uea61\uf015")
	scr := newTestScreen(cols, rows)
	style := vt.Style{Fg: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, Bg: color.NRGBA{A: 0xff}}
	for y := range rows {
		for x := 0; x+1 < cols; x += 2 {
			next := " "
			if (x/2+y)%2 == 0 {
				next = "x"
			}
			scr.SetCell(x, y, &vt.Cell{Content: string(icons[(x/2+y)%len(icons)]), Width: 1, Style: style})
			scr.SetCell(x+1, y, &vt.Cell{Content: next, Width: 1, Style: style})
		}
	}

	d := MustNew(WithScale(scaleFor(benchmarkFontSizePt)))
	frame := newImage(d, scr)
	for range benchmarkWarmupIters {
		d.Draw(frame, scr, focused, 0)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.Draw(frame, scr, focused, 0)
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
			scr.SetCell(x, y, &vt.Cell{
				Content: fmt.Sprintf("%c%d", rune(0x21+i%90), i),
				Width:   1,
				Style:   vt.Style{Fg: fg, Bg: bg},
			})
		}
	}

	d := MustNew(WithScale(scaleFor(benchmarkFontSizePt)))
	frame := newImage(d, scr)
	for range benchmarkWarmupIters {
		d.Draw(frame, scr, focused, 0)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.Draw(frame, scr, focused, 0)
	}
}

// BenchmarkDrawChrome measures the title bar's share of a full draw.
func BenchmarkDrawChrome(b *testing.B) {
	scr := benchmarkTerminalScreen()
	for _, chrome := range []bool{false, true} {
		b.Run(fmt.Sprintf("chrome=%t", chrome), func(b *testing.B) {
			d := MustNew(WithScale(scaleFor(benchmarkFontSizePt)), WithWindowChrome(chrome))
			st := vt.State{Title: "go test ./... — benchmark", Focused: true}
			frame := newImage(d, scr)
			for range benchmarkWarmupIters {
				d.Draw(frame, scr, st, 0)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				d.Draw(frame, scr, st, 0)
			}
		})
	}
}

// BenchmarkDrawTitleChange redraws through [Renderer.Draw] after only the
// title changes, like a program that updates its title while the screen is
// idle. Without chrome the title is not painted, so the cached frame is reused.
func BenchmarkDrawTitleChange(b *testing.B) {
	for _, chrome := range []bool{false, true} {
		b.Run(fmt.Sprintf("chrome=%t", chrome), func(b *testing.B) {
			scr := benchmarkTerminalScreen().Buffer
			d := MustNew(WithScale(scaleFor(14)), WithWindowChrome(chrome))
			st := vt.State{Focused: true}
			frame := newImage(d, scr)
			draw := func(i int) {
				st.Title = fmt.Sprintf("build %d%%", i%100)
				d.Draw(frame, scr, st, 0)
			}
			for i := range benchmarkWarmupIters {
				draw(i)
			}

			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			for b.Loop() {
				draw(i)
				i++
			}
		})
	}
}

// benchmarkStatusOpts are the options of a renderer that draws program
// status with an outline in the margin.
func benchmarkStatusOpts(size float64) []Option {
	return []Option{
		WithScale(scaleFor(size)), WithWindowChrome(true),
		WithMargin(36, color.NRGBA{R: 0x15, G: 0x16, B: 0x1c, A: 0xff}), WithBorderRadius(8),
	}
}

// benchmarkIndicatorStates are the indicator states the status benchmarks
// draw: each program status look, and an OSC 9;4 bar with no status.
var benchmarkIndicatorStates = []struct {
	name  string
	state vt.State
}{
	{"spinner", vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{
		{State: vt.ProgramWorking, Title: "Refactoring renderer", Message: "Reading render.go", Progress: -1},
		{ID: "build", State: vt.ProgramDone, Progress: -1},
	}}},
	{"blocked", vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{
		{State: vt.ProgramBlocked, Kind: vt.ProgramKindPermission, Title: "Edit render.go?", Message: "Apply 3 changes", Progress: -1},
	}}},
	{"progress", vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{
		{State: vt.ProgramWorking, Title: "Running tests", Message: "42 of 100 packages", Progress: 42},
	}}},
	{"done", vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{
		{State: vt.ProgramDone, Title: "Refactor complete", Message: "12 files changed", Progress: -1},
	}}},
	{"bar-only", vt.State{Focused: true, Title: "build", Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}},
}

// BenchmarkRepaint compares drawing a whole frame with repainting only the
// indicators, as a recording does to keep an animated indicator moving while
// the screen holds still. The offset moves 50ms per frame.
func BenchmarkRepaint(b *testing.B) {
	scr := benchmarkTerminalScreen()
	for _, tc := range benchmarkIndicatorStates {
		if tc.name == "done" {
			continue
		}
		for _, mode := range []string{"draw", "repaint"} {
			b.Run(tc.name+"/"+mode, func(b *testing.B) {
				var at time.Duration
				d := MustNew(benchmarkStatusOpts(benchmarkFontSizePt)...)
				frame := newImage(d, scr)
				d.Draw(frame, scr, tc.state, at)
				step := func() {
					at += 50 * time.Millisecond
					if mode == "draw" {
						d.Draw(frame, scr, tc.state, at)
					} else {
						d.Repaint(frame, scr, tc.state, at)
					}
				}
				for range benchmarkWarmupIters {
					step()
				}

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					step()
				}
			})
		}
	}
}

// BenchmarkDrawIncrementalStatus redraws an 80x24 screen through
// [Renderer.Draw] after one cell changes, with the offset moving 50ms per
// frame, for program status that is steady, that fills to a percent, and that
// breathes.
func BenchmarkDrawIncrementalStatus(b *testing.B) {
	for _, tc := range benchmarkIndicatorStates {
		if tc.name != "done" && tc.name != "blocked" && tc.name != "progress" {
			continue
		}
		b.Run(tc.name, func(b *testing.B) {
			var at time.Duration
			d := MustNew(benchmarkStatusOpts(14)...)
			scr := vt.NewBuffer(80, 24)
			frame := newImage(d, scr)
			step := func(i int) {
				at += 50 * time.Millisecond
				scr.SetCell(i%60, 12, &vt.Cell{Content: string(rune('a' + i%26)), Width: 1})
				d.Draw(frame, scr, tc.state, at)
			}
			for i := range benchmarkWarmupIters {
				step(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			for b.Loop() {
				step(i)
				i++
			}
		})
	}
}

// BenchmarkDrawStatus draws a whole frame with an animated program status.
func BenchmarkDrawStatus(b *testing.B) {
	scr := benchmarkTerminalScreen()
	var at time.Duration
	d := MustNew(benchmarkStatusOpts(benchmarkFontSizePt)...)
	st := benchmarkIndicatorStates[0].state
	frame := newImage(d, scr)
	step := func() {
		at += 50 * time.Millisecond
		d.Draw(frame, scr, st, at)
	}
	for range benchmarkWarmupIters {
		step()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		step()
	}
}

// BenchmarkDrawRounded renders a small frame with margin and rounded corners so
// the rounded-corner mask is a measurable share of the draw.
func BenchmarkDrawRounded(b *testing.B) {
	for _, radius := range []int{12, 64} {
		b.Run(fmt.Sprintf("radius=%d", radius), func(b *testing.B) {
			scr := newTestScreen(20, 4)
			d := MustNew(
				WithBorderRadius(radius),
				WithMargin(8, color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}),
			)
			frame := newImage(d, scr)
			for range benchmarkWarmupIters {
				d.Draw(frame, scr, focused, 0)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				d.Draw(frame, scr, focused, 0)
			}
		})
	}
}

// BenchmarkDrawIncremental redraws a screen through [Renderer.Draw] after one
// cell changes and the cursor moves to it, like a typed character.
func BenchmarkDrawIncremental(b *testing.B) {
	sparse := func(cols, rows int) *vt.Buffer {
		scr := vt.NewBuffer(cols, rows)
		for y := 0; y < rows; y += 3 {
			for x, r := range "user@host:~/src/project$ make test && go vet ./..." {
				scr.SetCell(x, y, &vt.Cell{Content: string(r), Width: 1})
			}
		}
		return scr
	}
	cases := []struct {
		name string
		scr  *vt.Buffer
		opts []Option
	}{
		{"dense-120x30-40pt", benchmarkTerminalScreen().Buffer, []Option{WithScale(scaleFor(benchmarkFontSizePt))}},
		{"sparse-80x24", sparse(80, 24), nil},
		{"sparse-240x72", sparse(240, 72), nil},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			d := MustNew(tc.opts...)
			rows := tc.scr.Height()
			st := vt.State{Focused: true, Cursor: vt.Cursor{Visible: true, Y: rows / 2}}
			frame := newImage(d, tc.scr)
			draw := func(i int) {
				tc.scr.SetCell(i%60, rows/2, &vt.Cell{Content: string(rune('a' + i%26)), Width: 1})
				st.Cursor.X = i % 60
				d.Draw(frame, tc.scr, st, 0)
			}
			for i := range benchmarkWarmupIters {
				draw(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			i := 0
			for b.Loop() {
				draw(i)
				i++
			}
		})
	}
}
