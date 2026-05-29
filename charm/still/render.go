// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/draw"
	"iter"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
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

	return d.contextLocked(image.Point{}, scr).Size()
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

	d.drawIntoContextLocked(d.contextLocked(origin, scr), dst, area, scr)
}

func (d *Renderer) drawIntoContextLocked(ctx Context, dst draw.Image, area image.Rectangle, scr uv.Screen) {
	required := ctx.ImageBounds()
	if area.Empty() {
		area = required
		dstBounds := dst.Bounds()
		if dstBounds.Dx() < area.Dx() || dstBounds.Dy() < area.Dy() {
			panic("destination bounds too small for rendering")
		}
	} else if area.Dx() < required.Dx() || area.Dy() < required.Dy() {
		panic("target area is too small")
	}

	d.renderLocked(ctx, dst, scr)
}

func (d *Renderer) renderLocked(ctx Context, dst draw.Image, scr uv.Screen) {
	if d.hooks.backgroundDrawer != nil {
		d.hooks.backgroundDrawer(ctx, dst, ctx.ImageBounds())
	}

	for area, cell := range iterCells(ctx, scr) {
		d.hooks.cellBgDrawer(ctx, dst, area, cell)
	}
	d.drawCellForegrounds(ctx, dst, scr)

	if ctx.cfg.HasState && ctx.cfg.State.CursorVisible && idraw.CursorVisible(ctx.cfg) {
		if cursor := ctx.CursorBounds(); !cursor.Empty() {
			d.hooks.cursorDrawer(ctx, dst, cursor)
		}
	}

	if ctx.cfg.Scrollbar && ctx.cfg.HasState && !ctx.cfg.State.AltScreen &&
		ctx.cfg.State.ScrollbackCount > ctx.ScreenBounds().Dy() &&
		!ctx.ScrollbarBounds().Empty() {
		d.hooks.scrollbarDrawer(ctx, dst, ctx.ScrollbarBounds())
	}

	if ctx.cfg.HasState && !ctx.cfg.State.Focused {
		effects.ApplyFocusDimming(ctx, dst)
	}

	effects.ApplyRoundedMask(ctx, dst)
}

func (d *Renderer) drawCellForegrounds(ctx Context, dst draw.Image, scr uv.Screen) {
	frame, ok := dst.(*image.NRGBA)
	if !ok {
		for area, cell := range iterCells(ctx, scr) {
			d.hooks.cellFgDrawer(ctx, dst, area, cell)
		}
		return
	}

	d.fgPass.Reset(ctx.GridBounds())
	ctx = ctx.withGlyphPass(&d.fgPass)
	for area, cell := range iterCells(ctx, scr) {
		d.hooks.cellFgDrawer(ctx, dst, area, cell)
	}
	d.fgPass.Composite(frame)
	d.fgPass.DrawPending(ctx, dst)
}

func iterCells(ctx Context, scr uv.Screen) iter.Seq2[image.Rectangle, *uv.Cell] {
	screen := ctx.ScreenBounds()
	gridBounds := ctx.GridBounds()
	cellWidth := ctx.Metrics().CellWidth.Int()
	return func(yield func(image.Rectangle, *uv.Cell) bool) {
		var cell *uv.Cell
		var area image.Rectangle
		for y := screen.Min.Y; y < screen.Max.Y; y++ {
			for x := screen.Min.X; x < screen.Max.X; x++ {
				cell = scr.CellAt(x, y)
				if cell == nil {
					cell = &uv.EmptyCell
				}
				if cell.Width == 0 {
					continue
				}
				area = ctx.CellBounds(x, y)
				if cell.Width > 1 {
					area.Max.X = min(gridBounds.Max.X, area.Min.X+cell.Width*cellWidth)
				}
				if !yield(area, cell) {
					return
				}
			}
		}
	}
}

func (d *Renderer) contextLocked(origin image.Point, scr uv.Screen) Context {
	if scr == nil {
		panic("nil screen")
	}

	screen := scr.Bounds()
	cell := d.metrics.CellSize()
	margin := d.opts.Margin.Int()
	padding := d.opts.Padding.Int()
	scrollbarWidth := 0
	if d.opts.Scrollbar {
		scrollbarWidth = scrollbarWidthPx
	}

	gridSize := image.Pt(screen.Dx()*cell.X, screen.Dy()*cell.Y)
	windowSize := image.Pt(gridSize.X+scrollbarWidth+2*padding, gridSize.Y+2*padding)
	size := image.Pt(windowSize.X+2*margin, windowSize.Y+2*margin)
	imageBounds := image.Rectangle{Min: origin, Max: origin.Add(size)}
	windowBounds := imageBounds.Inset(margin)
	gridBounds := image.Rectangle{Min: windowBounds.Min.Add(image.Pt(padding, padding)), Max: windowBounds.Min.Add(image.Pt(padding, padding)).Add(gridSize)}
	scrollbarBounds := image.Rectangle{}
	if scrollbarWidth > 0 {
		scrollbarBounds = image.Rect(gridBounds.Max.X, gridBounds.Min.Y, gridBounds.Max.X+scrollbarWidth, gridBounds.Max.Y)
	}
	var cursorCell *uv.Cell
	if d.emulatorState != nil {
		cursorCell = scr.CellAt(screen.Min.X+d.emulatorState.CursorX, screen.Min.Y+d.emulatorState.CursorY)
	}

	return Context{
		cfg:             config.NewSnapshot(d.opts, d.metrics, d.fonts, d.emulatorState),
		screenBounds:    screen,
		imageBounds:     imageBounds,
		windowBounds:    windowBounds,
		gridBounds:      gridBounds,
		scrollbarBounds: scrollbarBounds,
		cursorCell:      cursorCell,
	}
}
