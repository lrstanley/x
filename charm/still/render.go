// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/internal/effects"
)

const scrollbarWidthPx = 1

// DrawInto draws scr into dst within area.
func (d *Renderer) DrawInto(dst draw.Image, area image.Rectangle, scr uv.Screen) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	d.drawIntoLocked(dst, area, scr)
}

// Size returns the pixel size required to draw scr.
func (d *Renderer) Size(scr uv.Screen) image.Point {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.buildFrame(image.Point{}, scr).size()
}

// Bounds returns the pixel bounds required to draw scr at the origin.
func (d *Renderer) Bounds(scr uv.Screen) image.Rectangle {
	return image.Rectangle{Max: d.Size(scr)}
}

// CellSize returns the current terminal cell size.
func (d *Renderer) CellSize() image.Point {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.metrics.CellSize()
}

func (d *Renderer) drawIntoLocked(dst draw.Image, area image.Rectangle, scr uv.Screen) {
	if dst == nil {
		panic("nil destination image")
	}

	origin := area.Min
	if area.Empty() {
		origin = dst.Bounds().Min
	}

	f := d.buildFrame(origin, scr)
	required := f.imageBounds
	if area.Empty() {
		area = required
		dstBounds := dst.Bounds()
		if dstBounds.Dx() < area.Dx() || dstBounds.Dy() < area.Dy() {
			panic("destination bounds too small for rendering")
		}
	} else if area.Dx() < required.Dx() || area.Dy() < required.Dy() {
		panic("target area is too small")
	}

	d.render(dst, scr, f)
}

func (d *Renderer) render(dst draw.Image, scr uv.Screen, f *renderFrame) {
	d.drawBackground(dst, f)

	screen := f.screenBounds
	gridMaxX := f.gridBounds.Max.X
	cellW := f.cellW
	cellH := f.cellH
	gridMin := f.gridBounds.Min

	frame, usePass := dst.(*image.NRGBA)
	if usePass {
		d.fgPass.Reset(f.gridBounds)
	}

	var cell *uv.Cell
	var area image.Rectangle
	for y := screen.Min.Y; y < screen.Max.Y; y++ {
		row := y - screen.Min.Y
		cellMinY := gridMin.Y + row*cellH
		cellMaxY := cellMinY + cellH
		for x := screen.Min.X; x < screen.Max.X; x++ {
			cell = scr.CellAt(x, y)
			if cell == nil {
				cell = &uv.EmptyCell
			}
			if cell.Width == 0 {
				continue
			}
			col := x - screen.Min.X
			cellMinX := gridMin.X + col*cellW
			cellMaxX := cellMinX + cellW
			if cell.Width > 1 {
				cellMaxX = min(gridMaxX, cellMinX+cell.Width*cellW)
			}
			area = image.Rect(cellMinX, cellMinY, cellMaxX, cellMaxY)

			d.drawCellBg(dst, area, cell, f)
			d.drawCellFg(dst, area, cell, f, usePass)
		}
	}

	if usePass {
		d.fgPass.Composite(frame)
		d.fgPass.DrawPending(f, dst)
	}

	if f.hasEmu && f.emu.CursorVisible && idraw.CursorVisible(f, f.emu, f.hasEmu) {
		if cursor := f.cursorBounds(); !cursor.Empty() {
			d.drawCursor(dst, cursor, f)
		}
	}

	if d.opts.Scrollbar && f.hasEmu && !f.emu.AltScreen &&
		f.emu.ScrollbackCount > screen.Dy() &&
		!f.scrollbarBounds.Empty() {
		d.drawScrollbar(dst, f.scrollbarBounds, f)
	}

	if f.hasEmu && !f.emu.Focused {
		effects.ApplyFocusDimming(f, dst)
	}

	effects.ApplyRoundedMask(f, dst)
}
