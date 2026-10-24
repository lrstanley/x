// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
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

// BenchmarkDrawChrome measures the title bar's share of a full draw.
func BenchmarkDrawChrome(b *testing.B) {
	scr := benchmarkTerminalScreen()
	for _, chrome := range []bool{false, true} {
		b.Run(fmt.Sprintf("chrome=%t", chrome), func(b *testing.B) {
			d := MustNew(WithFontSizePt(benchmarkFontSizePt), WithWindowChrome(chrome))
			d.UpdateEmulatorState(&EmulatorState{Title: "go test ./... — benchmark", Focused: true})
			frame := image.NewNRGBA(d.Bounds(scr))
			for range benchmarkWarmupIters {
				d.DrawInto(frame, frame.Bounds(), scr)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				d.DrawInto(frame, frame.Bounds(), scr)
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
			d := MustNew(WithFontSizePt(Pt(14)), WithWindowChrome(chrome))
			state := EmulatorState{Focused: true}
			draw := func(i int) {
				state.Title = fmt.Sprintf("build %d%%", i%100)
				d.UpdateEmulatorState(&state)
				d.Draw(scr)
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
// status with an outline in the margin, at clock *at.
func benchmarkStatusOpts(size Pt, at *time.Time) []Option {
	return []Option{
		WithFontSizePt(size), WithWindowChrome(true),
		WithMargin(36, color.NRGBA{R: 0x15, G: 0x16, B: 0x1c, A: 0xff}), WithBorderRadius(8),
		WithClock(func() time.Time { return *at }),
	}
}

// benchmarkIndicatorStates are the indicator states the status benchmarks
// draw: each program status look, and an OSC 9;4 bar with no status.
var benchmarkIndicatorStates = []struct {
	name  string
	state EmulatorState
}{
	{"spinner", EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{
		{State: ProgramWorking, Title: "Refactoring renderer", Message: "Reading render.go", Progress: -1},
		{ID: "build", State: ProgramDone, Progress: -1},
	}}},
	{"blocked", EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{
		{State: ProgramBlocked, Kind: ProgramKindPermission, Title: "Edit render.go?", Message: "Apply 3 changes", Progress: -1},
	}}},
	{"progress", EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{
		{State: ProgramWorking, Title: "Running tests", Message: "42 of 100 packages", Progress: 42},
	}}},
	{"done", EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{
		{State: ProgramDone, Title: "Refactor complete", Message: "12 files changed", Progress: -1},
	}}},
	{"bar-only", EmulatorState{Focused: true, Title: "build", Progress: Progress{State: ProgressIndeterminate, Percent: -1}}},
}

// BenchmarkRepaintIndicators compares drawing a whole frame with repainting
// only the indicators, as a recording does to keep an animated indicator
// moving while the screen holds still. The clock moves 50ms per frame.
func BenchmarkRepaintIndicators(b *testing.B) {
	scr := benchmarkTerminalScreen()
	for _, tc := range benchmarkIndicatorStates {
		if tc.name == "done" {
			continue
		}
		for _, mode := range []string{"draw", "repaint"} {
			b.Run(tc.name+"/"+mode, func(b *testing.B) {
				at := time.Unix(1_000_000, 0)
				d := MustNew(benchmarkStatusOpts(benchmarkFontSizePt, &at)...)
				d.UpdateEmulatorState(&tc.state)
				frame := image.NewNRGBA(d.Bounds(scr))
				d.DrawInto(frame, frame.Bounds(), scr)
				step := func() {
					at = at.Add(50 * time.Millisecond)
					if mode == "draw" {
						d.DrawInto(frame, frame.Bounds(), scr)
					} else {
						d.RepaintIndicators(frame, scr)
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
// [Renderer.Draw] after one cell changes, with the clock moving 50ms per
// frame, for program status that is steady, that fills to a percent, and that
// breathes.
func BenchmarkDrawIncrementalStatus(b *testing.B) {
	for _, tc := range benchmarkIndicatorStates {
		if tc.name != "done" && tc.name != "blocked" && tc.name != "progress" {
			continue
		}
		b.Run(tc.name, func(b *testing.B) {
			at := time.Unix(1_000_000, 0)
			d := MustNew(benchmarkStatusOpts(14, &at)...)
			d.UpdateEmulatorState(&tc.state)
			scr := screen.NewBuffer(80, 24)
			step := func(i int) {
				at = at.Add(50 * time.Millisecond)
				scr.SetCell(i%60, 12, &screen.Cell{Content: string(rune('a' + i%26)), Width: 1})
				d.Draw(scr)
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

// BenchmarkDrawIntoStatus draws a whole frame with an animated program
// status into an NRGBA image, and into an RGBA image, which takes the
// generic pixel paths.
func BenchmarkDrawIntoStatus(b *testing.B) {
	scr := benchmarkTerminalScreen()
	for _, kind := range []string{"nrgba", "rgba"} {
		b.Run(kind, func(b *testing.B) {
			at := time.Unix(1_000_000, 0)
			d := MustNew(benchmarkStatusOpts(benchmarkFontSizePt, &at)...)
			d.UpdateEmulatorState(&benchmarkIndicatorStates[0].state)
			var frame draw.Image = image.NewNRGBA(d.Bounds(scr))
			if kind == "rgba" {
				frame = image.NewRGBA(d.Bounds(scr))
			}
			step := func() {
				at = at.Add(50 * time.Millisecond)
				d.DrawInto(frame, frame.Bounds(), scr)
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
			frame := image.NewNRGBA(d.Bounds(scr))
			for range benchmarkWarmupIters {
				d.DrawInto(frame, frame.Bounds(), scr)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				d.DrawInto(frame, frame.Bounds(), scr)
			}
		})
	}
}

// BenchmarkDrawIncremental redraws a screen through [Renderer.Draw] after one
// cell changes and the cursor moves to it, like a typed character.
func BenchmarkDrawIncremental(b *testing.B) {
	sparse := func(cols, rows int) *screen.Buffer {
		scr := screen.NewBuffer(cols, rows)
		for y := 0; y < rows; y += 3 {
			for x, r := range "user@host:~/src/project$ make test && go vet ./..." {
				scr.SetCell(x, y, &screen.Cell{Content: string(r), Width: 1})
			}
		}
		return scr
	}
	cases := []struct {
		name string
		scr  *screen.Buffer
		opts []Option
	}{
		{"dense-120x30-40pt", benchmarkTerminalScreen().Buffer, []Option{WithFontSizePt(benchmarkFontSizePt)}},
		{"sparse-80x24", sparse(80, 24), nil},
		{"sparse-240x72", sparse(240, 72), nil},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			d := MustNew(tc.opts...)
			rows := tc.scr.Height()
			state := EmulatorState{Focused: true, CursorVisible: true, CursorY: rows / 2}
			draw := func(i int) {
				tc.scr.SetCell(i%60, rows/2, &screen.Cell{Content: string(rune('a' + i%26)), Width: 1})
				state.CursorX = i % 60
				d.UpdateEmulatorState(&state)
				d.Draw(tc.scr)
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
