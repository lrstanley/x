// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/bloom/anim"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/bloom/fx"
)

// The benchmarks lay out and draw a dashboard on a 200×60 terminal: a
// header, a collapsible sidebar of 30 styled items, four stat cards, a
// scrolled list of 200 rows, a log panel, and a status bar. At 120 frames a
// second a frame has 8.3ms; the layout engine's share should stay a small
// part of that, leaving the rest to the application.

const benchW, benchH = 200, 60

type benchView struct{ s string }

func (v benchView) View() string { return v.s }

var (
	benchBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#5f87af"))
	benchItem     = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0c0c0"))
	benchSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#005f87")).Bold(true)
	benchHeader   = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#87afd7")).Bold(true)
	benchList     = benchListView()
)

func benchListView() benchView {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "\x1b[38;5;%dm%04d\x1b[m  service-%02d  \x1b[1mok\x1b[m  %s\n", 16+i%200, i, i%40, strings.Repeat("·", 40+i%30))
	}
	return benchView{s: strings.TrimSuffix(b.String(), "\n")}
}

func benchLog(frame int) benchView {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "\x1b[2m%06d\x1b[m request served in %dms ✓\n", frame+i, (frame+i)%97)
	}
	return benchView{s: b.String()}
}

// benchTree builds the dashboard for a frame: selected picks the sidebar
// item, frame changes the log, and dialog, when not negative, is the
// progress of a dialog fading in over a dimmed backdrop.
func benchTree(selected, frame int, dialog float64) Child {
	items := make([]Child, 0, 31)
	for i := range 30 {
		st := benchItem
		if i == selected {
			st = benchSelected
		}
		items = append(items, Text(fmt.Sprintf(" • item %02d", i)).Style(st).ID(i))
	}
	items = append(items, Space())

	cards := make([]Child, 0, 4)
	for i := range 4 {
		cards = append(cards, Fill(Box(benchBorder, Text(fmt.Sprintf("metric %d\n%d req/s\n▁▂▃▅▇", i, 1000+i*37)))))
	}

	base := Column(
		Text("dashboard").Style(benchHeader),
		Fill(Row(
			Fixed(28, Box(benchBorder, Column(items...))).ID("sidebar").Collapsible(1),
			Fill(Column(
				Row(cards...).Gap(1),
				Fill(Box(benchBorder, Scroll(benchList, selected*3))),
			)).Min(60),
			Fixed(48, Box(benchBorder, benchLog(frame))).ID("log").Collapsible(2),
		).Gap(1)),
		Text(fmt.Sprintf("frame %d", frame)).Style(benchHeader),
	)
	if dialog < 0 {
		return base
	}
	return Overlay(base,
		Layer(Box(benchBorder.Padding(1, 2), Text("Deploy service-07?\n\n[ yes ]  [ no ]"))).
			ID("dialog").
			Modal().
			Backdrop(fx.Dim{Amount: 0.6}).
			Shadow(2, 1, fx.Dim{Amount: 0.8}).
			Transition(dialog, fx.Fade{}, fx.Slide{From: fx.Bottom}),
	)
}

// BenchmarkSolveNested lays out rows and columns nested 16 deep.
func BenchmarkSolveNested(b *testing.B) {
	tree := nested(16, Text("abcdefghij"))
	b.ReportAllocs()
	for b.Loop() {
		Solve(tree, benchW, benchH)
	}
}

func BenchmarkSolve(b *testing.B) {
	tree := benchTree(3, 0, -1)
	b.ReportAllocs()
	for b.Loop() {
		Solve(tree, benchW, benchH)
	}
}

// BenchmarkRenderUnchanged draws the same frame over and over, which is
// laid out but not composited again.
func BenchmarkRenderUnchanged(b *testing.B) {
	r := New(WithColors(color.White, color.Black))
	tree := benchTree(3, 0, -1)
	b.ReportAllocs()
	for b.Loop() {
		r.View(benchW, benchH, tree)
	}
}

// BenchmarkRenderChanged draws a frame where the selection, the scroll, the
// log, and the status bar change every time, as when a key is held down.
func BenchmarkRenderChanged(b *testing.B) {
	r := New(WithColors(color.White, color.Black))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		r.View(benchW, benchH, benchTree(i%30, i, -1))
	}
}

// BenchmarkRenderDialog draws a dialog sliding and fading in over a dimmed
// backdrop with a shadow, with the progress changing every frame.
func BenchmarkRenderDialog(b *testing.B) {
	r := New(WithColors(color.White, color.Black))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		r.View(benchW, benchH, benchTree(3, 0, float64(i%60)/60))
	}
}

// BenchmarkFrameRoundTrip is BenchmarkRenderChanged plus what Bubble Tea
// does with the result: parsing the frame's string back into cells.
func BenchmarkFrameRoundTrip(b *testing.B) {
	r := New(WithColors(color.White, color.Black))
	scr := uv.NewScreenBuffer(benchW, benchH)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		v := r.View(benchW, benchH, benchTree(i%30, i, -1))
		uv.NewStyledString(v.Content).Draw(scr, scr.Bounds())
	}
}

// BenchmarkRenderReflow draws several nodes moving and resizing at 200×60.
func BenchmarkRenderReflow(b *testing.B) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	tree := func(p int) Child {
		return Row(
			Percent(p, Box(benchBorder, Text("sidebar"))).ID("sidebar").Animate(anim.Spring(14, 0.8)),
			Fill(benchTree(3, 0, -1)).ID("main").Animate(),
			Percent(30-p/2, Box(benchBorder, Text("details"))).ID("details").Animate(),
		)
	}
	r.View(benchW, benchH, tree(10))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		now = now.Add(time.Second / 120)
		r.View(benchW, benchH, tree(10+(i/30)%3*10))
	}
}
