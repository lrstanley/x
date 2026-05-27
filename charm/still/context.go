// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	icol "github.com/lrstanley/x/charm/still/internal/color"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/raster"
	"golang.org/x/image/font"
)

// Context is an immutable snapshot for one draw.
type Context struct {
	cfg config.Snapshot

	screenBounds    image.Rectangle
	imageBounds     image.Rectangle
	windowBounds    image.Rectangle
	gridBounds      image.Rectangle
	scrollbarBounds image.Rectangle

	cursorCell *uv.Cell

	glyphPass *raster.Pass
}

func (c Context) withGlyphPass(pass *raster.Pass) Context {
	c.glyphPass = pass
	return c
}

// Snapshot returns the per-draw configuration snapshot.
func (c Context) Snapshot() config.Snapshot {
	return c.cfg
}

// FontFace returns the configured font face for cell's style and glyph.
func (c Context) FontFace(cell *uv.Cell) font.Face {
	if c.cfg.Fonts == nil {
		return nil
	}
	return c.cfg.Fonts.FaceForCell(cell)
}

// UsesGridLayout reports whether face is a primary grid monospace variant.
func (c Context) UsesGridLayout(face font.Face) bool {
	if c.cfg.Fonts == nil {
		return false
	}
	return c.cfg.Fonts.UsesGridLayout(face)
}

// Metrics returns the derived metrics for this draw.
func (c Context) Metrics() Metrics {
	return c.cfg.Metrics
}

// Palette returns a copy of the palette for this draw.
func (c Context) Palette() Palette {
	return config.ClonePalette(c.cfg.Palette)
}

// EmulatorState returns the emulator state for this draw.
func (c Context) EmulatorState() EmulatorState {
	return c.cfg.State
}

// HasEmulatorState reports whether emulator state was applied for this draw.
func (c Context) HasEmulatorState() bool {
	return c.cfg.HasState
}

// ScreenBounds returns the source screen bounds.
func (c Context) ScreenBounds() image.Rectangle {
	return c.screenBounds
}

// ImageBounds returns the destination image bounds used by the renderer.
func (c Context) ImageBounds() image.Rectangle {
	return c.imageBounds
}

// WindowBounds returns the terminal window area inside the outside margin.
func (c Context) WindowBounds() image.Rectangle {
	return c.windowBounds
}

// GridBounds returns the terminal cell grid area.
func (c Context) GridBounds() image.Rectangle {
	return c.gridBounds
}

// ScrollbarBounds returns the reserved scrollbar area, if any.
func (c Context) ScrollbarBounds() image.Rectangle {
	return c.scrollbarBounds
}

// Size returns the pixel size required for this draw.
func (c Context) Size() image.Point {
	return image.Pt(c.imageBounds.Dx(), c.imageBounds.Dy())
}

// CellBounds returns the destination bounds for a source screen cell.
func (c Context) CellBounds(x, y int) image.Rectangle {
	if !image.Pt(x, y).In(c.screenBounds) {
		return image.Rectangle{}
	}

	cell := c.cfg.Metrics.CellSize()
	col := x - c.screenBounds.Min.X
	row := y - c.screenBounds.Min.Y
	cellMin := image.Pt(
		c.gridBounds.Min.X+col*cell.X,
		c.gridBounds.Min.Y+row*cell.Y,
	)
	return image.Rectangle{
		Min: cellMin,
		Max: cellMin.Add(cell),
	}
}

// CursorBounds returns the destination bounds for the current cursor.
func (c Context) CursorBounds() image.Rectangle {
	if !c.cfg.HasState {
		return image.Rectangle{}
	}
	return c.CellBounds(c.screenBounds.Min.X+c.cfg.State.CursorX, c.screenBounds.Min.Y+c.cfg.State.CursorY)
}

// CursorCell returns the cell underneath the cursor for this draw, if any.
func (c Context) CursorCell() *uv.Cell {
	return c.cursorCell
}

// Glyph returns the renderable glyph content for cell.
func (c Context) Glyph(cell *uv.Cell) string {
	if cell == nil || cell.Content == "" {
		return " "
	}
	return cell.Content
}

// CellColorsNRGBA returns resolved foreground and background as concrete NRGBA values.
func (c Context) CellColorsNRGBA(cell *uv.Cell) (fg, bg color.NRGBA) {
	return icol.ResolveCellColorsNRGBA(c.cfg, cell)
}

// CellColors returns resolved foreground and background colors for cell.
func (c Context) CellColors(cell *uv.Cell) (fg, bg color.Color) {
	f, b := c.CellColorsNRGBA(cell)
	return f, b
}

// CellBackgroundColor returns the cell background with opacity applied.
func (c Context) CellBackgroundColor(cell *uv.Cell) color.Color {
	return icol.ResolveCellBackground(c.cfg, cell)
}

// ForegroundColor returns the resolved default foreground color.
func (c Context) ForegroundColor() color.Color {
	return icol.ResolveForeground(c.cfg, nil)
}

// BackgroundColor returns the resolved default background color.
func (c Context) BackgroundColor() color.Color {
	return icol.ResolveBackground(c.cfg, nil)
}

// WindowBackgroundColor returns the terminal background with global opacity.
func (c Context) WindowBackgroundColor() color.Color {
	return icol.ApplyAlpha(c.BackgroundColor(), c.cfg.BackgroundOpacity)
}

// CursorColor returns the resolved cursor color.
func (c Context) CursorColor() color.Color {
	return icol.ResolveCursor(c.cfg, c.ForegroundColor())
}

// CursorTextColor returns the resolved cursor text color.
func (c Context) CursorTextColor() color.Color {
	if c.cfg.Palette.CursorText != nil {
		return c.cfg.Palette.CursorText
	}
	return c.BackgroundColor()
}

// MarginColor returns the resolved outside-margin fill color.
func (c Context) MarginColor() color.Color {
	if c.cfg.MarginFill != nil {
		return c.cfg.MarginFill
	}
	if c.cfg.Palette.Margin != nil {
		return c.cfg.Palette.Margin
	}
	return color.NRGBA{}
}
