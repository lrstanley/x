// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"image/color"
	"math"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// palette is every color the dashboard draws with. Palettes blend into each
// other (see [palette.blend]), which is how theme switches cross-fade and how
// panels fade in from the background.
type palette struct {
	name    string
	bg      color.Color
	surface color.Color
	border  color.Color
	subtle  color.Color
	muted   color.Color
	text    color.Color
	bright  color.Color
	accent  [3]color.Color
	ok      color.Color
	warn    color.Color
	err     color.Color
}

var hex = lipgloss.Color

var themes = []palette{
	{
		name:    "aurora",
		bg:      hex("#0b0e14"),
		surface: hex("#161b26"),
		border:  hex("#262d3d"),
		subtle:  hex("#3a4356"),
		muted:   hex("#6c7891"),
		text:    hex("#c8d0e0"),
		bright:  hex("#f2f5fa"),
		accent:  [3]color.Color{hex("#5eead4"), hex("#818cf8"), hex("#f472b6")},
		ok:      hex("#4ade80"),
		warn:    hex("#fbbf24"),
		err:     hex("#f87171"),
	},
	{
		name:    "ember",
		bg:      hex("#110b09"),
		surface: hex("#1e1512"),
		border:  hex("#33241e"),
		subtle:  hex("#4d3a31"),
		muted:   hex("#94776a"),
		text:    hex("#e6d3c8"),
		bright:  hex("#fff4ec"),
		accent:  [3]color.Color{hex("#fde047"), hex("#fb923c"), hex("#f43f5e")},
		ok:      hex("#a3e635"),
		warn:    hex("#facc15"),
		err:     hex("#ef4444"),
	},
	{
		name:    "tide",
		bg:      hex("#060d17"),
		surface: hex("#0e1a2b"),
		border:  hex("#1a2c45"),
		subtle:  hex("#2b4466"),
		muted:   hex("#6383ab"),
		text:    hex("#c6d8ee"),
		bright:  hex("#f0f7ff"),
		accent:  [3]color.Color{hex("#a5f3fc"), hex("#38bdf8"), hex("#6366f1")},
		ok:      hex("#34d399"),
		warn:    hex("#fcd34d"),
		err:     hex("#fb7185"),
	},
}

// Animated colors snap to a few steps. A GIF palette holds 255 colors, and a
// fade that mints new colors every frame fills it quickly; each time it does,
// the recording starts a new palette, which costs a local color table and
// often a keyframe.
const (
	fadeSteps     = 6
	gradientSteps = 32
	mixSteps      = 16
)

func snap(t float64, steps int) float64 {
	return float64(int(t*float64(steps)+0.5)) / float64(steps)
}

// blendKey identifies one [lipgloss.Blend1D] call.
type blendKey struct {
	steps int
	stops [3]color.RGBA
}

var (
	blendMu    sync.Mutex
	blendCache = map[blendKey][]color.Color{}
)

// ramp samples evenly spaced color stops (at most 3) at t in [0,1], snapped
// to one of steps+1 colors. Each set of stops is blended once, with
// [lipgloss.Blend1D], and reused.
func ramp(t float64, steps int, stops ...color.Color) color.Color {
	key := blendKey{steps: steps}
	for i, c := range stops {
		r, g, b, a := c.RGBA()
		key.stops[i] = color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)} //nolint:gosec // 16-bit channels to 8.
	}
	blendMu.Lock()
	colors, ok := blendCache[key]
	if !ok {
		// Blend1D keeps its arguments, so only a miss copies stops.
		colors = lipgloss.Blend1D(steps+1, slices.Clone(stops)...)
		blendCache[key] = colors
	}
	blendMu.Unlock()
	return colors[int(min(max(t, 0), 1)*float64(len(colors)-1)+0.5)]
}

// mix returns the color t of the way from a to b.
func mix(a, b color.Color, t float64) color.Color { return ramp(t, mixSteps, a, b) }

// blend returns the palette t of the way from p to q.
func (p palette) blend(q palette, t float64) palette {
	t = snap(t, fadeSteps)
	if t <= 0 {
		return p
	}
	if t >= 1 {
		return q
	}
	step := func(a, b color.Color) color.Color { return ramp(t, fadeSteps, a, b) }
	out := palette{
		name:    q.name,
		bg:      step(p.bg, q.bg),
		surface: step(p.surface, q.surface),
		border:  step(p.border, q.border),
		subtle:  step(p.subtle, q.subtle),
		muted:   step(p.muted, q.muted),
		text:    step(p.text, q.text),
		bright:  step(p.bright, q.bright),
		ok:      step(p.ok, q.ok),
		warn:    step(p.warn, q.warn),
		err:     step(p.err, q.err),
	}
	for i := range out.accent {
		out.accent[i] = step(p.accent[i], q.accent[i])
	}
	return out
}

// fade returns the palette with every color a of the way from the
// background to its own value, so a = 0 is invisible and a = 1 is unchanged.
func (p palette) fade(a float64) palette {
	if a >= 1 {
		return p
	}
	flat := p
	for _, c := range []*color.Color{
		&flat.surface, &flat.border, &flat.subtle, &flat.muted, &flat.text,
		&flat.bright, &flat.ok, &flat.warn, &flat.err,
		&flat.accent[0], &flat.accent[1], &flat.accent[2],
	} {
		*c = p.bg
	}
	return flat.blend(p, max(a, 0))
}

// gradient samples the accent stops at t in [0,1].
func (p palette) gradient(t float64) color.Color {
	return ramp(t, gradientSteps, p.accent[:]...)
}

// styles are the lipgloss styles derived from one palette.
type styles struct {
	pal palette

	text, muted, subtle, bright lipgloss.Style
	bold, label                 lipgloss.Style
	ok, warn, err, accent       lipgloss.Style
	border                      lipgloss.Style
	selected                    lipgloss.Style
	key, keyDesc                lipgloss.Style
}

func newStyles(p palette) *styles {
	s := lipgloss.NewStyle()
	return &styles{
		pal:      p,
		text:     s.Foreground(p.text),
		muted:    s.Foreground(p.muted),
		subtle:   s.Foreground(p.subtle),
		bright:   s.Foreground(p.bright),
		bold:     s.Foreground(p.bright).Bold(true),
		label:    s.Foreground(p.muted).Bold(true),
		ok:       s.Foreground(p.ok),
		warn:     s.Foreground(p.warn),
		err:      s.Foreground(p.err),
		accent:   s.Foreground(p.accent[0]),
		border:   s.Foreground(p.border),
		selected: s.Background(p.surface),
		key:      s.Foreground(p.accent[0]).Bold(true),
		keyDesc:  s.Foreground(p.muted),
	}
}

// fgSeq returns the SGR sequence that sets a 24-bit foreground color. Gradient
// text colors every cell differently, so it is written as raw sequences
// rather than through one lipgloss style per cell.
func fgSeq(c color.Color) string { return ansi.NewStyle().ForegroundColor(c).String() }

// bgSeq returns the SGR sequence that sets a 24-bit background color.
func bgSeq(c color.Color) string { return ansi.NewStyle().BackgroundColor(c).String() }

const (
	fgReset = "\x1b[39m"
	bgReset = "\x1b[49m"
)

// paint writes s to sb in color c.
func paint(sb *strings.Builder, c color.Color, s string) {
	sb.WriteString(fgSeq(c))
	sb.WriteString(s)
	sb.WriteString(fgReset)
}

// gradientText colors each cell of s along the accent gradient, shifted by
// offset (which wraps), so a moving offset makes the text shimmer.
func gradientText(p palette, s string, offset float64, bold bool) string {
	runes := []rune(s)
	var sb strings.Builder
	if bold {
		sb.WriteString("\x1b[1m")
	}
	for i, r := range runes {
		paint(&sb, p.gradient(mirror(float64(i)/float64(max(len(runes)-1, 1))+offset)), string(r))
	}
	if bold {
		sb.WriteString("\x1b[22m")
	}
	return sb.String()
}

// mirror wraps t into [0,1) and folds it into a rise and fall, so a gradient
// sampled at a moving t loops without a seam.
func mirror(t float64) float64 {
	return 1 - math.Abs(2*(t-math.Floor(t))-1)
}
