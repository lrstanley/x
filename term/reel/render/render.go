// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"time"

	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/effects"
	"github.com/lrstanley/x/term/reel/vt"
)

// minScrollbarWidthPx is the narrowest scrollbar that scales with the cell
// width, so the thumb stays visible at small scales.
const minScrollbarWidthPx = 2

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

// Layout is where a renderer places a screen in its image. Rectangles are
// relative to the image origin; [Renderer.Draw] into a destination at another
// origin offsets them by it.
type Layout struct {
	// Image is the whole image: the margin, the window with its title bar
	// and padding, the cell grid, and the scrollbar.
	Image image.Rectangle
	// Grid is the cell grid within Image.
	Grid image.Rectangle
	// Cell is the pixel size of one cell.
	Cell image.Point
	// Scale is the font scale, where 1 is the default font size.
	Scale float64
}

// CellRect is the pixel rectangle of the cell at column x and row y, both
// from 0, or the empty rectangle when that is outside the grid.
func (l Layout) CellRect(x, y int) image.Rectangle {
	at := l.Grid.Min.Add(image.Pt(x*l.Cell.X, y*l.Cell.Y))
	return image.Rectangle{Min: at, Max: at.Add(l.Cell)}.Intersect(l.Grid)
}

// Layout returns the layout of an image of a cols by rows screen. It depends
// only on the size and the renderer's options, not on the screen or state.
func (d *Renderer) Layout(cols, rows int) Layout {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.layout(cols, rows)
}

// layout is [Renderer.Layout] without the lock.
func (d *Renderer) layout(cols, rows int) Layout {
	cell := d.metrics.CellSize()
	margin, padding, chrome := d.opts.Margin.Int(), d.opts.Padding.Int(), d.chromeHeight()
	inset := 2 * (margin + padding)
	bar := d.scrollbarWidth()
	grid := image.Rectangle{Max: image.Pt(cols*cell.X, rows*cell.Y)}.
		Add(image.Pt(margin+padding, margin+chrome+padding))
	return Layout{
		Image: image.Rect(0, 0, grid.Dx()+bar+inset, grid.Dy()+chrome+inset),
		Grid:  grid,
		Cell:  cell,
		Scale: d.metrics.Scale,
	}
}

// scrollbarWidth is the width of the scrollbar in pixels, zero when there is
// none: [WithScrollbarWidth], or a third of a cell.
func (d *Renderer) scrollbarWidth() int {
	if !d.opts.Scrollbar {
		return 0
	}
	if w := d.opts.ScrollbarWidth.Int(); w > 0 {
		return w
	}
	return max(minScrollbarWidthPx, d.metrics.CellSize().X/3)
}

// Animates reports whether the indicators drawn for st move with the
// timeline: the OSC 9;4 progress bar when it is indeterminate or an error with
// no percent ([WithProgressBar]), and the OSC 7501 program status when its
// most urgent record is blocked or working with no progress
// ([WithProgramStatus]), and the bell while it shows at at ([WithBell]).
// [Renderer.Draw] then repaints them as they move, and a recorder that holds
// a frame from at can keep them moving with [Renderer.Repaint]. A blinking
// cursor or blinking text is not an indicator.
func (d *Renderer) Animates(st vt.State, at time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shownIndicators(st, at).clocked()
}

// Changes reports whether the indicators drawn for st may look different at
// offset to than at from. It is always true while they animate, and for a
// blocked program status or a fading bell only when its step differs, so a
// recorder that holds a frame can keep the image it repainted at from
// instead of calling [Renderer.Repaint] again. It is false for indicators
// that do not move with the timeline.
func (d *Renderer) Changes(st vt.State, from, to time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	was := d.shownIndicators(st, from)
	return was.animates() || was != d.shownIndicators(st, to)
}

// Repaint redraws only the parts of dst the indicators that moved between
// offsets from and at cover, for st as of at: the progress bar's rows, with
// program status, the title bar and a few pixels around the window edge,
// and with the bell, the title bar or, while it flashes, the whole window.
// dst must hold an image of scr with st that [Renderer.Draw] drew at
// dst.Rect.Min with the same renderer, as of from. Every other pixel is left
// alone. It costs a fraction of drawing the whole image, so a recorder that
// holds a frame can copy it and keep the indicators moving, see
// [Renderer.Animates]. It does nothing when none of them moved.
//
// The parts are drawn as of the same offset as the indicators, so a blinking
// cursor or blinking text that reaches into them, which can only be the
// outermost cells of the grid, may differ from the rest of the cell.
func (d *Renderer) Repaint(dst *image.NRGBA, scr vt.Screen, st vt.State, from, at time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	if dst == nil {
		panic("render: nil destination image")
	}
	f := d.borrowRenderFrame(dst.Rect.Min, scr, st, at)
	defer releaseRenderFrame(f)
	bands := f.repaintBands(d.shownIndicators(st, from), f.indicators())
	d.renderBands(dst, scr, f, bands[:]...)
}

// indicators are what the OSC 9;4 progress bar, the OSC 7501 program
// status, and the bell draw. The value is comparable, so the frame cache can
// tell when they change.
type indicators struct {
	progress vt.Progress
	status   statusView
	bell     bellView
}

// shownIndicators are the indicators drawn for st at at.
func (d *Renderer) shownIndicators(st vt.State, at time.Duration) indicators {
	status := d.shownStatus(st, at)
	return indicators{progress: d.shownProgress(st), status: status, bell: d.shownBell(st, at, status)}
}

// animates reports whether i looks different at every frame: the sweep of
// an indeterminate progress bar, or the spinner and comets of a working
// status. A frame that shows it is never reused.
func (i indicators) animates() bool {
	return progressAnimates(i.progress) || i.status.animates()
}

// clocked reports whether i moves with the clock at all: what animates, and
// a blocked status, which breathes, and a bell, which fades. Their steps are
// part of i, so a frame that shows them is reused until a step changes.
func (i indicators) clocked() bool {
	return i.animates() || i.status.breathes() || i.bell != bellView{}
}

// repaintBands are the parts of the image to repaint when the indicators
// drawn go from old to now: the progress bar's band when the bar changed or
// animates, the program status bands when the status changed or animates,
// and the bell's band. The others are empty. See [renderFrame.progressBand],
// [renderFrame.statusBands], and [renderFrame.bellBand].
func (f *renderFrame) repaintBands(old, now indicators) (b [6]image.Rectangle) {
	if old.progress != now.progress || progressAnimates(now.progress) {
		b[0] = f.progressBand()
	}
	if old.status != now.status || now.status.animates() {
		sb := f.statusBands()
		copy(b[1:], sb[:])
	}
	b[5] = f.bellBand(old.bell, now.bell)
	return b
}

// renderBands renders the parts of dst within bands from the background up,
// so effects that run after the cells (dimming, rounded mask, cursor,
// scrollbar, outline) apply to fresh pixels. Overlapping bands are each
// repainted whole, so the overlap comes out the same, and a band within
// another, such as any band within the window while the bell flashes, is
// skipped.
func (d *Renderer) renderBands(dst *image.NRGBA, scr vt.Screen, f *renderFrame, bands ...image.Rectangle) {
	for i, band := range bands {
		if band = band.Intersect(dst.Rect); band.Empty() || bandCovered(bands, i) {
			continue
		}
		sub, _ := dst.SubImage(band).(*image.NRGBA) // NRGBA.SubImage is always an NRGBA.
		d.render(sub, scr, f)
	}
}

// bandCovered reports whether bands[i] lies within another of bands. Of
// equal bands, the first is not covered.
func bandCovered(bands []image.Rectangle, i int) bool {
	for j, o := range bands {
		if j != i && !o.Empty() && bands[i].In(o) && (j < i || bands[i] != o) {
			return true
		}
	}
	return false
}

// render paints f's part of dst from the background up. Glyphs go through
// the foreground pass, which composites them once the cell backgrounds are
// down.
func (d *Renderer) render(dst *image.NRGBA, scr vt.Screen, f *renderFrame) {
	windowBg := d.drawBackground(dst, f)

	view := f.screenBounds
	gridMaxX := f.gridBounds.Max.X
	cellW := f.cellW
	cellH := f.cellH
	gridMin := f.gridBounds.Min

	d.fgPass.Reset(f.gridBounds.Union(f.chromeBounds))
	d.drawChrome(dst, f)
	d.drawImages(dst, f, layerBelowBackground)

	// Only rows and columns that can reach dst: glyphSpill more on each side,
	// and on the left, the rest of a wide cell that starts there.
	db, ch, cw := dst.Bounds(), max(cellH, 1), max(cellW, 1)
	r0 := max(0, (db.Min.Y-gridMin.Y)/ch-glyphSpill)
	r1 := min(view.Dy(), (db.Max.Y-gridMin.Y+ch-1)/ch+glyphSpill)
	c0 := max(0, (db.Min.X-gridMin.X)/cw-glyphSpill-(maxCellWidth-1))
	c1 := min(view.Dx(), (db.Max.X-gridMin.X+cw-1)/cw+glyphSpill)

	var cell *vt.Cell
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
				cell = &vt.EmptyCell
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
			if !cell.KittyPlaceholder() {
				d.drawCellFg(area, cell, f, fg, fgReady, cellSpansNext(scr, view, x, y, cell))
			}
		}
		if open {
			idraw.Fill(dst, image.Rect(runX0, rowY0, runX1, rowY1), runColor)
		}
	}

	d.drawImages(dst, f, layerBelowText)
	d.fgPass.Composite(dst)
	d.fgPass.DrawPending(f, dst)
	d.drawImages(dst, f, layerAboveText)
	d.images.rotate()
	d.drawPromptMarks(dst, f)

	if cur := f.st.Cursor; cur.Visible && idraw.CursorVisible(f, cur) {
		if cursor := f.cursorBounds(); !cursor.Empty() {
			d.drawCursor(dst, cursor, f)
		}
	}

	d.drawProgress(dst, f)

	if d.opts.Scrollbar && !f.st.AltScreen &&
		f.st.ScrollbackLen > 0 &&
		!f.scrollbarBounds.Empty() {
		d.drawScrollbar(dst, f.scrollbarBounds, f)
	}

	if !f.st.Focused {
		effects.ApplyFocusDimming(f, dst)
	}
	drawBellFlash(dst, f)

	effects.ApplyRoundedMask(f, dst)

	// The program status outline sits on the margin the rounded mask
	// restores, so it is drawn last.
	d.drawStatus(dst, f)
}
