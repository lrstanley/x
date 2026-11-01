// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// Fade crossfades a node in over the frame beneath it. Backgrounds blend
// from the one beneath to the node's own; until t=0.5 each cell shows the
// glyph beneath, fading into that background, and after it the node's own,
// fading in. At t=0 the node looks exactly like what is beneath it. Cells
// over or holding wide glyphs keep the node's glyph throughout.
type Fade struct{}

// Apply implements [Effect].
func (Fade) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	each(s, func(x, y int) {
		c, under := s.Cell(x, y), s.Under(x, y)
		bg := s.Mix(s.Bg(under), s.Bg(c), t)
		if t < 0.5 && under != nil && under.Width == 1 && c.Width == 1 {
			c.Content, c.Link = under.Content, under.Link
			c.Style.Attrs, c.Style.Underline = under.Style.Attrs, under.Style.Underline
			c.Style.Fg = s.Mix(s.Fg(under), bg, 2*t)
		} else {
			c.Style.Fg = s.Mix(bg, s.Fg(c), max(2*t-1, 0))
		}
		c.Style.Bg = bg
	})
}

// Slide moves a node in from beyond an edge of its viewport, such as a
// dialog rising from the bottom of the screen.
type Slide struct {
	From Edge
}

// Apply implements [Effect].
func (e Slide) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	r, vp := s.Rect(), s.Viewport
	var off image.Point
	switch e.From {
	case Bottom:
		off.Y = lerpInt(vp.Max.Y-r.Min.Y, 0, t)
	case Top:
		off.Y = lerpInt(vp.Min.Y-r.Max.Y, 0, t)
	case Left:
		off.X = lerpInt(vp.Min.X-r.Max.X, 0, t)
	case Right:
		off.X = lerpInt(vp.Max.X-r.Min.X, 0, t)
	}
	s.Offset = s.Offset.Add(off)
}

// Wipe reveals a node from one edge to the other.
type Wipe struct {
	From Edge
}

// Apply implements [Effect].
func (e Wipe) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	w, h := s.Width(), s.Height()
	shownX, shownY := int(float64(w)*t), int(float64(h)*t)
	each(s, func(x, y int) {
		var shown bool
		switch e.From {
		case Left:
			shown = x < shownX
		case Right:
			shown = x >= w-shownX
		case Top:
			shown = y < shownY
		case Bottom:
			shown = y >= h-shownY
		}
		if !shown {
			s.Hide(x, y)
		}
	})
}

// Dissolve reveals a node's cells in a random order, the same for the same
// Seed.
type Dissolve struct {
	Seed uint32
}

// Apply implements [Effect].
func (e Dissolve) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	each(s, func(x, y int) {
		if hash(x, y, e.Seed) >= t {
			s.Hide(x, y)
		}
	})
}

// Typewriter reveals a node's cells in reading order.
type Typewriter struct{}

// Apply implements [Effect].
func (Typewriter) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	shown := int(float64(s.Width()*s.Height()) * t)
	each(s, func(x, y int) {
		if y*s.Width()+x >= shown {
			s.Hide(x, y)
		}
	})
}

// scrambleGlyphs are what [Scramble] shows in place of a cell's own glyph.
const scrambleGlyphs = `!#$%&*+-/<=>?@\^_|~0123456789`

// Scramble shows random glyphs that settle into a node's own, in a random
// order, the same for the same Seed. Only single-width glyphs are scrambled.
type Scramble struct {
	Seed uint32
}

// scrambleSteps is how many times per transition the random glyphs change.
const scrambleSteps = 24

// Apply implements [Effect].
func (e Scramble) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	step := uint32(t * scrambleSteps)
	each(s, func(x, y int) {
		c := s.Cell(x, y)
		if c.Width != 1 || c.Content == " " || hash(x, y, e.Seed) < t {
			return
		}
		i := int(hash(x, y, e.Seed^step*0x9e3779b9) * float64(len(scrambleGlyphs)))
		c.Content = scrambleGlyphs[i : i+1]
	})
}
