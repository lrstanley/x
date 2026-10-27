// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"time"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	"github.com/lrstanley/x/term/reel/render/internal/config"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/pool"
	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt"
	"golang.org/x/image/font"
)

// renderFrame holds per-draw geometry and satisfies internal render-context
// interfaces (config.ColorSource, config.FrameSource, config.WindowSource,
// config.CellFrameSource, raster.GlyphContext).
type renderFrame struct {
	r *Renderer

	metrics              Metrics
	palette              types.Palette
	boxThicknessOverride bool
	opts                 config.Options

	at              time.Duration
	st              vt.State
	screenBounds    image.Rectangle
	imageBounds     image.Rectangle
	windowBounds    image.Rectangle
	chromeBounds    image.Rectangle
	gridBounds      image.Rectangle
	scrollbarBounds image.Rectangle
	progressBounds  image.Rectangle
	progress        vt.Progress
	status          frameStatus
	cursorCell      *vt.Cell
	cellW           int
	cellH           int
}

// frameStatus is the OSC 7501 program status a frame draws.
type frameStatus struct {
	view statusView
	ring statusRing
	// lut is the outline look, built on first use by
	// [Renderer.statusPaints], so repainting several bands builds it once.
	lut []statusPaint
}

// indicators are the indicators f draws.
func (f *renderFrame) indicators() indicators {
	return indicators{progress: f.progress, status: f.status.view}
}

// renderFramePool recycles per-draw frames on the Draw and Repaint hot path.
var renderFramePool = pool.Pool[*renderFrame]{
	New: func() *renderFrame { return &renderFrame{} },
	Prepare: func(f *renderFrame) *renderFrame {
		*f = renderFrame{}
		return f
	},
}

// borrowRenderFrame lays out a frame for scr with st at origin, as of at.
func (d *Renderer) borrowRenderFrame(origin image.Point, scr vt.Screen, st vt.State, at time.Duration) *renderFrame {
	f := renderFramePool.Get()
	d.populateRenderFrame(f, origin, scr, st, at)
	return f
}

func releaseRenderFrame(f *renderFrame) {
	renderFramePool.Put(f)
}

// chromeHeight is the title bar height in pixels, or 0 without window chrome.
func (d *Renderer) chromeHeight() int {
	if !d.opts.WindowChrome {
		return 0
	}
	cellH := d.metrics.CellHeight.Int()
	return cellH + cellH/2
}

func (d *Renderer) populateRenderFrame(f *renderFrame, origin image.Point, scr vt.Screen, st vt.State, at time.Duration) {
	if scr == nil {
		panic("render: nil screen")
	}

	f.r = d
	f.at = at
	f.st = st

	screen := scr.Bounds()
	l := d.layout(screen.Dx(), screen.Dy())
	cell := l.Cell

	f.imageBounds = l.Image.Add(origin)
	f.windowBounds = f.imageBounds.Inset(d.opts.Margin.Int())
	f.chromeBounds = f.windowBounds
	f.chromeBounds.Max.Y = f.chromeBounds.Min.Y + d.chromeHeight()
	f.gridBounds = l.Grid.Add(origin)
	f.scrollbarBounds = image.Rectangle{}
	if d.opts.Scrollbar {
		f.scrollbarBounds = image.Rect(
			f.gridBounds.Max.X, f.gridBounds.Min.Y,
			f.gridBounds.Max.X+scrollbarWidthPx, f.gridBounds.Max.Y,
		)
	}
	d.layoutProgress(f)
	d.layoutStatus(f)

	f.screenBounds = screen
	f.cellW = cell.X
	f.cellH = cell.Y
	f.cursorCell = scr.CellAt(screen.Min.X+st.Cursor.X, screen.Min.Y+st.Cursor.Y)

	f.metrics = d.metrics
	f.palette = d.palette
	f.boxThicknessOverride = d.boxThicknessOverride
	f.opts = d.opts
}

func (f *renderFrame) cellBounds(x, y int) image.Rectangle {
	if !image.Pt(x, y).In(f.screenBounds) {
		return image.Rectangle{}
	}
	col := x - f.screenBounds.Min.X
	row := y - f.screenBounds.Min.Y
	cellMin := image.Pt(
		f.gridBounds.Min.X+col*f.cellW,
		f.gridBounds.Min.Y+row*f.cellH,
	)
	return image.Rectangle{
		Min: cellMin,
		Max: cellMin.Add(image.Pt(f.cellW, f.cellH)),
	}
}

func (f *renderFrame) cursorBounds() image.Rectangle {
	return f.cellBounds(f.screenBounds.Min.X+f.st.Cursor.X, f.screenBounds.Min.Y+f.st.Cursor.Y)
}

func (f *renderFrame) At() time.Duration { return f.at }

func (f *renderFrame) CursorBlinkSpeed() time.Duration { return f.opts.CursorBlinkSpeed }

func (f *renderFrame) Palette() types.Palette { return f.palette }

func (f *renderFrame) State() vt.State { return f.st }

func (f *renderFrame) FaintFactor() float64 { return f.opts.FaintFactor }

func (f *renderFrame) BackgroundOpacity() float64 { return f.opts.BackgroundOpacity }

func (f *renderFrame) BackgroundOpacityCells() bool { return f.opts.BackgroundOpacityCells }

func (f *renderFrame) FocusDimming() float64 { return f.opts.FocusDimming }

func (f *renderFrame) BorderRadius() units.Px { return f.opts.BorderRadius }

func (f *renderFrame) Metrics() types.Metrics { return f.metrics }

func (f *renderFrame) GridBounds() image.Rectangle { return f.gridBounds }

func (f *renderFrame) WindowBounds() image.Rectangle { return f.windowBounds }

func (f *renderFrame) ScreenBounds() image.Rectangle { return f.screenBounds }

func (f *renderFrame) ImageBounds() image.Rectangle { return f.imageBounds }

func (f *renderFrame) ScrollbarBounds() image.Rectangle { return f.scrollbarBounds }

func (f *renderFrame) BoxThicknessOverride() bool { return f.boxThicknessOverride }

func (f *renderFrame) FontFace(cell *vt.Cell, cluster idraw.Cluster) font.Face {
	if f.r.fonts == nil {
		return nil
	}
	return f.r.fonts.FaceForCell(cell, cluster)
}

func (f *renderFrame) ColorGlyph(cluster idraw.Cluster) idraw.ColorGlyph {
	return f.r.fonts.ColorGlyph(cluster)
}

func (f *renderFrame) UsesGridLayout(face font.Face) bool {
	if f.r.fonts == nil {
		return false
	}
	return f.r.fonts.UsesGridLayout(face)
}

func (f *renderFrame) CursorCell() *vt.Cell { return f.cursorCell }

func (f *renderFrame) CellColorsNRGBA(cell *vt.Cell) (fg, bg color.NRGBA) {
	return icol.ResolveCellColorsNRGBA(f, cell)
}

func (f *renderFrame) ResolvePaletteColor(c color.Color) color.Color {
	return icol.ResolvePaletteColor(f, c)
}

func (f *renderFrame) cellBackgroundNRGBA(cell *vt.Cell) color.NRGBA {
	return icol.ResolveCellBackgroundNRGBA(f, cell)
}

func (f *renderFrame) cellBackgroundFromResolved(cell *vt.Cell, bg color.NRGBA) color.NRGBA {
	if icol.CellHasExplicitBackground(cell) && !f.BackgroundOpacityCells() {
		return bg
	}
	return icol.ApplyAlphaNRGBA(bg, f.BackgroundOpacity())
}

func (f *renderFrame) foregroundNRGBA(style *vt.Style) color.NRGBA {
	return icol.ResolveForegroundNRGBA(f, style)
}

func (f *renderFrame) backgroundNRGBA(style *vt.Style) color.NRGBA {
	return icol.ResolveBackgroundNRGBA(f, style)
}

func (f *renderFrame) windowBackgroundNRGBA() color.NRGBA {
	return icol.ApplyAlphaNRGBA(f.backgroundNRGBA(nil), f.opts.BackgroundOpacity)
}

func (f *renderFrame) cursorColorNRGBA() color.NRGBA {
	return icol.ResolveCursorNRGBA(f, f.foregroundNRGBA(nil))
}

func (f *renderFrame) cursorTextColorNRGBA() color.NRGBA {
	if f.palette.CursorText != nil {
		return icol.NRGBA(f.palette.CursorText)
	}
	return f.backgroundNRGBA(nil)
}

// MarginColor is the fill outside the window: the fill of [WithMargin], else the
// palette's margin color, else transparent.
func (f *renderFrame) MarginColor() color.Color {
	if f.opts.MarginFill != nil {
		return f.opts.MarginFill
	}
	if f.palette.Margin != nil {
		return f.palette.Margin
	}
	return color.NRGBA{}
}
