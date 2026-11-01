// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"
	"math"

	"github.com/lrstanley/x/term/bloom/canvas"
)

// Spotlight dims everything but a rectangle, for drawing attention to part
// of a screen, such as a step of a tour. It suits backdrops, where t is the
// strength.
type Spotlight struct {
	// Rect is the area left lit, in frame coordinates, as returned by
	// Frame.Rect. An empty Rect dims everything.
	Rect image.Rectangle
	// Amount is how far colors outside Rect move toward the terminal's
	// background, from 0 to 1; zero is 0.7.
	Amount float64
	// Soft is how many cells around Rect the dimming takes to reach its full
	// amount; zero is a hard edge. Horizontal distances count for half, as
	// cells are twice as tall as they are wide, so the falloff looks round.
	Soft int
}

// Apply implements [Effect].
func (e Spotlight) Apply(s *canvas.Surface, t float64) {
	amount := e.Amount
	if amount == 0 {
		amount = 0.7
	}
	amount *= t
	if amount <= 0 {
		return
	}
	lit := e.Rect.Sub(s.Rect().Min)
	soft := float64(max(e.Soft, 0))
	for y := range s.Height() {
		dy := max(lit.Min.Y-y, y-(lit.Max.Y-1), 0)
		for x := range s.Width() {
			dx := max(lit.Min.X-x, x-(lit.Max.X-1), 0)
			f := amount
			if !lit.Empty() {
				if dx == 0 && dy == 0 {
					continue
				}
				if soft > 0 {
					d := math.Hypot(float64(dx)/2, float64(dy)) / soft
					if d < 1 {
						f *= d * d * (3 - 2*d)
					}
				}
			}
			if f < spotlightMin || !s.Visible(x, y) {
				continue
			}
			c := s.Cell(x, y)
			c.Style.Fg = s.Mix(s.Fg(c), s.Env.Bg, f)
			if c.Style.Bg != nil {
				c.Style.Bg = s.Mix(c.Style.Bg, s.Env.Bg, f)
			}
		}
	}
}

// spotlightMin is the dimming below which colors are left alone, as the
// mixer cannot tell such amounts apart from none.
const spotlightMin = 1.0 / 1024
