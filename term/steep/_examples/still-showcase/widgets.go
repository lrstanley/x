// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// fit pads or truncates s to exactly w cells.
func fit(s string, w int) string {
	sw := lipgloss.Width(s)
	switch {
	case sw > w:
		return ansi.Truncate(s, w, "…")
	case sw < w:
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// spread places left and right at either end of w cells.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(left+" "+right, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// center pads s on both sides to w cells.
func center(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return fit(s, w)
	}
	left := (w - sw) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-sw-left)
}

// panel is a rounded box with a title and optional meta text set into its top
// border. A focused panel draws its border along the accent gradient, rotated
// by phase, so the light appears to travel around it.
type panel struct {
	title   string
	meta    string
	focused bool
	phase   float64
}

// view draws the panel at an outer size of w x h around body, which is fitted
// to the inner size.
func (p panel) view(st *styles, w, h int, body []string) string {
	if w < 4 || h < 2 {
		return ""
	}
	perimeter := float64(2*(w-1) + 2*(h-1))
	borderColor := func(pos int) color.Color {
		if !p.focused {
			return st.pal.border
		}
		return st.pal.gradient(mirror(float64(pos)/perimeter + p.phase))
	}
	// Perimeter positions run clockwise from the top left corner.
	right := func(y int) int { return w - 1 + y }
	bottom := func(x int) int { return 2*(w-1) + (h - 1) - x }
	left := func(y int) int { return 2*(w-1) + 2*(h-1) - y }

	var sb strings.Builder
	inner := w - 2

	// Top border: ╭─ title ───── meta ─╮
	title := ""
	if p.title != "" {
		title = " " + st.label.Render(p.title) + " "
		if p.focused {
			title = " " + gradientText(st.pal, p.title, p.phase, true) + " "
		}
	}
	meta := ""
	if p.meta != "" {
		meta = " " + p.meta + " "
	}
	fill := inner - 1 - lipgloss.Width(title) - lipgloss.Width(meta) - 1
	if fill < 0 {
		meta, fill = "", inner-1-lipgloss.Width(title)
	}
	if fill < 0 {
		title, fill = "", inner
	}
	x := 0
	edge := func(s string) {
		for _, r := range s {
			paint(&sb, borderColor(x), string(r))
			x++
		}
	}
	edge("╭")
	if title != "" || meta != "" {
		edge("─")
		sb.WriteString(title)
		x += lipgloss.Width(title)
		edge(strings.Repeat("─", fill))
		sb.WriteString(meta)
		x += lipgloss.Width(meta)
		edge("─")
	} else {
		edge(strings.Repeat("─", inner))
	}
	edge("╮")
	sb.WriteByte('\n')

	for y := 1; y < h-1; y++ {
		line := ""
		if y-1 < len(body) {
			line = body[y-1]
		}
		paint(&sb, borderColor(left(y)), "│")
		sb.WriteString(fit(line, inner))
		paint(&sb, borderColor(right(y)), "│")
		sb.WriteByte('\n')
	}

	paint(&sb, borderColor(bottom(0)), "╰")
	for i := 1; i < w-1; i++ {
		paint(&sb, borderColor(bottom(i)), "─")
	}
	paint(&sb, borderColor(bottom(w-1)), "╯")
	return sb.String()
}

// spinnerFrame is the frame of a MiniDot spinner t seconds in.
func spinnerFrame(t float64) string {
	frames := spinner.MiniDot.Frames
	return frames[int(t*14)%len(frames)]
}

var eighths = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// barChart draws vals (0-1, newest last) as solid columns of w x h cells,
// shaded bottom to top along stops, over a faint dotted grid.
func barChart(p palette, vals []float64, w, h int, stops ...color.Color) []string {
	vals = tail(vals, w)
	pad := w - len(vals)
	lines := make([]string, h)
	for r := range h {
		var sb strings.Builder
		row := h - 1 - r // 0 at the bottom.
		c := ramp(float64(row)/float64(max(h-1, 1)), mixSteps, stops...)
		grid := row > 0 && row%2 == 0
		for x := range w {
			cell := 0
			if x >= pad {
				cell = min(max(int(vals[x-pad]*float64(h*8)+0.5)-row*8, 0), 8)
			}
			switch {
			case cell > 0:
				paint(&sb, c, eighths[cell])
			case grid && x%2 == 0:
				paint(&sb, p.border, "·")
			default:
				sb.WriteByte(' ')
			}
		}
		lines[r] = sb.String()
	}
	return lines
}

// brailleBits are the dot bits of a braille cell, by column and by row from
// the top.
var brailleBits = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// brailleArea draws vals (newest last) as a filled area of w x h cells using
// braille dots, two samples per cell, scaled to the range of the visible
// samples. The fill fades from color toward the background going down, and
// the top dot of every column is drawn brighter, so the outline reads as a
// line over the fill.
func brailleArea(p palette, vals []float64, w, h int, c color.Color) []string {
	vals = tail(vals, w*2)
	pad := w*2 - len(vals)
	lo, hi := valueRange(vals, 0.35)
	dots := h * 4
	level := func(col int) int {
		if col < pad {
			return 0
		}
		return min(max(int((vals[col-pad]-lo)/(hi-lo)*float64(dots)+0.5), 1), dots)
	}

	lines := make([]string, h)
	for r := range h {
		var sb strings.Builder
		row := h - 1 - r
		fill := mix(c, p.bg, 0.78-0.5*float64(row)/float64(max(h-1, 1)))
		edge := mix(c, p.bright, 0.3)
		for x := range w {
			var bits rune
			top := false
			for dc := range 2 {
				lv := level(x*2 + dc)
				for dr := range 4 {
					dot := row*4 + (3 - dr)
					if dot < lv {
						bits |= brailleBits[dc][dr]
						top = top || dot == lv-1
					}
				}
			}
			switch {
			case bits == 0:
				sb.WriteByte(' ')
			case top:
				paint(&sb, edge, string(0x2800+bits))
			default:
				paint(&sb, fill, string(0x2800+bits))
			}
		}
		lines[r] = sb.String()
	}
	return lines
}

// brailleLines draws each series (newest last) as a connected line of w x h
// cells using braille dots, two samples per cell, on a shared scale from 0
// to hi. Later series draw over earlier ones.
func brailleLines(p palette, series [][]float64, colors []color.Color, hi float64, w, h int) []string {
	dots := h * 4
	bits := make([]rune, w*h)
	owner := make([]int, w*h)
	for si, vals := range series {
		vals = tail(vals, w*2)
		pad := w*2 - len(vals)
		prev := -1
		for col := pad; col < w*2; col++ {
			y := min(max(int(vals[col-pad]/hi*float64(dots-1)+0.5), 0), dots-1)
			from, to := y, y
			if prev >= 0 {
				from, to = min(prev, y), max(prev, y)
				// Meet the previous sample halfway, so steep moves are
				// shared between the two columns.
				if y > prev {
					from = prev + (y-prev)/2
				} else if y < prev {
					to = y + (prev-y)/2
				}
			}
			// Two dots thick, so the line holds together between cells.
			if from == to {
				from = max(from-1, 0)
			}
			for d := from; d <= to; d++ {
				cellX, cellY := col/2, h-1-d/4
				i := cellY*w + cellX
				bits[i] |= brailleBits[col%2][3-d%4]
				owner[i] = si
			}
			prev = y
		}
	}

	lines := make([]string, h)
	for r := range h {
		var sb strings.Builder
		grid := r > 0 && r%2 == 0
		for x := range w {
			i := r*w + x
			switch {
			case bits[i] != 0:
				paint(&sb, colors[owner[i]], string(0x2800+bits[i]))
			case grid && x%2 == 0:
				paint(&sb, p.border, "·")
			default:
				sb.WriteByte(' ')
			}
		}
		lines[r] = sb.String()
	}
	return lines
}

// valueRange is the range of vals, widened by margin of its span on each side.
func valueRange(vals []float64, margin float64) (lo, hi float64) {
	if len(vals) == 0 {
		return 0, 1
	}
	lo, hi = vals[0], vals[0]
	for _, v := range vals {
		lo, hi = min(lo, v), max(hi, v)
	}
	span := max(hi-lo, 1e-9)
	return max(lo-span*margin, 0), hi + span*margin*0.4
}

func tail(vals []float64, n int) []float64 {
	if len(vals) > n {
		return vals[len(vals)-n:]
	}
	return vals
}

// sparkline draws vals as one row of w eighth blocks scaled to their own
// range, shaded left to right from lo to hi.
func sparkline(vals []float64, w int, lo, hi color.Color) string {
	vals = tail(vals, w)
	if len(vals) == 0 {
		return strings.Repeat(" ", w)
	}
	vmin, vmax := vals[0], vals[0]
	for _, v := range vals {
		vmin, vmax = min(vmin, v), max(vmax, v)
	}
	span := max(vmax-vmin, 1e-9)
	var sb strings.Builder
	sb.WriteString(strings.Repeat(" ", w-len(vals)))
	for i, v := range vals {
		idx := 1 + int((v-vmin)/span*7+0.5)
		paint(&sb, mix(lo, hi, float64(i)/float64(max(w-1, 1))), eighths[idx])
	}
	return sb.String()
}

// meter draws a w cell horizontal line filled to pct (0-1) along the accent
// gradient. The leading cell blends between the fill and the track, so the
// line advances smoothly between cells.
func meter(p palette, pct float64, w int) string {
	pct = min(max(pct, 0), 1)
	filled := pct * float64(w)
	var sb strings.Builder
	for x := range w {
		c := p.gradient(float64(x) / float64(max(w-1, 1)))
		switch k := filled - float64(x); {
		case k >= 1:
			paint(&sb, c, "━")
		case k > 0:
			paint(&sb, ramp(k, 4, p.border, c), "━")
		default:
			paint(&sb, p.border, "━")
		}
	}
	return sb.String()
}
