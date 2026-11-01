// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package canvas

import (
	"image/color"

	"github.com/charmbracelet/colorprofile"
)

// Env describes the terminal a frame is drawn for.
type Env struct {
	// Fg and Bg are the terminal's default colors, which a cell without a
	// color shows. Effects that blend colors need them; a frame drawn before
	// the terminal reported them uses white on black.
	Fg, Bg color.Color
	// Profile is the terminal's color profile. Colors are blended in true
	// color either way, and Bubble Tea converts them down, but an effect may
	// want plainer colors on a terminal with few.
	Profile colorprofile.Profile
}

// Equal reports whether e and o describe the same terminal. Colors are
// compared by value, so any [color.Color] can be compared.
func (e Env) Equal(o Env) bool {
	return e.Profile == o.Profile && sameColor(e.Fg, o.Fg) && sameColor(e.Bg, o.Bg)
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return toRGBA(a) == toRGBA(b)
}

// DefaultEnv is the [Env] used until the terminal reports its colors.
var DefaultEnv = Env{
	Fg:      color.RGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff},
	Bg:      color.RGBA{A: 0xff},
	Profile: colorprofile.TrueColor,
}

// mixOp is the kind of color math a [Mixer] result was computed with.
type mixOp uint8

const (
	opMix mixOp = iota
	opGray
)

type mixKey struct {
	op   mixOp
	a, b color.RGBA
	t    uint16
}

// mixSteps is how finely blend amounts are told apart, which bounds how many
// distinct colors a fade produces.
const mixSteps = 1024

// mixMemoLimit bounds the number of colors a [Mixer] remembers.
const mixMemoLimit = 1 << 14

// Mixer blends colors and remembers the results, since a frame tends to
// blend the same few colors over and over and each new [color.Color] value
// is an allocation.
type Mixer struct {
	// recent is checked before memo: neighboring cells tend to share colors,
	// and a direct-mapped slot is cheaper than a map lookup.
	recent [64]struct {
		key mixKey
		out color.Color
	}
	memo map[mixKey]color.Color
}

// Mix returns the color t of the way from a to b, so a at t=0 and b at t=1.
// Neither color may be nil.
func (m *Mixer) Mix(a, b color.Color, t float64) color.Color {
	return m.blend(opMix, a, b, t)
}

// Gray returns c moved t of the way toward its own luminance.
func (m *Mixer) Gray(c color.Color, t float64) color.Color {
	return m.blend(opGray, c, c, t)
}

func (m *Mixer) blend(op mixOp, a, b color.Color, t float64) color.Color {
	t = min(max(t, 0), 1)
	k := mixKey{op: op, a: toRGBA(a), b: toRGBA(b), t: uint16(t * mixSteps)}
	if k.t == 0 && op == opMix {
		return a
	}
	if k.t == mixSteps && op == opMix {
		return b
	}
	slot := &m.recent[(uint(k.a.R)*7+uint(k.a.G)*11+uint(k.a.B)*13+uint(k.b.R)*3+uint(k.b.G)*5+uint(k.b.B)+uint(k.t)+uint(op))%uint(len(m.recent))]
	if slot.out != nil && slot.key == k {
		return slot.out
	}
	if c, ok := m.memo[k]; ok {
		slot.key, slot.out = k, c
		return c
	}
	if m.memo == nil || len(m.memo) >= mixMemoLimit {
		m.memo = make(map[mixKey]color.Color, 256)
	}

	f := float64(k.t) / mixSteps
	var out color.RGBA
	switch op {
	case opMix:
		out = lerp(k.a, k.b, f)
	case opGray:
		y := uint8(0.299*float64(k.a.R) + 0.587*float64(k.a.G) + 0.114*float64(k.a.B))
		out = lerp(k.a, color.RGBA{R: y, G: y, B: y, A: 0xff}, f)
	}
	m.memo[k] = out
	slot.key, slot.out = k, out
	return out
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 0xff}
}

func toRGBA(c color.Color) color.RGBA {
	if rgba, ok := c.(color.RGBA); ok {
		return rgba
	}
	// RGBA returns 16-bit channels; the high byte is the 8-bit value.
	r, g, b, _ := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff} //nolint:gosec // channels are at most 0xffff.
}
