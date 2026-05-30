// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	icol "github.com/lrstanley/x/charm/still/internal/color"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
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

	now             time.Time
	emu             types.EmulatorState
	hasEmu          bool
	screenBounds    image.Rectangle
	imageBounds     image.Rectangle
	windowBounds    image.Rectangle
	gridBounds      image.Rectangle
	scrollbarBounds image.Rectangle
	cursorCell      *uv.Cell
	cellW           int
	cellH           int
}

func (d *Renderer) buildFrame(origin image.Point, scr uv.Screen) *renderFrame {
	if scr == nil {
		panic("nil screen")
	}

	f := &renderFrame{r: d}
	f.now = d.opts.Now()
	f.hasEmu = false
	if d.emulatorState != nil {
		f.emu = *d.emulatorState
		f.hasEmu = true
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
	f.imageBounds = image.Rectangle{Min: origin, Max: origin.Add(size)}
	f.windowBounds = f.imageBounds.Inset(margin)
	gridOrigin := f.windowBounds.Min.Add(image.Pt(padding, padding))
	f.gridBounds = image.Rectangle{
		Min: gridOrigin,
		Max: gridOrigin.Add(gridSize),
	}
	f.scrollbarBounds = image.Rectangle{}
	if scrollbarWidth > 0 {
		f.scrollbarBounds = image.Rect(
			f.gridBounds.Max.X, f.gridBounds.Min.Y,
			f.gridBounds.Max.X+scrollbarWidth, f.gridBounds.Max.Y,
		)
	}

	f.screenBounds = screen
	f.cellW = cell.X
	f.cellH = cell.Y
	f.cursorCell = nil
	if f.hasEmu {
		f.cursorCell = scr.CellAt(screen.Min.X+f.emu.CursorX, screen.Min.Y+f.emu.CursorY)
	}

	f.metrics = d.metrics
	f.palette = d.palette
	f.boxThicknessOverride = d.boxThicknessOverride
	f.opts = d.opts
	return f
}

// layoutSnapshot prepares per-draw geometry for tests. Snapshotted metrics,
// palette, and layout rectangles are safe after the lock is released. FontFace
// still reads the renderer font set, which is immutable until the next rebuild.
func (d *Renderer) layoutSnapshot(t interface{ Helper() }, origin image.Point, scr uv.Screen) *renderFrame {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buildFrame(origin, scr)
}

func (f *renderFrame) size() image.Point {
	return image.Pt(f.imageBounds.Dx(), f.imageBounds.Dy())
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
	if !f.hasEmu {
		return image.Rectangle{}
	}
	return f.cellBounds(f.screenBounds.Min.X+f.emu.CursorX, f.screenBounds.Min.Y+f.emu.CursorY)
}

func (f *renderFrame) Now() time.Time { return f.now }

func (f *renderFrame) CursorBlinkSpeed() time.Duration { return f.opts.CursorBlinkSpeed }

func (f *renderFrame) Palette() types.Palette { return f.palette }

func (f *renderFrame) EmulatorState() (types.EmulatorState, bool) {
	return f.emu, f.hasEmu
}

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

func (f *renderFrame) FontFace(cell *uv.Cell) font.Face {
	if f.r.fonts == nil {
		return nil
	}
	return f.r.fonts.FaceForCell(cell)
}

func (f *renderFrame) UsesGridLayout(face font.Face) bool {
	if f.r.fonts == nil {
		return false
	}
	return f.r.fonts.UsesGridLayout(face)
}

func (f *renderFrame) CursorCell() *uv.Cell { return f.cursorCell }

func (f *renderFrame) Glyph(cell *uv.Cell) string {
	if cell == nil || cell.Content == "" {
		return " "
	}
	return cell.Content
}

func (f *renderFrame) CellColorsNRGBA(cell *uv.Cell) (fg, bg color.NRGBA) {
	return icol.ResolveCellColorsNRGBA(f, cell)
}

func (f *renderFrame) ResolvePaletteColor(c color.Color) color.Color {
	return icol.ResolvePaletteColor(f, c)
}

func (f *renderFrame) cellBackgroundNRGBA(cell *uv.Cell) color.NRGBA {
	return icol.ResolveCellBackgroundNRGBA(f, cell)
}

func (f *renderFrame) foregroundNRGBA(style *uv.Style) color.NRGBA {
	return icol.ResolveForegroundNRGBA(f, style)
}

func (f *renderFrame) backgroundNRGBA(style *uv.Style) color.NRGBA {
	return icol.ResolveBackgroundNRGBA(f, style)
}

func (f *renderFrame) windowBackgroundNRGBA() color.NRGBA {
	return icol.NRGBA(icol.ApplyAlpha(f.backgroundNRGBA(nil), f.opts.BackgroundOpacity))
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

func (f *renderFrame) marginColor() color.Color {
	if f.opts.MarginFill != nil {
		return f.opts.MarginFill
	}
	if f.palette.Margin != nil {
		return f.palette.Margin
	}
	return color.NRGBA{}
}

func (f *renderFrame) MarginColor() color.Color { return f.marginColor() }
