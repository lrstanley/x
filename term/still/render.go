// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"time"

	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/internal/effects"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/vt/screen"
)

const scrollbarWidthPx = 1

const (
	// glyphSpill is how many cells past its own a glyph's ink can reach on
	// any side, so a render clipped to part of the image also draws the
	// cells that far outside it.
	glyphSpill = 1
	// maxCellWidth is the widest cell, in columns. A cell is drawn from its
	// first column, so a render that starts inside a wide cell starts that
	// many columns, less one, earlier.
	maxCellWidth = 2
)

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

// IndicatorsAnimate reports whether the indicators drawn for the current
// [EmulatorState] move with the [WithClock] time: the OSC 9;4 progress bar
// when it is indeterminate or an error with no percent ([WithProgressBar]),
// and the OSC 7501 program status when its most urgent record is blocked or
// working with no progress ([WithProgramStatus]). Draw then repaints them as
// they move, and a recorder that holds a frame can keep them moving with
// [Renderer.RepaintIndicators].
func (d *Renderer) IndicatorsAnimate() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shownIndicators(d.emulatorState, d.opts.Now()).clocked()
}

// RepaintIndicators redraws only the parts of dst the indicators that move
// with the clock cover, for the current [EmulatorState] and [WithClock] time:
// the progress bar's rows, and with program status, the title bar and a few
// pixels around the window edge. dst must be an image of scr that
// [Renderer.DrawInto] painted with the renderer's current options and
// [EmulatorState], at any time. Every other pixel is left alone. It costs a
// fraction of drawing the whole image, so a recorder that holds a frame can
// copy it and keep the indicators moving, see [Renderer.IndicatorsAnimate].
// It does nothing when none of them move.
//
// The parts are drawn as of the same clock as the indicators, so a blinking
// cursor or blinking text that reaches into them, which can only be the
// outermost cells of the grid, may differ from the rest of the cell.
func (d *Renderer) RepaintIndicators(dst *image.NRGBA, scr screen.Screen) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	f := d.borrowRenderFrame(image.Point{}, scr, d.opts.Now())
	defer releaseRenderFrame(f)
	now := f.indicators()
	bands := f.repaintBands(now.sansClock(), now)
	d.renderBands(dst, scr, f, bands[:]...)
}

// indicators are what the OSC 9;4 progress bar and the OSC 7501 program
// status draw. The value is comparable, so the frame cache can tell when they
// change.
type indicators struct {
	progress types.Progress
	status   statusView
}

// shownIndicators are the indicators drawn for st at now.
func (d *Renderer) shownIndicators(st *EmulatorState, now time.Time) indicators {
	return indicators{progress: d.shownProgress(st), status: d.shownStatus(st, now)}
}

// animates reports whether i looks different at every frame: the sweep of
// an indeterminate progress bar, or the spinner and comets of a working
// status. A frame that shows it is never reused.
func (i indicators) animates() bool {
	return progressAnimates(i.progress) || i.status.animates()
}

// clocked reports whether i moves with the clock at all: what animates, and
// a blocked status, which breathes. Its breath step is part of i, so a frame
// that shows it is reused until the step changes.
func (i indicators) clocked() bool {
	return i.animates() || i.status.breathes()
}

// sansClock is i as drawn at an unknown time, which no breath step matches,
// so [renderFrame.repaintBands] from it repaints everything that moves with
// the clock.
func (i indicators) sansClock() indicators {
	if i.status.breathes() {
		i.status.breath = -1
	}
	return i
}

// repaintBands are the parts of the image to repaint when the indicators
// drawn go from old to now: the progress bar's band when the bar changed or
// animates, and the program status bands when the status changed or animates.
// The others are empty. See [renderFrame.progressBand] and
// [renderFrame.statusBands].
func (f *renderFrame) repaintBands(old, now indicators) (b [5]image.Rectangle) {
	if old.progress != now.progress || progressAnimates(now.progress) {
		b[0] = f.progressBand()
	}
	if old.status != now.status || now.status.animates() {
		sb := f.statusBands()
		copy(b[1:], sb[:])
	}
	return b
}

// renderBands renders the parts of dst within bands from the background up,
// so effects that run after the cells (dimming, rounded mask, cursor,
// scrollbar, outline) apply to fresh pixels. Overlapping bands are each
// repainted whole, so the overlap comes out the same.
func (d *Renderer) renderBands(dst *image.NRGBA, scr screen.Screen, f *renderFrame, bands ...image.Rectangle) {
	for _, band := range bands {
		if band = band.Intersect(dst.Rect); band.Empty() {
			continue
		}
		sub, _ := dst.SubImage(band).(*image.NRGBA) // NRGBA.SubImage is always an NRGBA.
		d.render(sub, scr, f)
	}
}

func (d *Renderer) drawIntoLocked(dst draw.Image, area image.Rectangle, scr screen.Screen) {
	if dst == nil {
		panic("nil destination image")
	}

	origin := area.Min
	if area.Empty() {
		origin = dst.Bounds().Min
	}

	f := d.borrowRenderFrame(origin, scr, d.opts.Now())
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
		d.fgPass.Reset(f.gridBounds.Union(f.chromeBounds))
	}
	d.drawChrome(dst, f, usePass)

	// Only rows and columns that can reach dst: glyphSpill more on each side,
	// and on the left, the rest of a wide cell that starts there.
	db, ch, cw := dst.Bounds(), max(cellH, 1), max(cellW, 1)
	r0 := max(0, (db.Min.Y-gridMin.Y)/ch-glyphSpill)
	r1 := min(view.Dy(), (db.Max.Y-gridMin.Y+ch-1)/ch+glyphSpill)
	c0 := max(0, (db.Min.X-gridMin.X)/cw-glyphSpill-(maxCellWidth-1))
	c1 := min(view.Dx(), (db.Max.X-gridMin.X+cw-1)/cw+glyphSpill)

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
		for x := view.Min.X + c0; x < view.Min.X+c1; x++ {
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
		for x := view.Min.X + c0; x < view.Min.X+c1; x++ {
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

	d.drawProgress(dst, f)

	if d.opts.Scrollbar && f.hasEmu && !f.emu.AltScreen &&
		f.emu.ScrollbackCount > view.Dy() &&
		!f.scrollbarBounds.Empty() {
		d.drawScrollbar(dst, f.scrollbarBounds, f)
	}

	if f.hasEmu && !f.emu.Focused {
		effects.ApplyFocusDimming(f, dst)
	}

	effects.ApplyRoundedMask(f, dst)

	// The program status outline sits on the margin the rounded mask
	// restores, so it is drawn last.
	d.drawStatus(dst, f)
}
