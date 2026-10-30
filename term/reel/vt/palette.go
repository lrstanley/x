// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"image/color"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// Palette is a color for some of the 256 indexed colors. The zero value has
// none.
type Palette struct {
	set    [4]uint64
	colors [256]color.NRGBA
}

// Color returns the color at index i, and whether p has one. A nil p has
// none.
func (p *Palette) Color(i uint8) (color.NRGBA, bool) {
	if p == nil || p.set[i>>6]&(1<<(i&63)) == 0 {
		return color.NRGBA{}, false
	}
	return p.colors[i], true
}

// Set sets the color at index i, for building a palette. A palette from a
// [State] is shared and must not be changed.
func (p *Palette) Set(i uint8, c color.NRGBA) {
	p.set[i>>6] |= 1 << (i & 63)
	p.colors[i] = c
}

// samePalette reports whether a and b have the same colors.
func samePalette(a, b *Palette) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

// paletteLocked returns the indexed colors the program set with OSC 4 and has
// not reset: those of the current palette that differ from the default one.
// It is nil when there are none, and t.palette while they are unchanged, so
// states share it.
func (t *Terminal) paletteLocked() *Palette {
	if t.paletteFresh {
		return t.palette
	}
	const n = 256 * wasmvt.SizeColorRgb
	cur, def := t.scratch, t.scratch+n
	// Both reads always succeed: a palette is never unset.
	if t.mod.Get(t.term, wasmvt.TerminalDataColorPalette, cur) != nil ||
		t.mod.Get(t.term, wasmvt.TerminalDataColorPaletteDefault, def) != nil {
		return t.palette
	}
	t.paletteFresh = true
	a, b := t.mod.Slice(cur, n), t.mod.Slice(def, n)
	if bytes.Equal(a, b) {
		t.palette = nil
		return nil
	}
	var next Palette
	for i := range 256 {
		c, d := a[i*3:i*3+3], b[i*3:i*3+3]
		if c[0] != d[0] || c[1] != d[1] || c[2] != d[2] {
			next.Set(uint8(i), color.NRGBA{R: c[0], G: c[1], B: c[2], A: 0xff})
		}
	}
	if t.palette == nil || *t.palette != next {
		t.palette = new(Palette)
		*t.palette = next
	}
	return t.palette
}
