// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image/color"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// The effects in this file suit backdrops, where t is the strength, though
// they work on any surface.

// Dim moves colors toward the terminal's background by Amount, from 0 to 1.
// Unset backgrounds are left unset, as they already show the terminal's.
type Dim struct {
	Amount float64
}

// Apply implements [Effect].
func (e Dim) Apply(s *canvas.Surface, t float64) {
	f := e.Amount * t
	if f <= 0 {
		return
	}
	toBg := func(c color.Color) color.Color { return s.Mix(c, s.Env.Bg, f) }
	recolor(s, toBg, toBg, true)
}

// Tint moves colors toward Color by Amount, from 0 to 1.
type Tint struct {
	Color  color.Color
	Amount float64
}

// Apply implements [Effect].
func (e Tint) Apply(s *canvas.Surface, t float64) {
	f := e.Amount * t
	if f <= 0 || e.Color == nil {
		return
	}
	to := func(c color.Color) color.Color { return s.Mix(c, e.Color, f) }
	recolor(s, to, to, false)
}

// Recolor sets every cell to the colors Fg and Bg, keeping glyphs, the way
// vex draws its dialog backdrop; below t=1 it blends toward them. A nil color
// is left as it is.
type Recolor struct {
	Fg, Bg color.Color
}

// Apply implements [Effect].
func (e Recolor) Apply(s *canvas.Surface, t float64) {
	if t <= 0 {
		return
	}
	var fg, bg func(color.Color) color.Color
	if e.Fg != nil {
		fg = func(c color.Color) color.Color { return s.Mix(c, e.Fg, t) }
	}
	if e.Bg != nil {
		bg = func(c color.Color) color.Color { return s.Mix(c, e.Bg, t) }
	}
	recolor(s, fg, bg, false)
}

// Desaturate moves colors toward gray by Amount, from 0 to 1. Unset
// backgrounds are left unset.
type Desaturate struct {
	Amount float64
}

// Apply implements [Effect].
func (e Desaturate) Apply(s *canvas.Surface, t float64) {
	f := e.Amount * t
	if f <= 0 {
		return
	}
	gray := func(c color.Color) color.Color { return s.Gray(c, f) }
	recolor(s, gray, gray, true)
}
