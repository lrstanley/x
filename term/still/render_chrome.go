// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/charmbracelet/x/ansi"
	icol "github.com/lrstanley/x/term/still/internal/color"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/vt/screen"
)

// How far the title bar, its bottom edge, and the title text blend from the
// window background toward the foreground.
const (
	chromeBarTint      = 0.08
	chromeHairlineTint = 0.16
	chromeTitleTint    = 0.7
)

// chromeButtons are the window button colors, left to right.
var chromeButtons = [...]*image.Uniform{
	image.NewUniform(color.NRGBA{R: 0xff, G: 0x5f, B: 0x57, A: 0xff}),
	image.NewUniform(color.NRGBA{R: 0xfe, G: 0xbc, B: 0x2e, A: 0xff}),
	image.NewUniform(color.NRGBA{R: 0x28, G: 0xc8, B: 0x40, A: 0xff}),
}

// drawChrome paints the title bar. The title is laid out as one row of cells,
// so it uses the same fonts, fallbacks, and wide-cell handling as the grid,
// and joins the foreground pass when there is one so glyph masks are cached.
func (d *Renderer) drawChrome(dst draw.Image, f *renderFrame, usePass bool) {
	bar := f.chromeBounds
	if !bar.Overlaps(dst.Bounds()) {
		return
	}
	fg, bg := f.foregroundNRGBA(nil), f.backgroundNRGBA(nil)
	tint := func(k float64) color.NRGBA {
		return icol.ApplyAlphaNRGBA(icol.NRGBA(icol.Blend(bg, fg, k)), f.opts.BackgroundOpacity)
	}

	body := bar
	body.Max.Y-- // the hairline
	idraw.Fill(dst, bar, tint(chromeBarTint))
	idraw.Fill(dst, image.Rectangle{Min: image.Pt(bar.Min.X, body.Max.Y), Max: bar.Max}, tint(chromeHairlineTint))

	diameter := body.Dy() / 3
	if d.chromeMask == nil || d.chromeMask.Rect.Dx() != diameter {
		d.chromeMask = idraw.CircleMask(diameter)
	}
	top := body.Min.Y + (body.Dy()-diameter)/2
	x := body.Min.X + body.Dy()/2
	for _, src := range chromeButtons {
		dr := image.Rect(x, top, x+diameter, top+diameter)
		draw.DrawMask(dst, dr, src, image.Point{}, d.chromeMask, image.Point{}, draw.Over)
		x += 2 * diameter
	}

	// The title is centered in the room the buttons leave on both sides.
	if !f.hasEmu {
		return
	}
	room := body.Dx() - 2*(x-body.Min.X)
	title := ansi.Truncate(f.emu.Title, room/f.cellW, "…")
	cell := screen.Cell{Style: screen.Style{Fg: icol.NRGBA(icol.Blend(bg, fg, chromeTitleTint))}}
	x = body.Min.X + (body.Dx()-ansi.StringWidth(title)*f.cellW)/2
	y := body.Min.Y + (body.Dy()-f.cellH)/2
	var state byte
	for title != "" {
		var n int
		cell.Content, cell.Width, n, state = ansi.DecodeSequence(title, state, nil)
		title = title[n:]
		if cell.Width == 0 {
			continue
		}
		area := image.Rect(x, y, x+cell.Width*f.cellW, y+f.cellH)
		d.drawCellFg(dst, area, &cell, f, usePass)
		x = area.Max.X
	}
}
