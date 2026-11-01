// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// The effects in this file wrap another effect, changing the t it sees or
// the part of the surface it runs on. They are comparable when the effects
// they wrap are.

// Stagger splits a surface into rows, or columns, that each run Effect on
// their own, one after another, such as a list whose items slide in one by
// one. Part i of n starts at Spread*i/(n-1) of t and runs over the 1-Spread
// that is left, so the last part ends as t reaches 1.
//
// Offsets that Effect sets on a part, as [Slide] does, are applied by moving
// that part's cells within its own row or column, so a part never leaves
// its band: Stagger{Effect: Slide{From: Left}, From: Top} has each row of a
// node slide in from the left, clipped to the node's width. Parts see the
// node as their viewport, so a slide starts at the node's own edge rather
// than the screen's. A wide glyph cut by the edge of a column that is being
// moved is blanked while it moves.
type Stagger struct {
	Effect Effect
	// From is where the first part is, and so which parts come first: Top
	// and Bottom split the surface into rows, Left and Right into columns.
	// The zero value is Bottom.
	From Edge
	// Spread is how much of t is spent starting the parts, from 0 to 1: 0.5
	// has the last part start as the first is halfway, and 1 has each part
	// run in an instant. Zero means 0.5.
	Spread float64
}

// Apply implements [Effect].
func (e Stagger) Apply(s *canvas.Surface, t float64) {
	if e.Effect == nil {
		return
	}
	spread := e.Spread
	if spread <= 0 {
		spread = 0.5
	}
	spread = min(spread, 1)

	w, h := s.Width(), s.Height()
	vertical := e.From == Top || e.From == Bottom
	n := w
	if vertical {
		n = h
	}
	base := s.Offset
	node := s.Rect().Add(base)
	for i := range n {
		var start float64
		if n > 1 {
			start = spread * float64(i) / float64(n-1)
		}
		var band image.Rectangle
		switch e.From {
		case Top:
			band = image.Rect(0, i, w, i+1)
		case Bottom:
			band = image.Rect(0, h-1-i, w, h-i)
		case Left:
			band = image.Rect(i, 0, i+1, h)
		case Right:
			band = image.Rect(w-1-i, 0, w-i, h)
		}
		part := s.Sub(band)
		part.Viewport = node
		e.Effect.Apply(part, windowed(t, start, start+(1-spread)))
		if d := part.Offset.Sub(base); d != (image.Point{}) {
			s.Move(band, d)
		}
	}
}

// Delay runs Effect over part of t, from From to To: before From it sees 0,
// and after To it sees 1. A zero To means 1. Delays combined in a list of
// effects run in sequence.
type Delay struct {
	Effect   Effect
	From, To float64
}

// Apply implements [Effect].
func (e Delay) Apply(s *canvas.Surface, t float64) {
	if e.Effect == nil {
		return
	}
	to := e.To
	if to == 0 {
		to = 1
	}
	e.Effect.Apply(s, windowed(t, e.From, to))
}

// Reverse runs Effect with 1-t, such as an effect for a node that is leaving
// while the progress passed to it rises, or a backdrop that weakens as t
// grows.
type Reverse struct {
	Effect Effect
}

// Apply implements [Effect].
func (e Reverse) Apply(s *canvas.Surface, t float64) {
	if e.Effect != nil {
		e.Effect.Apply(s, 1-t)
	}
}

// Ease runs Effect with t eased by Easing, such as one of the easings of
// package anim. A nil Easing leaves t as it is, and 0 and 1 are never eased,
// so an easing that overshoots, like anim.OutBack, still ends where the
// effect does.
//
// A function is not comparable, so a frame that uses Ease is composited
// again on every frame, even if t has not changed.
type Ease struct {
	Effect Effect
	Easing func(float64) float64
}

// Apply implements [Effect].
func (e Ease) Apply(s *canvas.Surface, t float64) {
	if e.Effect == nil {
		return
	}
	if e.Easing != nil && t > 0 && t < 1 {
		t = e.Easing(t)
	}
	e.Effect.Apply(s, t)
}

// windowed returns where t is between from and to, clamped to [0, 1]. A
// window with no length is a step at from.
func windowed(t, from, to float64) float64 {
	switch {
	case t <= from && t < to:
		return 0
	case t >= to:
		return 1
	}
	return (t - from) / (to - from)
}
