// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Boot timeline, in seconds since the program started.
const (
	bootLogoEnd    = 0.9 // the wordmark finishes sweeping in
	bootChecksFrom = 1.0 // the first check starts
	bootCheckEach  = 0.5 // each check takes this long
	bootFadeFrom   = 3.55
	bootEnd        = 3.95 // the dashboard starts revealing
)

var bootChecks = []struct{ label, detail string }{
	{"connecting to prod-us-east", "14ms"},
	{"syncing service registry", "7 services"},
	{"streaming telemetry", "3 feeds"},
	{"verifying release signatures", "ed25519"},
}

func bootChecksEnd() float64 { return bootChecksFrom + float64(len(bootChecks))*bootCheckEach }

// bootProgress is the boot progress reported to the terminal (OSC 9;4): an
// indeterminate sweep while the wordmark draws, then the checks' progress.
func bootProgress(t float64) *tea.ProgressBar {
	if t < bootChecksFrom {
		return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, int(bootPct(t)*100))
}

func bootPct(t float64) float64 {
	return min(max((t-bootChecksFrom)/(bootChecksEnd()-bootChecksFrom), 0), 1)
}

// Letters of the wordmark as 8x10 pixel bitmaps. Two pixel rows share a
// cell, drawn with half blocks, so the pixels come out close to square.
//
//nolint:goconst // Bitmap rows.
var pixelFont = map[rune][]string{
	'A': {
		"..####..",
		".######.",
		"##....##",
		"##....##",
		"########",
		"########",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
	},
	'U': {
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"###..###",
		".######.",
		"..####..",
	},
	'R': {
		"######..",
		"#######.",
		"##....##",
		"##....##",
		"#######.",
		"######..",
		"##..##..",
		"##...##.",
		"##....##",
		"##....##",
	},
	'O': {
		"..####..",
		".######.",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		".######.",
		"..####..",
	},
}

// wordmark is the brand name as a pixel grid, true where a letter is.
var wordmark = func() [][]bool {
	const gap = 2
	rows := make([][]bool, len(pixelFont['A']))
	for i, letter := range "AURORA" {
		for y, row := range pixelFont[letter] {
			if i > 0 {
				rows[y] = append(rows[y], make([]bool, gap)...)
			}
			for _, px := range row {
				rows[y] = append(rows[y], px == '#')
			}
		}
	}
	return rows
}()

const (
	pxEmpty = iota
	pxShadow
	pxLetter
)

// wordmarkView draws the wordmark t seconds in. A diagonal sweep reveals it
// with a bright leading edge, a drop shadow sits behind it, and the gradient
// keeps drifting once it is drawn.
func wordmarkView(p palette, t float64) []string {
	h, w := len(wordmark), len(wordmark[0])+1
	sweep := t/bootLogoEnd*float64(w+h+8) - 6

	pixel := func(x, y int) (int, color.Color) {
		at := func(x, y int) bool {
			return y >= 0 && y < h && x >= 0 && x < len(wordmark[y]) && wordmark[y][x]
		}
		d := sweep - float64(x+y)
		if d < 0 {
			return pxEmpty, nil
		}
		c := p.gradient(mirror(float64(x)/float64(w) + t*0.12))
		switch {
		case at(x, y):
			if d < 4 {
				c = ramp(1-d/4, 3, c, p.bright)
			}
			return pxLetter, c
		case at(x-1, y-1):
			return pxShadow, mix(p.accent[1], p.bg, 0.8)
		}
		return pxEmpty, nil
	}

	lines := make([]string, 0, (h+2)/2)
	for y := 0; y < h+1; y += 2 {
		var sb strings.Builder
		for x := range w {
			top, ct := pixel(x, y)
			bot, cb := pixel(x, y+1)
			switch {
			case top == pxEmpty && bot == pxEmpty:
				sb.WriteByte(' ')
			case top == pxEmpty:
				paint(&sb, cb, "▄")
			case bot == pxEmpty:
				paint(&sb, ct, "▀")
			default:
				sb.WriteString(bgSeq(cb))
				paint(&sb, ct, "▀")
				sb.WriteString(bgReset)
			}
		}
		lines = append(lines, sb.String())
	}
	return lines
}

// bootView draws the boot screen t seconds in, centered in w x h.
func bootView(st *styles, t float64, w, h int) string {
	p := st.pal
	var lines []string

	lines = append(lines, wordmarkView(p, t)...)
	lines = append(lines, "")

	// Tagline, typed out letter by letter.
	tagline := []rune("D E P L O Y   C O N T R O L   R O O M")
	n := min(max(int((t-0.55)*90), 0), len(tagline))
	lines = append(lines, st.muted.Render(string(tagline[:n])), "", "")

	// Checks.
	const listW = 46
	for i, c := range bootChecks {
		start := bootChecksFrom + float64(i)*bootCheckEach
		var line string
		switch {
		case t < start:
			line = st.subtle.Render("·  " + c.label)
		case t < start+bootCheckEach:
			line = st.accent.Render(spinnerFrame(t)) + "  " + st.text.Render(c.label)
		default:
			line = spread(st.ok.Render("✓")+"  "+st.muted.Render(c.label), st.subtle.Render(c.detail), listW)
		}
		lines = append(lines, fit(line, listW))
	}

	// Progress.
	pct := bootPct(t)
	status := spread(st.muted.Render("booting"), st.bright.Render(fmt.Sprintf("%3.0f%%", pct*100)), listW)
	if t >= bootChecksEnd()+0.1 {
		status = center(st.ok.Render("●")+" "+st.text.Render("all systems nominal"), listW)
	}
	lines = append(lines, "", meter(p, pct, listW), status)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}
