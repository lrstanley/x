// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fx has cell effects: functions that transform a node's rendered
// cells, or the frame beneath a dialog, for a progress value.
//
// The same [Effect] serves two purposes. Given to a transition, it is applied
// to the node's own cells, and t runs from 0, as the node starts to enter, to
// 1, when it is fully shown; at 1 a transition effect leaves the cells as
// they are. Given to a backdrop, it is applied to everything drawn beneath a
// layer, and t is the strength, following the layer's transition.
package fx

import (
	"image/color"
	"math"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// Effect transforms the cells of a surface for progress t, from 0 to 1.
//
// The layout engine skips compositing a frame identical to the previous one,
// which it decides by comparing effects with ==. Effects should be comparable
// values, as all of this package's are; effects that are not, and effects
// behind pointers, which can change in place, are assumed to change on every
// frame.
type Effect interface {
	Apply(s *canvas.Surface, t float64)
}

// Func adapts a function to an [Effect].
type Func func(s *canvas.Surface, t float64)

// Apply implements [Effect].
func (f Func) Apply(s *canvas.Surface, t float64) { f(s, t) }

// Edge is a side of a node or of the screen.
type Edge uint8

const (
	// Bottom is the bottom edge.
	Bottom Edge = iota
	// Top is the top edge.
	Top
	// Left is the left edge.
	Left
	// Right is the right edge.
	Right
)

// each calls fn for every shown cell of s.
func each(s *canvas.Surface, fn func(x, y int)) {
	for y := range s.Height() {
		for x := range s.Width() {
			if s.Visible(x, y) {
				fn(x, y)
			}
		}
	}
}

// recolor sets the colors of every shown cell of s from its current colors,
// resolved to the terminal's defaults when unset. A nil fg or bg leaves that
// color alone, and keepUnsetBg leaves unset backgrounds unset.
func recolor(s *canvas.Surface, fg, bg func(color.Color) color.Color, keepUnsetBg bool) {
	each(s, func(x, y int) {
		c := s.Cell(x, y)
		if fg != nil {
			c.Style.Fg = fg(s.Fg(c))
		}
		if bg != nil && (c.Style.Bg != nil || !keepUnsetBg) {
			c.Style.Bg = bg(s.Bg(c))
		}
	})
}

// hash returns a well-mixed value in [0, 1) for a cell and seed.
func hash(x, y int, seed uint32) float64 {
	h := uint64(x)*0x9e3779b97f4a7c15 ^ uint64(y)*0xc2b2ae3d27d4eb4f ^ uint64(seed)*0x165667b19e3779f9 //nolint:gosec // wrapping is fine for a hash.
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return float64(h>>11) / (1 << 53)
}

func lerpInt(a, b int, t float64) int { return int(math.Round(float64(a) + float64(b-a)*t)) }
