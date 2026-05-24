// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/image/font"
)

// frameConfig is the immutable per-draw configuration snapshot. It is built
// once in [Renderer.contextLocked] from [rendererOptions], derived metrics, and fonts.
//
// Color and opacity behavior follows Ghostty configuration concepts.
type frameConfig struct {
	// metrics holds per-draw cell and decoration metrics (derived with the same
	// ideas as Ghostty's terminal cell grid sizing).
	//
	// https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig
	metrics Metrics
	// fonts selects faces per cell style and codepoint map (mirrors Ghostty's
	// primary vs mapped-font measurement model).
	fonts *fontSet
	// palette is a snapshot of indexed ANSI colors and sparse overrides for this draw.
	palette Palette

	// state is the emulator snapshot (cursor, scroll/focus flags, etc.) when
	// hasState is true.
	state EmulatorState
	// hasState reports whether state was supplied; if false, drawers skip cursor,
	// scrollbar, and focus-only effects.
	hasState bool

	// margin is the outside margin width in pixels (outside the rounded window).
	margin Px
	// marginFill is the optional fill for the margin area; nil selects palette/default.
	marginFill color.Color
	// padding is the inset between the window edge and the cell grid, in pixels.
	padding Px
	// scrollbar is whether a scrollbar gutter is reserved (actual thumb/track painting
	// uses emulator state elsewhere).
	scrollbar bool

	// borderRadius is the corner radius applied when masking the terminal window.
	borderRadius Px
	// backgroundOpacity scales the default window background alpha (Ghostty's
	// `background-opacity`).
	//
	// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
	backgroundOpacity float64
	// backgroundOpacityCells extends that opacity to cells with explicit BG colors when true
	// (Ghostty's `background-opacity-cells`).
	//
	// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
	backgroundOpacityCells bool
	// faintFactor blends faint-attribute foreground toward the background (tunable
	// analog of Ghostty's `faint-opacity`).
	//
	// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
	faintFactor float64
	// focusDimming multiplies RGB when the emulated surface is unfocused (same "dim
	// unfocused UI" idea as Ghostty's `unfocused-split-opacity`).
	//
	// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
	focusDimming float64
	// cursorBlinkSpeed is one half-cycle of the cursor blink animation (Ghostty's
	// renderer uses a 600ms step).
	//
	// https://github.com/ghostty-org/ghostty/blob/main/src/renderer/Thread.zig
	cursorBlinkSpeed time.Duration
	// now is the wall clock used for blink phase and related animation sampling.
	now time.Time
	// boxThicknessOverride is true when [WithBoxThickness] set an explicit pixel
	// thickness; default metrics keep font-native box drawing strokes.
	boxThicknessOverride bool
}

// Context is an immutable snapshot for one draw.
//
// Geometry matches the renderer plan: margin is outside the raster, padding
// sits inside the rounded terminal window, and the cell grid shares its origin
// with gridBounds. When [frameConfig.hasState] is false, cursor, scrollbar, and
// focus effects are omitted by higher-level drawers.
//
// Appearance, metrics, and palette configuration live in the embedded
// [frameConfig]; Context adds pixel layout and derived cell lookups only.
type Context struct {
	cfg frameConfig

	// screenBounds is scr.Bounds() in screen cell coordinates.
	screenBounds image.Rectangle
	// imageBounds is the full output rectangle (including outside margin) in pixels.
	imageBounds image.Rectangle
	// windowBounds is the terminal window rectangle inside the outside margin (padding,
	// grid, and optional scrollbar gutter).
	windowBounds image.Rectangle
	// gridBounds is the destination rectangle for the cell grid in pixels.
	gridBounds image.Rectangle
	// scrollbarBounds is the scrollbar gutter reserved beside the grid, or empty if none.
	scrollbarBounds image.Rectangle

	// cursorCell is the [uv.Cell] under the logical cursor, if state and coordinates
	// are valid.
	cursorCell *uv.Cell

	// glyphPass, when non-nil, routes [DrawCellFg] grid glyphs through max-alpha
	// coverage compositing for *image.NRGBA destinations during [Renderer.renderLocked].
	glyphPass *glyphCoveragePass
}

// withGlyphCoveragePass returns ctx with pass wired for the current foreground draw.
func (c Context) withGlyphCoveragePass(pass *glyphCoveragePass) Context {
	c.glyphPass = pass
	return c
}

// FontFace returns the configured font face for cell's style and glyph.
func (c Context) FontFace(cell *uv.Cell) font.Face {
	return c.cfg.fonts.faceForCell(cell)
}

// usesGridLayout reports whether face is a primary grid monospace variant
// (regular, bold, italic, bold italic) as opposed to a codepoint-mapped symbol face.
func (c Context) usesGridLayout(face font.Face) bool {
	if c.cfg.fonts == nil {
		return false
	}
	return c.cfg.fonts.usesGridLayout(face)
}

// Metrics returns the derived metrics for this draw.
func (c Context) Metrics() Metrics {
	return c.cfg.metrics
}

// Palette returns a copy of the palette for this draw.
func (c Context) Palette() Palette {
	return clonePalette(c.cfg.palette)
}

// EmulatorState returns the emulator state for this draw.
func (c Context) EmulatorState() EmulatorState {
	return c.cfg.state
}

// HasEmulatorState reports whether emulator state was applied for this draw.
func (c Context) HasEmulatorState() bool {
	return c.cfg.hasState
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

	cell := c.cfg.metrics.CellSize()
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
	if !c.cfg.hasState {
		return image.Rectangle{}
	}
	return c.CellBounds(c.screenBounds.Min.X+c.cfg.state.CursorX, c.screenBounds.Min.Y+c.cfg.state.CursorY)
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

// CellColors returns resolved foreground and background colors for cell.
func (c Context) CellColors(cell *uv.Cell) (fg, bg color.Color) {
	return resolveCellColors(c, cell)
}

// CellBackgroundColor returns the cell background with opacity applied. When
// backgroundOpacityCells is false, cells with an explicit background color keep
// full opacity (Ghostty default for `background-opacity-cells`).
//
// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
func (c Context) CellBackgroundColor(cell *uv.Cell) color.Color {
	return resolveCellBackground(c, cell)
}

// ForegroundColor returns the resolved default foreground color.
func (c Context) ForegroundColor() color.Color {
	return resolveForeground(c, nil)
}

// BackgroundColor returns the resolved default background color.
func (c Context) BackgroundColor() color.Color {
	return resolveBackground(c, nil)
}

// WindowBackgroundColor returns the terminal background with global opacity
// (Ghostty `background-opacity`.
//
// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
func (c Context) WindowBackgroundColor() color.Color {
	return applyAlpha(c.BackgroundColor(), c.cfg.backgroundOpacity)
}

// CursorColor returns the resolved cursor color (Ghostty `cursor-color`).
//
// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
func (c Context) CursorColor() color.Color {
	return resolveCursor(c)
}

// CursorTextColor returns the resolved cursor text color (Ghostty `cursor-text`).
//
// https://github.com/ghostty-org/ghostty/blob/main/src/config/Config.zig
func (c Context) CursorTextColor() color.Color {
	if c.cfg.palette.CursorText != nil {
		return c.cfg.palette.CursorText
	}
	return c.BackgroundColor()
}

// MarginColor returns the resolved outside-margin fill color.
func (c Context) MarginColor() color.Color {
	if c.cfg.marginFill != nil {
		return c.cfg.marginFill
	}
	if c.cfg.palette.Margin != nil {
		return c.cfg.palette.Margin
	}
	return color.NRGBA{}
}
