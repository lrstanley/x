// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image/color"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	"github.com/lrstanley/x/term/reel/vt"
)

// ResolveColor resolves a terminal color with the renderer's palette and
// default-color policy. Background selects the default role when c is nil.
func ResolveColor(state vt.State, palette Palette, c color.Color, background bool) color.Color {
	src := colorSource{state: state, palette: palette}
	style := &vt.Style{Fg: c, Bg: c}
	if background {
		return icol.ResolveBackgroundNRGBA(src, style)
	}
	return icol.ResolveForegroundNRGBA(src, style)
}

type colorSource struct {
	state   vt.State
	palette Palette
}

func (s colorSource) State() vt.State            { return s.state }
func (s colorSource) Palette() Palette           { return s.palette }
func (colorSource) FaintFactor() float64         { return 0.5 }
func (colorSource) BackgroundOpacity() float64   { return 1 }
func (colorSource) BackgroundOpacityCells() bool { return false }
