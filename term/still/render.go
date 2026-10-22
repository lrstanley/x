// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"

	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/internal/effects"
	"github.com/lrstanley/x/term/vt/screen"
)

const scrollbarWidthPx = 1

// DrawInto draws scr into dst within area.
func (d *Renderer) DrawInto(dst draw.Image, area image.Rectangle, scr screen.Screen) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	d.drawIntoLocked(dst, area, scr)
}

// Size returns the pixel size required to draw scr.
func (d *Renderer) Size(scr screen.Screen) image.Point {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.buildFrame(image.Point{}, scr).size()
}

// Bounds returns the pixel bounds required to draw scr at the origin.
func (d *Renderer) Bounds(scr screen.Screen) image.Rectangle {
	return image.Rectangle{Max: d.Size(scr)}
}

// CellSize returns the current terminal cell size.
func (d *Renderer) CellSize() image.Point {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.metrics.CellSize()
}

func (d *Renderer) drawIntoLocked(dst draw.Image, area image.Rectangle, scr screen.Screen) {
	if dst == nil {
		panic("nil destination image")
	}

	origin := area.Min
	if area.Empty() {
		origin = dst.Bounds().Min
	}

	f := d.borrowRenderFrame(origin, scr)
	defer releaseRenderFrame(f)
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

func (d *Renderer) render(dst draw.Image, scr screen.Screen, f *renderFrame) {
	windowBg := d.drawBackground(dst, f)

	view := f.screenBounds
	gridMaxX := f.gridBounds.Max.X
	cellW := f.cellW
	cellH := f.cellH
	gridMin := f.gridBounds.Min

	frame, usePass := dst.(*image.NRGBA)
	if usePass {
		d.fgPass.Reset(f.gridBounds)
	}

	// Only rows that can reach dst: one extra row each side for glyph spill.
	db, ch := dst.Bounds(), max(cellH, 1)
	r0 := max(0, (db.Min.Y-gridMin.Y)/ch-1)
	r1 := min(view.Dy(), (db.Max.Y-gridMin.Y+ch-1)/ch+1)

	var cell *screen.Cell
	for y := view.Min.Y + r0; y < view.Min.Y+r1; y++ {
		row := y - view.Min.Y
		rowY0 := gridMin.Y + row*cellH
		rowY1 := rowY0 + cellH

		// Consecutive explicit backgrounds that still need paint and share a
		// stored color become one Fill. Width-0 spacers are skipped and do
		// not end a run; a cell that does not need a fill does. A run extends
		// only when the next span starts where the previous one ended, so a
		// wide cell contributes its full pixel width and gaps stay unpainted.
		var (
			open     bool
			runX0    int
			runX1    int
			runColor color.NRGBA
		)
		for x := view.Min.X; x < view.Max.X; x++ {
			cell = scr.CellAt(x, y)
			if cell == nil {
				cell = &screen.EmptyCell
			}
			if cell.Width == 0 {
				continue
			}
			col := x - view.Min.X
			area := cellDrawArea(gridMin, gridMaxX, cellW, cellH, col, row, cell.Width)
			fg, fgReady, fill, need := cellBackgroundFill(f, cell, area, windowBg)
			if !need {
				if open {
					idraw.Fill(dst, image.Rect(runX0, rowY0, runX1, rowY1), runColor)
					open = false
				}
			} else if open && fill == runColor && area.Min.X == runX1 {
				runX1 = area.Max.X
			} else {
				if open {
					idraw.Fill(dst, image.Rect(runX0, rowY0, runX1, rowY1), runColor)
				}
				open = true
				runX0 = area.Min.X
				runX1 = area.Max.X
				runColor = fill
			}
			if !usePass {
				continue
			}
			if fgReady {
				d.drawCellFgColor(dst, area, cell, f, true, fg, true)
				continue
			}
			d.drawCellFg(dst, area, cell, f, true)
		}
		if open {
			idraw.Fill(dst, image.Rect(runX0, rowY0, runX1, rowY1), runColor)
		}
		if usePass {
			continue
		}
		for x := view.Min.X; x < view.Max.X; x++ {
			cell = scr.CellAt(x, y)
			if cell == nil {
				cell = &screen.EmptyCell
			}
			if cell.Width == 0 {
				continue
			}
			col := x - view.Min.X
			area := cellDrawArea(gridMin, gridMaxX, cellW, cellH, col, row, cell.Width)
			d.drawCellFg(dst, area, cell, f, false)
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
		f.emu.ScrollbackCount > view.Dy() &&
		!f.scrollbarBounds.Empty() {
		d.drawScrollbar(dst, f.scrollbarBounds, f)
	}

	if f.hasEmu && !f.emu.Focused {
		effects.ApplyFocusDimming(f, dst)
	}

	effects.ApplyRoundedMask(f, dst)
}
