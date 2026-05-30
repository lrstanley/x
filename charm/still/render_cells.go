// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	icol "github.com/lrstanley/x/charm/still/internal/color"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/internal/raster"
)

func (d *Renderer) drawBackground(dst draw.Image, f *renderFrame) {
	idraw.Fill(dst, f.imageBounds, icol.NRGBA(f.marginColor()))
	idraw.Fill(dst, f.windowBounds, f.windowBackgroundNRGBA())
}

func (d *Renderer) drawCellBg(dst draw.Image, area image.Rectangle, cell *uv.Cell, f *renderFrame) {
	idraw.Fill(dst, area, f.cellBackgroundNRGBA(cell))
}

func (d *Renderer) drawCellFg(dst draw.Image, area image.Rectangle, cell *uv.Cell, f *renderFrame, usePass bool) {
	d.drawCellFgColor(dst, area, cell, f, usePass, color.NRGBA{}, false)
}

func (d *Renderer) drawCellFgColor(dst draw.Image, area image.Rectangle, cell *uv.Cell, f *renderFrame, usePass bool, fg color.NRGBA, fgOK bool) {
	if !idraw.TextVisible(f, cell) {
		return
	}
	hasGlyph := cellHasForegroundGlyph(cell)
	hasDecorations := cellHasForegroundDecorations(cell)
	if !hasGlyph && !hasDecorations {
		return
	}
	if !fgOK {
		fg, _ = f.CellColorsNRGBA(cell)
	}
	var layout raster.Layout
	hasLayout := false
	if hasGlyph {
		hasLayout = raster.LayoutGlyph(f, area, cell, fg, &layout)
	}
	if usePass {
		if hasLayout {
			if layout.Kind == raster.KindGrid {
				d.fgPass.AccumulateGlyphLayout(f, &layout)
			} else {
				d.fgPass.AppendGlyph(layout)
			}
		}
		if hasDecorations {
			d.fgPass.AppendDecoration(area, cell, fg)
		}
		return
	}
	if hasLayout {
		raster.DrawGlyphLayout(f, dst, &layout)
	}
	if hasDecorations {
		idraw.Decorations(f, dst, area, cell, fg)
	}
}

func (d *Renderer) drawCursor(dst draw.Image, area image.Rectangle, f *renderFrame) {
	if !f.hasEmu {
		return
	}
	metrics := f.metrics

	switch f.emu.CursorStyle {
	case uv.CursorUnderline:
		area.Min.Y = max(area.Min.Y, area.Max.Y-metrics.CursorThickness.Int())
	case uv.CursorBar:
		area.Max.X = min(area.Max.X, area.Min.X+metrics.CursorThickness.Int())
	case uv.CursorBlock:
		height := min(metrics.CursorHeight.Int(), area.Dy())
		area.Min.Y += (area.Dy() - height) / 2
		area.Max.Y = area.Min.Y + height
	}

	idraw.Fill(dst, area, f.cursorColorNRGBA())
	if cursorCell := f.cursorCell; cursorCell != nil &&
		idraw.TextVisible(f, cursorCell) && cellHasForegroundGlyph(cursorCell) {
		raster.DrawGlyph(f, dst, f.cursorBounds(), cursorCell, f.cursorTextColorNRGBA())
	}
}

func (d *Renderer) drawScrollbar(dst draw.Image, area image.Rectangle, f *renderFrame) {
	screenRows := f.screenBounds.Dy()
	if !f.hasEmu {
		return
	}
	totalRows := f.emu.ScrollbackCount + screenRows
	if screenRows <= 0 || totalRows <= screenRows {
		return
	}

	fg := f.foregroundNRGBA(nil)
	bg := f.backgroundNRGBA(nil)
	track := icol.NRGBA(icol.Blend(bg, fg, 0.12))
	thumb := icol.NRGBA(icol.Blend(bg, fg, 0.35))
	idraw.Fill(dst, area, track)

	thumbHeight := max(1, area.Dy()*screenRows/totalRows)
	thumbHeight = min(thumbHeight, area.Dy())
	thumbArea := area
	thumbArea.Min.Y = area.Max.Y - thumbHeight
	idraw.Fill(dst, thumbArea, thumb)
}

func cellHasForegroundGlyph(cell *uv.Cell) bool {
	if cell == nil {
		return false
	}
	return cell.Content != "" && cell.Content != " "
}

func cellHasForegroundDecorations(cell *uv.Cell) bool {
	if cell == nil {
		return false
	}
	return cell.Style.Underline != uv.UnderlineNone || cell.Style.Attrs&uv.AttrStrikethrough != 0
}
