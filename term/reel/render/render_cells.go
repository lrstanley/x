// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"image/draw"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/raster"
	"github.com/lrstanley/x/term/reel/vt"
)

// drawBackground paints the margin and the window. Equal rectangles or equal
// stored colors are one fill; a margin frame whose color differs is painted
// first, then the window. The returned color is what windowBounds holds.
func (d *Renderer) drawBackground(dst draw.Image, f *renderFrame) color.NRGBA {
	margin := icol.NRGBA(f.MarginColor())
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
func cellBackgroundFill(f *renderFrame, cell *vt.Cell, area image.Rectangle, windowBg color.NRGBA) (fg color.NRGBA, fgReady bool, fill color.NRGBA, need bool) {
	if !icol.CellHasExplicitBackground(cell) {
		return color.NRGBA{}, false, color.NRGBA{}, false
	}
	if idraw.TextVisible(f, cell) && (cellHasForegroundGlyph(cell) || idraw.HasDecorations(f, cell)) {
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

// cellSpansNext reports whether the icon in the cell at x, y may grow into the
// cell after it. A change to the next cell changes how the icon draws, but it
// is in the same row, which the incremental draw repaints whole.
func cellSpansNext(scr vt.Screen, view image.Rectangle, x, y int, cell *vt.Cell) bool {
	if cell.Width != 1 || x+1 >= view.Max.X || !raster.IsSpanIcon(cell.Content) {
		return false
	}
	// A missing cell reads as an empty one.
	var prev, next string
	if x > view.Min.X {
		if c := scr.CellAt(x-1, y); c != nil {
			prev = c.Content
		}
	}
	if c := scr.CellAt(x+1, y); c != nil {
		next = c.Content
	}
	return raster.IconRoom(prev, next)
}

// drawCellFg adds the glyph and decorations of cell, in area, to the
// foreground pass. fg is their color when fgOK; otherwise it is resolved
// here, once the cell turns out to draw anything.
func (d *Renderer) drawCellFg(area image.Rectangle, cell *vt.Cell, f *renderFrame, fg color.NRGBA, fgOK, spansNext bool) {
	if !idraw.TextVisible(f, cell) {
		return
	}
	hasGlyph := cellHasForegroundGlyph(cell)
	hasDecorations := idraw.HasDecorations(f, cell)
	if !hasGlyph && !hasDecorations {
		return
	}
	if !fgOK {
		fg, _ = f.CellColorsNRGBA(cell)
	}
	var layout raster.Layout
	hasLayout := false
	if hasGlyph {
		hasLayout = raster.LayoutGlyph(f, area, cell, spansNext, fg, &layout)
	}
	if hasLayout {
		d.fgPass.AddGlyph(f, &layout)
	}
	if hasDecorations {
		d.fgPass.AppendDecoration(area, cell, fg)
	}
}

func (d *Renderer) drawCursor(dst draw.Image, area image.Rectangle, f *renderFrame) {
	metrics := f.metrics

	switch f.st.Cursor.Shape {
	case vt.CursorUnderline:
		area.Min.Y = max(area.Min.Y, area.Max.Y-metrics.CursorThickness.Int())
	case vt.CursorBar:
		area.Max.X = min(area.Max.X, area.Min.X+metrics.CursorThickness.Int())
	case vt.CursorBlock:
		height := min(metrics.CursorHeight.Int(), area.Dy())
		area.Min.Y += (area.Dy() - height) / 2
		area.Max.Y = area.Min.Y + height
	case vt.CursorBlockHollow:
		// Drawn as the whole cell.
	}

	idraw.Fill(dst, area, f.cursorColorNRGBA())
	if cursorCell := f.cursorCell; cursorCell != nil &&
		idraw.TextVisible(f, cursorCell) && cellHasForegroundGlyph(cursorCell) {
		raster.DrawGlyph(f, dst, f.cursorBounds(), cursorCell, f.cursorTextColorNRGBA())
	}
}

// drawScrollbar draws the track and the thumb in area, for a state with
// scrollback.
func (d *Renderer) drawScrollbar(dst draw.Image, area image.Rectangle, f *renderFrame) {
	screenRows := f.screenBounds.Dy()
	totalRows := f.st.ScrollbackLen + screenRows

	fg := f.foregroundNRGBA(nil)
	bg := f.backgroundNRGBA(nil)
	track := icol.BlendNRGBA(bg, fg, 0.12)
	thumb := icol.BlendNRGBA(bg, fg, 0.35)
	idraw.Fill(dst, area, track)

	thumbHeight := max(1, area.Dy()*screenRows/totalRows)
	thumbHeight = min(thumbHeight, area.Dy())
	// The thumb's top is the viewport's first row, as a share of all rows,
	// kept so the thumb stays inside the track.
	scrolled := max(0, min(f.st.ScrollbackOffset, f.st.ScrollbackLen))
	top := (area.Dy() - thumbHeight) * (f.st.ScrollbackLen - scrolled) / f.st.ScrollbackLen
	thumbArea := area
	thumbArea.Min.Y = area.Min.Y + top
	thumbArea.Max.Y = thumbArea.Min.Y + thumbHeight
	idraw.Fill(dst, thumbArea, thumb)
}

func cellHasForegroundGlyph(cell *vt.Cell) bool {
	if cell == nil {
		return false
	}
	return cell.Content != "" && cell.Content != " "
}
