// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"
	"image/color"
	"math"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// burstGlyphs are what a particle twinkles through, and burstFaded what it
// shrinks to as it fades.
var burstGlyphs = [...]string{"•", "*", "✦", "·"}

const burstFaded = "·"

var burstPalette = [...]color.Color{
	color.RGBA{R: 0xff, G: 0x5d, B: 0x7a, A: 0xff},
	color.RGBA{R: 0xff, G: 0xb4, B: 0x3b, A: 0xff},
	color.RGBA{R: 0xff, G: 0xe0, B: 0x4a, A: 0xff},
	color.RGBA{R: 0x5d, G: 0xe0, B: 0x7a, A: 0xff},
	color.RGBA{R: 0x4a, G: 0xd6, B: 0xff, A: 0xff},
	color.RGBA{R: 0x7a, G: 0x8c, B: 0xff, A: 0xff},
	color.RGBA{R: 0xc8, G: 0x7a, B: 0xff, A: 0xff},
	color.RGBA{R: 0xff, G: 0x7a, B: 0xe0, A: 0xff},
}

const (
	burstGravity  = 30.0 // rows per burst squared.
	burstFadeFrom = 0.6  // when particles start to fade.
	burstTwinkles = 10   // how many times a particle changes glyph over a burst.
)

// Burst is a one-shot burst of confetti, such as for a success: particles
// fly out from a point, mostly upward, fall under gravity, twinkle, and fade.
// t is the burst's age, from 0 to 1; nothing is drawn at 1.
//
// Particles are placed from Seed, their index, and t alone, so a burst keeps
// no state and is the same for the same Seed. They only change the glyph and
// foreground of the single-width cells they land on, so a burst works on
// a region of the frame, such as a layer's backdrop, or on a node's cells,
// as in bloom.Transition(age, base, fx.Burst{...}). Only the cells of the
// surface are drawn on, so a burst from a small node stays inside it.
type Burst struct {
	// At is where the burst starts, in frame coordinates.
	At image.Point
	// Count is the number of particles; zero is 40.
	Count int
	// Seed varies the burst.
	Seed uint32
}

// Apply implements [Effect].
func (e Burst) Apply(s *canvas.Surface, t float64) {
	if t < 0 || t >= 1 {
		return
	}
	n := e.Count
	if n <= 0 {
		n = 40
	}
	origin := e.At.Sub(s.Rect().Min)
	fade := max((t-burstFadeFrom)/(1-burstFadeFrom), 0)
	twinkle := int(t * burstTwinkles)
	for i := range n {
		// Speed and angle are uniform: out to the sides and up, with some
		// falling short of the horizon. Cells are half as wide as they are
		// tall, so horizontal speed is doubled.
		speed := 8 + 14*hash(i, 0, e.Seed)
		angle := (hash(i, 1, e.Seed)*1.2 - 0.1) * math.Pi
		sin, cos := math.Sincos(angle)
		x := origin.X + int(math.Round(2*speed*cos*t))
		y := origin.Y + int(math.Round(-speed*sin*t+0.5*burstGravity*t*t))

		c := s.Cell(x, y)
		if c == nil || c.Width != 1 || !s.Visible(x, y) {
			continue
		}
		fg := burstPalette[int(hash(i, 2, e.Seed)*float64(len(burstPalette)))]
		if fade > 0 {
			fg = s.Mix(fg, s.Bg(c), fade)
		}
		if fade > 0.5 {
			c.Content = burstFaded
		} else {
			c.Content = burstGlyphs[(i+twinkle)%(len(burstGlyphs)-1)]
		}
		c.Style.Fg = fg
	}
}
