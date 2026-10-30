// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"fmt"
	"image"
	"slices"
)

// Corner is a corner of the terminal's cell grid in a frame.
type Corner int

// The corners of the cell grid.
const (
	TopLeft Corner = iota
	TopRight
	BottomLeft
	BottomRight
)

// check returns an error for a corner that is none of the four, of what
// the corner is for, such as "toast".
func (c Corner) check(what string) error {
	if c < TopLeft || c > BottomRight {
		return fmt.Errorf("effects: %s corner %d out of range", what, c)
	}
	return nil
}

// cornerAt is where a box of size goes in corner c of grid, inset from its
// edges.
func cornerAt(grid image.Rectangle, size image.Point, inset int, c Corner) image.Point {
	at := grid.Min.Add(image.Pt(inset, inset))
	if c == TopRight || c == BottomRight {
		at.X = grid.Max.X - inset - size.X
	}
	if c == BottomLeft || c == BottomRight {
		at.Y = grid.Max.Y - inset - size.Y
	}
	return at
}

// cornerNames are the names of the corners, by value.
var cornerNames = []string{"top_left", "top_right", "bottom_left", "bottom_right"}

// parseCorner returns the corner named name (see cornerNames).
func parseCorner(name string) (Corner, error) {
	c := slices.Index(cornerNames, name)
	if c < 0 {
		return 0, fmt.Errorf("unknown corner %q", name)
	}
	return Corner(c), nil
}
