// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/draw"
	"iter"

	uv "github.com/charmbracelet/ultraviolet"
)

const scrollbarWidthPx = 1

// DrawInto draws scr into dst within area. When area is empty, the required
// pixel bounds are derived from scr and the current renderer options, anchored at
// dst.Bounds().Min; dst must be at least that large.
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

// drawIntoLocked renders scr into dst, placing the full terminal frame at area.Min.
//
// When area is empty, bounds are derived from scr and renderer options with origin
// at dst.Bounds().Min. It panics if dst is nil, if dst is too small in the empty
// area case, or if area is smaller than [Context.ImageBounds] would require.
// The caller must hold [Renderer.mu].
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

// drawIntoContextLocked validates area against ctx and runs [Renderer.renderLocked].
// ctx must have been built with the same origin [drawIntoLocked] would use:
// area.Min when area is non-empty, otherwise dst.Bounds().Min.
// The caller must hold [Renderer.mu].
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

// renderLocked runs the full-frame pipeline in order: optional background, every
// occupied cell background, every occupied cell foreground (skipping zero-width
// continuations; wide cells span [uv.Cell.Width] columns), cursor when visible,
// scrollbar when enabled and applicable, focus dimming, then the antialiased
// rounded window mask.
//
// [uv.Screen] samples use absolute coordinates: each iteration passes the loop (x,
// y) to scr.CellAt(x, y). Pixel placement is normalized by [Context]:
// [Context.CellBounds] subtracts [Context.ScreenBounds].Min so a non-zero-minimum
// [uv.Screen] still maps into the grid anchored at [Context.GridBounds].Min.
//
// See [Renderer.DrawInto] for the outermost placement of the image rectangle at the
// destination origin (typically area.Min from [Renderer.DrawInto]).
func (d *Renderer) renderLocked(ctx Context, dst draw.Image, scr uv.Screen) {
	if d.opts.backgroundDrawer != nil {
		d.opts.backgroundDrawer(ctx, dst, ctx.ImageBounds())
	}

	// TODO: future dirty-rect rendering could use VT damage/touched state when
	// an appropriate source interface is available.
	for area, cell := range iterCells(ctx, scr) {
		d.opts.cellBgDrawer(ctx, dst, area, cell)
	}
	d.drawCellForegrounds(ctx, dst, scr)

	if ctx.cfg.hasState && ctx.cfg.state.CursorVisible && cursorVisible(ctx) {
		if cursor := ctx.CursorBounds(); !cursor.Empty() {
			d.opts.cursorDrawer(ctx, dst, cursor)
		}
	}

	if ctx.cfg.scrollbar && ctx.cfg.hasState && !ctx.cfg.state.AltScreen &&
		ctx.cfg.state.ScrollbackCount > ctx.ScreenBounds().Dy() &&
		!ctx.ScrollbarBounds().Empty() {
		d.opts.scrollbarDrawer(ctx, dst, ctx.ScrollbarBounds())
	}

	if ctx.cfg.hasState && !ctx.cfg.state.Focused {
		applyFocusDimming(ctx, dst)
	}

	applyRoundedMask(ctx, dst)
}

// drawCellForegrounds renders glyph masks and cell decorations. [glyphGrid]
// glyphs are accumulated with max-alpha coverage and composited once over
// backgrounds so antialiased ink from neighboring cells does not stack at seams.
// [glyphPowerline] and [glyphNerdIcon] glyphs are drawn afterward with [draw.Over].
//
// On *image.NRGBA destinations every cell invokes [rendererOptions.cellFgDrawer]; when
// the hook calls [DrawCellFg], [glyphGrid] glyphs participate in the coverage pass and
// other glyph kinds plus decorations are deferred until after compositing. Custom hooks
// that draw directly during the cell walk keep that behavior; hooks that only wrap
// [DrawCellFg] retain seam-fix compositing.
func (d *Renderer) drawCellForegrounds(ctx Context, dst draw.Image, scr uv.Screen) {
	frame, ok := dst.(*image.NRGBA)
	if !ok {
		for area, cell := range iterCells(ctx, scr) {
			d.opts.cellFgDrawer(ctx, dst, area, cell)
		}
		return
	}

	d.fgPass.reset(&d.glyphCov, ctx.GridBounds())
	ctx = ctx.withGlyphCoveragePass(&d.fgPass)
	for area, cell := range iterCells(ctx, scr) {
		d.opts.cellFgDrawer(ctx, dst, area, cell)
	}
	d.fgPass.composite(frame)
	d.fgPass.drawPending(ctx, dst)
}

// iterCells returns an iterator over all cells in the screen.
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

// contextLocked builds the immutable [Context] for one draw from scr and the
// destination origin.
//
// Layout matches the planned frame model: margin is outside the rounded terminal
// window; padding sits between the window edge and the cell grid; when scrollbar
// support is enabled a fixed-width gutter is reserved past the grid without
// changing logical screen dimensions. The pixel size of each cell comes from the
// renderer's derived metrics (Ghostty-style rounding and baseline rules as in
// [gh-metrics-zig]), while margin and padding use DPI-scaled pixel options from
// [Renderer].
//
// Coordinate normalization: [Context.ImageBounds] is anchored at origin
// ([Renderer.DrawInto] passes area.Min), so the raster is written at the caller's
// chosen offset. [Context.ScreenBounds] is scr.Bounds() verbatim; loops use
// absolute (x, y) with scr.CellAt(x, y), and [Context.CellBounds] maps into the
// grid by subtracting [Context.ScreenBounds].Min. Emulator cursor indices are
// interpreted relative to the same absolute origin.
//
// [frameConfig.backgroundOpacity] and [frameConfig.backgroundOpacityCells] follow the
// window-versus-cell opacity split described for Ghostty's background-opacity
// style settings (see [gh-config-zig]).
//
// [gh-metrics-zig]: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig
// [gh-config-zig]: https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
func (d *Renderer) contextLocked(origin image.Point, scr uv.Screen) Context {
	if scr == nil {
		panic("nil screen")
	}

	screen := scr.Bounds()
	cell := d.metrics.CellSize()
	margin := d.opts.margin.Int()
	padding := d.opts.padding.Int()
	scrollbarWidth := 0
	if d.opts.scrollbar {
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
	if d.opts.hasState {
		cursorCell = scr.CellAt(screen.Min.X+d.opts.state.CursorX, screen.Min.Y+d.opts.state.CursorY)
	}

	return Context{
		cfg: frameConfig{
			metrics:                d.metrics,
			fonts:                  d.fonts,
			palette:                clonePalette(d.opts.palette),
			state:                  d.opts.state,
			hasState:               d.opts.hasState,
			margin:                 d.opts.margin,
			marginFill:             d.opts.marginFill,
			padding:                d.opts.padding,
			scrollbar:              d.opts.scrollbar,
			borderRadius:           d.opts.borderRadius,
			backgroundOpacity:      d.opts.backgroundOpacity,
			backgroundOpacityCells: d.opts.backgroundOpacityCells,
			faintFactor:            d.opts.faintFactor,
			focusDimming:           d.opts.focusDimming,
			cursorBlinkSpeed:       d.opts.cursorBlinkSpeed,
			now:                    d.opts.now(),
			boxThicknessOverride:   d.opts.boxThickness > 0,
		},
		screenBounds:    screen,
		imageBounds:     imageBounds,
		windowBounds:    windowBounds,
		gridBounds:      gridBounds,
		scrollbarBounds: scrollbarBounds,
		cursorCell:      cursorCell,
	}
}
