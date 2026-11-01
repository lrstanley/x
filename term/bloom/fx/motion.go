// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"image"
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/bloom/canvas"
)

// decodeGlyphs are what the leading cell of a [Decode] column shows.
const decodeGlyphs = `0123456789ABCDEF:=+*<>|/\`

// decodeSteps is how many times per transition the leading glyphs change.
const decodeSteps = 48

// Decode reveals a node like digital rain settling into text: each column is
// revealed from the top down, after a random delay. The leading cell of a
// column shows a bright random glyph, the cells above it their own content,
// and those below it nothing. The delays are the same for the same Seed. Only
// single-width glyphs are replaced.
type Decode struct {
	Seed uint32
}

// Apply implements [Effect].
func (e Decode) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	h := s.Height()
	step := uint32(max(t, 0) * decodeSteps)
	for x := range s.Width() {
		// Every column finishes by t=1, the later it starts the faster it falls.
		delay := hash(x, -1, e.Seed) * 0.5
		p := (t - delay) / (1 - delay)
		head := int(p * float64(h+1))
		if p < 0 {
			head = -1
		}
		for y := range h {
			if !s.Visible(x, y) {
				continue
			}
			switch {
			case y < head:
			case y > head:
				s.Hide(x, y)
			default:
				c := s.Cell(x, y)
				if c.Width != 1 {
					continue
				}
				i := int(hash(x, y, e.Seed^step*0x9e3779b9) * float64(len(decodeGlyphs)))
				c.Content = decodeGlyphs[i : i+1]
				c.Style.Fg = s.Env.Fg
				c.Style.Attrs |= uv.AttrBold
			}
		}
	}
}

// glitchSteps is how many times per transition the glitch pattern changes.
const glitchSteps = 16

// glitchGarble is what garbled cells show.
var glitchGarble = [...]string{"░", "▒", "▓"}

var (
	glitchRed  color.Color = color.RGBA{R: 0xff, G: 0x30, B: 0x50, A: 0xff}
	glitchCyan color.Color = color.RGBA{R: 0x20, G: 0xe0, B: 0xff, A: 0xff}
)

// Glitch shows a node as if the signal were breaking up: rows jitter
// sideways in short bursts, their colors split toward red and cyan, and some
// cells are hidden or garbled. The pattern changes in steps, so it flickers,
// and fades to nothing as t reaches 1. It is the same for the same Seed. Rows
// holding wide glyphs are not moved.
type Glitch struct {
	Seed uint32
}

// Apply implements [Effect].
func (e Glitch) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	step := uint32(max(t, 0) * glitchSteps)
	amount := 1 - float64(step)/glitchSteps
	seed := e.Seed ^ step*0x9e3779b9
	w := s.Width()

	for y := range s.Height() {
		if hash(-1, y, seed) < amount*0.4 {
			shift := 1 + int(hash(-2, y, seed)*amount*6)
			if hash(-3, y, seed) < 0.5 {
				shift = -shift
			}
			s.Move(image.Rect(0, y, w, y+1), image.Pt(shift, 0))
			split := glitchRed
			if y%2 != 0 {
				split = glitchCyan
			}
			for x := range w {
				if s.Visible(x, y) {
					c := s.Cell(x, y)
					c.Style.Fg = s.Mix(s.Fg(c), split, 0.3+0.5*amount)
				}
			}
		}
		for x := range w {
			if !s.Visible(x, y) {
				continue
			}
			u := hash(x, y, seed)
			switch c := s.Cell(x, y); {
			case u < amount*0.03:
				s.Hide(x, y)
			case u < amount*0.08 && c.Width == 1 && c.Content != " ":
				c.Content = glitchGarble[int(hash(y, x, seed)*float64(len(glitchGarble)))]
			}
		}
	}
}

// Shake moves a node from side to side with a decaying wobble, such as an
// input that rejected what was typed. t is the shake's progress: the node is
// still at 0 and at 1.
type Shake struct {
	// Amount is how far, in cells, the node moves at most; zero is 2.
	Amount int
	// Times is how many times the node swings each way; zero is 4.
	Times int
}

// Apply implements [Effect].
func (e Shake) Apply(s *canvas.Surface, t float64) {
	amount, times := e.Amount, e.Times
	if amount == 0 {
		amount = 2
	}
	if times <= 0 {
		times = 4
	}
	if t <= 0 || t >= 1 {
		return
	}
	s.Offset.X += int(math.Round(float64(amount) * math.Sin(2*math.Pi*float64(times)*t) * (1 - t)))
}

// Shimmer sweeps a highlight band across a node diagonally, for placeholders
// shown while content loads. t is the band's position: it starts just off the
// node and ends just off the other side, so a value that runs from 0 to 1 and
// then jumps back to 0 loops seamlessly.
type Shimmer struct {
	// Color is what colors are mixed toward in the band; nil is the
	// terminal's foreground.
	Color color.Color
	// Width is how wide the band is, in cells; zero is 6.
	Width int
}

// shimmerStrength is how far colors in the middle of the band move toward
// its color.
const shimmerStrength = 0.6

// Apply implements [Effect].
func (e Shimmer) Apply(s *canvas.Surface, t float64) {
	to, width := e.Color, e.Width
	if to == nil {
		to = s.Env.Fg
	}
	if width <= 0 {
		width = 6
	}
	half := float64(width) / 2
	// The band is a line at 45 degrees as the terminal draws it, as a cell
	// is twice as tall as it is wide: position u = x + 2y.
	span := float64(s.Width() + 2*s.Height())
	center := -half + min(max(t, 0), 1)*(span+float64(width))
	for y := range s.Height() {
		lo := max(int(math.Ceil(center-half))-2*y, 0)
		hi := min(int(math.Floor(center+half))-2*y, s.Width()-1)
		for x := lo; x <= hi; x++ {
			if !s.Visible(x, y) {
				continue
			}
			k := 1 - math.Abs(float64(x+2*y)-center)/half
			if k <= 0 {
				continue
			}
			f := shimmerStrength * k * k * (3 - 2*k)
			c := s.Cell(x, y)
			c.Style.Fg = s.Mix(s.Fg(c), to, f)
			c.Style.Bg = s.Mix(s.Bg(c), to, f)
		}
	}
}
