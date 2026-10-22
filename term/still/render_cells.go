// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"

	icol "github.com/lrstanley/x/term/still/internal/color"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/internal/raster"
	"github.com/lrstanley/x/term/vt/screen"
)

// drawBackground paints the margin and the window. Equal rectangles or equal
// stored colors are one fill; a margin frame whose color differs is painted
// first, then the window. The returned color is what windowBounds holds.
func (d *Renderer) drawBackground(dst draw.Image, f *renderFrame) color.NRGBA {
	margin := icol.NRGBA(f.marginColor())
	window := f.windowBackgroundNRGBA()
	if f.imageBounds.Eq(f.windowBounds) || margin == window {
		idraw.Fill(dst, f.imageBounds, window)
		return window
	}
	idraw.Fill(dst, f.imageBounds, margin)
	idraw.Fill(dst, f.windowBounds, window)
	return window
}

// cellBackgroundFill reports the foreground already resolved for a visible
// glyph or decoration, the cell fill that would be stored, and whether that
// fill still needs to be painted. Cells inside the window whose fill matches
// the color drawBackground stored there are skipped.
func cellBackgroundFill(f *renderFrame, cell *screen.Cell, area image.Rectangle, windowBg color.NRGBA) (fg color.NRGBA, fgReady bool, fill color.NRGBA, need bool) {
	if !icol.CellHasExplicitBackground(cell) {
		return color.NRGBA{}, false, color.NRGBA{}, false
	}
	if idraw.TextVisible(f, cell) && (cellHasForegroundGlyph(cell) || cellHasForegroundDecorations(cell)) {
		var bg color.NRGBA
		fg, bg = f.CellColorsNRGBA(cell)
		fgReady = true
		fill = f.cellBackgroundFromResolved(cell, bg)
	} else {
		fill = f.cellBackgroundNRGBA(cell)
	}
	return fg, fgReady, fill, !area.In(f.windowBounds) || fill != windowBg
}

func cellDrawArea(gridMin image.Point, gridMaxX, cellW, cellH, col, row, width int) image.Rectangle {
	minX := gridMin.X + col*cellW
	maxX := minX + cellW
	if width > 1 {
		maxX = min(gridMaxX, minX+width*cellW)
	}
	minY := gridMin.Y + row*cellH
	return image.Rect(minX, minY, maxX, minY+cellH)
}

func (d *Renderer) drawCellFg(dst draw.Image, area image.Rectangle, cell *screen.Cell, f *renderFrame, usePass bool) {
	d.drawCellFgColor(dst, area, cell, f, usePass, color.NRGBA{}, false)
}

func (d *Renderer) drawCellFgColor(dst draw.Image, area image.Rectangle, cell *screen.Cell, f *renderFrame, usePass bool, fg color.NRGBA, fgOK bool) {
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
			d.fgPass.AddGlyph(f, &layout)
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
	case screen.CursorUnderline:
		area.Min.Y = max(area.Min.Y, area.Max.Y-metrics.CursorThickness.Int())
	case screen.CursorBar:
		area.Max.X = min(area.Max.X, area.Min.X+metrics.CursorThickness.Int())
	case screen.CursorBlock:
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

func cellHasForegroundGlyph(cell *screen.Cell) bool {
	if cell == nil {
		return false
	}
	return cell.Content != "" && cell.Content != " "
}

func cellHasForegroundDecorations(cell *screen.Cell) bool {
	if cell == nil {
		return false
	}
	return cell.Style.Underline != screen.UnderlineNone || cell.Style.Attrs&screen.AttrStrikethrough != 0
}
