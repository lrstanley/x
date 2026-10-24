// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"
	"image/draw"

	icol "github.com/lrstanley/x/term/still/internal/color"
	"github.com/lrstanley/x/term/still/internal/config"
	"github.com/lrstanley/x/term/still/units"
	"github.com/lrstanley/x/term/vt/screen"
)

// Decorations renders underline, strikethrough, and related lines for a cell.
func Decorations(ctx config.CellFrameSource, img draw.Image, area image.Rectangle, cell *screen.Cell, fg color.NRGBA) {
	if cell == nil {
		return
	}
	style := cell.Style
	decoration := fg
	if style.UnderlineColor != nil && style.Attrs&screen.AttrConceal == 0 {
		decoration = icol.NRGBA(ctx.ResolvePaletteColor(style.UnderlineColor))
		if style.Attrs&screen.AttrFaint != 0 {
			_, bg := ctx.CellColorsNRGBA(cell)
			decoration = icol.BlendNRGBA(decoration, bg, ctx.FaintFactor())
		}
	}

	if style.Underline != screen.UnderlineNone {
		Underline(ctx, img, area, style.Underline, decoration)
	}
	if style.Attrs&screen.AttrStrikethrough != 0 {
		metrics := ctx.Metrics()
		y := units.Clamp(area.Min.Y+metrics.StrikethroughPosition.Int(), area.Min.Y, area.Max.Y-1)
		Fill(img, image.Rect(area.Min.X, y, area.Max.X, min(area.Max.Y, y+metrics.StrikethroughThickness.Int())), decoration)
	}
}

func periodicStart(start, period int) int {
	if period <= 0 {
		return start
	}
	if rem := start % period; rem != 0 {
		return start + period - rem
	}
	return start
}

// Underline paints the requested underline style at metrics-derived Y and thickness.
func Underline(ctx config.CellFrameSource, img draw.Image, area image.Rectangle, style screen.Underline, c color.NRGBA) {
	metrics := ctx.Metrics()
	y := units.Clamp(area.Min.Y+metrics.UnderlinePosition.Int(), area.Min.Y, area.Max.Y-1)
	thickness := metrics.UnderlineThickness.Int()
	clipX := ctx.GridBounds().Max.X
	switch style {
	case screen.UnderlineDouble:
		Fill(img, image.Rect(area.Min.X, y, area.Max.X, min(area.Max.Y, y+thickness)), c)
		y2 := min(area.Max.Y-1, y+thickness*2)
		Fill(img, image.Rect(area.Min.X, y2, area.Max.X, min(area.Max.Y, y2+thickness)), c)
	case screen.UnderlineDotted:
		step := max(2, thickness*2)
		for x := periodicStart(area.Min.X, step); x < area.Max.X; x += step {
			Fill(img, image.Rect(x, y, min(clipX, x+thickness), min(area.Max.Y, y+thickness)), c)
		}
	case screen.UnderlineDashed:
		dash := max(2, area.Dx()/3)
		gap := max(1, thickness*2)
		period := dash + gap
		for x := area.Min.X; x < area.Max.X; {
			phase := x % period
			if phase < dash {
				end := min(clipX, x+dash-phase)
				Fill(img, image.Rect(x, y, end, min(area.Max.Y, y+thickness)), c)
				x = end
				continue
			}
			x += period - phase
		}
	case screen.UnderlineCurly:
		pat := [8]int{0, 0, 1, 1, 0, 0, -1, -1}
		for x := area.Min.X; x < area.Max.X; x++ {
			yy := units.Clamp(y+pat[x%8], area.Min.Y, area.Max.Y-1)
			Fill(img, image.Rect(x, yy, x+1, min(area.Max.Y, yy+thickness)), c)
		}
	default:
		Fill(img, image.Rect(area.Min.X, y, area.Max.X, min(area.Max.Y, y+thickness)), c)
	}
}
