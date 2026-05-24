// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	icol "github.com/lrstanley/x/charm/still/internal/color"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/internal/raster"
)

// CellDrawer draws a cell (background or foreground). area is the cell bounding box.
type CellDrawer func(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell)

// CursorDrawer draws the cursor to the given image, where area is the bounding box of
// the cursor.
type CursorDrawer func(ctx Context, img draw.Image, area image.Rectangle)

// ScrollbarDrawer draws the scrollbar to the given image, where area is the bounding
// box of the scrollbar.
type ScrollbarDrawer func(ctx Context, img draw.Image, area image.Rectangle)

// BackgroundDrawer draws the background to the given image, where area is the bounding
// box of the background.
type BackgroundDrawer func(ctx Context, img draw.Image, area image.Rectangle)

// DrawCellBg fills the cell background.
func DrawCellBg(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell) {
	if !icol.CellHasExplicitBackground(cell) &&
		(cell == nil || cell.Style.Attrs&uv.AttrReverse == 0) {
		return
	}
	idraw.Fill(img, area, ctx.CellBackgroundColor(cell))
}

// DrawCellFg draws cell text and decorations.
func DrawCellFg(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell) {
	if !idraw.TextVisible(ctx.Snapshot(), cell) {
		return
	}
	fg, _ := ctx.CellColors(cell)
	layout, hasLayout := raster.LayoutGlyph(ctx, area, cell, fg)
	if pass := ctx.glyphPass; pass != nil {
		if hasLayout {
			if layout.Kind == raster.KindGrid {
				raster.AccumulateGlyphLayout(ctx, pass.Coverage(), layout)
			} else {
				pass.AppendGlyph(layout)
			}
		}
		pass.AppendDecoration(area, cell, fg)
		return
	}
	if hasLayout {
		raster.DrawGlyphLayout(ctx, img, layout)
	}
	idraw.Decorations(ctx, img, area, cell, fg)
}

// DrawCursor is the default implementation of [CursorDrawer] used by [Renderer].
func DrawCursor(ctx Context, img draw.Image, area image.Rectangle) {
	state := ctx.EmulatorState()
	metrics := ctx.Metrics()

	switch state.CursorStyle {
	case uv.CursorUnderline:
		area.Min.Y = max(area.Min.Y, area.Max.Y-metrics.CursorThickness.Int())
	case uv.CursorBar:
		area.Max.X = min(area.Max.X, area.Min.X+metrics.CursorThickness.Int())
	case uv.CursorBlock:
		height := min(metrics.CursorHeight.Int(), area.Dy())
		area.Min.Y += (area.Dy() - height) / 2
		area.Max.Y = area.Min.Y + height
	}

	idraw.Fill(img, area, ctx.CursorColor())
	if cursorCell := ctx.CursorCell(); cursorCell != nil && idraw.TextVisible(ctx.Snapshot(), cursorCell) {
		raster.DrawGlyph(ctx, img, ctx.CursorBounds(), cursorCell, ctx.CursorTextColor())
	}
}

// DrawScrollbar is the default implementation of [ScrollbarDrawer] used by [Renderer].
func DrawScrollbar(ctx Context, img draw.Image, area image.Rectangle) {
	screenRows := ctx.ScreenBounds().Dy()
	totalRows := ctx.EmulatorState().ScrollbackCount + screenRows
	if screenRows <= 0 || totalRows <= screenRows {
		return
	}

	fg := ctx.ForegroundColor()
	bg := ctx.BackgroundColor()
	track := icol.Blend(bg, fg, 0.12)
	thumb := icol.Blend(bg, fg, 0.35)
	idraw.Fill(img, area, track)

	thumbHeight := max(1, area.Dy()*screenRows/totalRows)
	thumbHeight = min(thumbHeight, area.Dy())
	thumbArea := area
	thumbArea.Min.Y = area.Max.Y - thumbHeight
	idraw.Fill(img, thumbArea, thumb)
}

// DrawBackground is the default implementation of [BackgroundDrawer] used by [Renderer].
func DrawBackground(ctx Context, img draw.Image, area image.Rectangle) {
	idraw.Fill(img, area, ctx.MarginColor())
	idraw.Fill(img, ctx.WindowBounds(), ctx.WindowBackgroundColor())
}
