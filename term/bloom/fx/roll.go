// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import (
	"unicode/utf8"

	"github.com/lrstanley/x/term/bloom/canvas"
)

const (
	rollDigits = "0123456789"
	rollWindow = 0.5  // how much of the transition each digit takes to roll.
	rollStep   = 0.1  // the most one digit starts after the one to its right.
	rollTint   = 0.35 // how far a rolling digit moves toward the background.
)

// Roll turns the digits of a number over like an odometer, for numbers that
// change, such as a counter. It is applied to a node showing the new number,
// with From the number it showed before, and each row of the node is treated
// as a number. Digits are matched from the right, skipping separators like
// commas, so 1,058 rolling from 1,021 turns its last two digits over, and the
// rest stay put. A digit rolls through the digits between its old and new
// ones the short way around, counting past 9 to 0 when that is shorter. The
// last digits start first, and rolling digits are dimmed slightly to read as
// motion.
//
// At t=0 the node shows From's digits where they line up with the new
// number's, and at 1 its own. Digits with nothing to roll from, as when the
// number gains a place, appear as they are.
type Roll struct {
	From string
}

// Apply implements [Effect].
func (e Roll) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	t = max(t, 0)
	for y := range s.Height() {
		// The number ends at the last cell with something in it, so styled
		// padding after it does not shift the digits.
		end := s.Width() - 1
		for end >= 0 && isBlank(s, end, y) {
			end--
		}
		rest, k := e.From, 0
		for x := end; x >= 0; x-- {
			c := s.Cell(x, y)
			if c.Width != 1 || len(c.Content) != 1 || c.Content[0] < '0' || c.Content[0] > '9' {
				continue
			}
			var old int
			old, rest = lastDigit(rest)
			k++
			if old < 0 {
				continue
			}
			if !s.Visible(x, y) {
				continue
			}
			next := int(c.Content[0] - '0')
			p := (t - float64(k-1)*min(rollStep, rollWindow/float64(end+1))) / rollWindow
			if next == old || p >= 1 {
				continue
			}
			p = max(p, 0)
			up := (next - old + 10) % 10
			steps, dir := up, 1
			if up > 5 {
				steps, dir = 10-up, -1
			}
			shown := (old + dir*min(int(p*float64(steps+1)), steps) + 10) % 10
			c.Content = rollDigits[shown : shown+1]
			if p > 0 {
				c.Style.Fg = s.Mix(s.Fg(c), s.Bg(c), rollTint)
			}
		}
	}
}

// isBlank reports whether the cell at x, y of s shows nothing.
func isBlank(s *canvas.Surface, x, y int) bool {
	c := s.Cell(x, y)
	return c.Content == "" || c.Content == " "
}

// lastDigit returns the last digit of str, skipping anything that is not
// one, and the rest of str before it, or -1 if there is none.
func lastDigit(str string) (digit int, rest string) {
	for str != "" {
		r, size := utf8.DecodeLastRuneInString(str)
		str = str[:len(str)-size]
		if r >= '0' && r <= '9' {
			return int(r - '0'), str
		}
	}
	return -1, ""
}
