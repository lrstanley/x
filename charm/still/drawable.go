// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// CellDrawer draws a cell (background or foreground). area is the cell bounding box.
type CellDrawer func(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell)

// CursorDrawer draws the cursor to the given image, where area is the bounding box of
// the cursor.
type CursorDrawer func(ctx Context, img draw.Image, area image.Rectangle)

// ScrollbarDrawer draws the scrollbar to the given image, where area is the bounding
// box of the scrollbar.
type ScrollbarDrawer func(ctx Context, img draw.Image, area image.Rectangle)

// BackgroundDrawer draws the background to the given image, where area is the bounding
// box of the background.
type BackgroundDrawer func(ctx Context, img draw.Image, area image.Rectangle)

// DrawCellBg fills the cell background. [Renderer] runs this in a first pass so
// glyph ink (e.g. synthetic italic) can extend into the next cell without being
// covered by that cell's background.
func DrawCellBg(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell) {
	// [DrawBackground] already filled the window with the default cell color.
	// Most cells inherit that same color, so skip ~O(cells) redundant draw.Draw
	// calls per frame. Reverse-video without an explicit fg still needs a fill
	// because resolveCellColors swaps fg/bg to something other than the window.
	if !cellHasExplicitBackground(cell) &&
		(cell == nil || cell.Style.Attrs&uv.AttrReverse == 0) {
		return
	}
	fill(img, area, ctx.CellBackgroundColor(cell))
}

// DrawCellFg draws cell text and decorations. [Renderer] runs this in a second
// pass after all cell backgrounds. When [Context] carries an active glyph
// coverage pass (NRGBA destinations), [glyphGrid] glyphs accumulate for
// seam-safe compositing and [glyphPowerline]/[glyphNerdIcon] glyphs plus
// decorations draw afterward.
func DrawCellFg(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell) {
	if !textVisible(ctx, cell) {
		return
	}
	fg, _ := ctx.CellColors(cell)
	layout, hasLayout := layoutGlyph(ctx, area, cell, fg)
	if pass := ctx.glyphPass; pass != nil {
		if hasLayout {
			if layout.kind == glyphGrid {
				accumulateGlyphLayout(ctx, pass.cov, layout)
			} else {
				pass.glyphs = append(pass.glyphs, layout)
			}
		}
		pass.decorations = append(pass.decorations, fgDecoration{area: area, cell: cell, fg: fg})
		return
	}
	if hasLayout {
		drawGlyphLayout(ctx, img, layout)
	}
	drawDecorations(ctx, img, area, cell, fg)
}

// DrawCursor is the default implementation of [CursorDrawer] used by [Renderer].
func DrawCursor(ctx Context, img draw.Image, area image.Rectangle) {
	state := ctx.EmulatorState()
	metrics := ctx.Metrics()

	switch state.CursorStyle {
	case uv.CursorUnderline:
		area.Min.Y = max(area.Min.Y, area.Max.Y-metrics.CursorThickness.Int())
	case uv.CursorBar:
		area.Max.X = min(area.Max.X, area.Min.X+metrics.CursorThickness.Int())
	case uv.CursorBlock:
		height := min(metrics.CursorHeight.Int(), area.Dy())
		area.Min.Y += (area.Dy() - height) / 2
		area.Max.Y = area.Min.Y + height
	}

	fill(img, area, ctx.CursorColor())
	if cursorCell := ctx.CursorCell(); cursorCell != nil && textVisible(ctx, cursorCell) {
		drawGlyph(ctx, img, ctx.CursorBounds(), cursorCell, ctx.CursorTextColor())
	}
}

// DrawScrollbar is the default implementation of [ScrollbarDrawer] used by [Renderer].
func DrawScrollbar(ctx Context, img draw.Image, area image.Rectangle) {
	screenRows := ctx.ScreenBounds().Dy()
	totalRows := ctx.cfg.state.ScrollbackCount + screenRows
	if screenRows <= 0 || totalRows <= screenRows {
		return
	}

	fg := ctx.ForegroundColor()
	bg := ctx.BackgroundColor()
	track := blendColor(bg, fg, 0.12)
	thumb := blendColor(bg, fg, 0.35)
	fill(img, area, track)

	thumbHeight := max(1, area.Dy()*screenRows/totalRows)
	thumbHeight = min(thumbHeight, area.Dy())
	thumbArea := area
	thumbArea.Min.Y = area.Max.Y - thumbHeight
	fill(img, thumbArea, thumb)
}

// DrawBackground is the default implementation of [BackgroundDrawer] used by [Renderer].
func DrawBackground(ctx Context, img draw.Image, area image.Rectangle) {
	fill(img, area, ctx.MarginColor())
	fill(img, ctx.WindowBounds(), ctx.WindowBackgroundColor())
}

// fillUniforms caches [image.Uniform] values for the generic [draw.Draw] fallback
// in [fill] and [overlay]. Avoids allocating a new Uniform on every underline
// dash, decoration segment, and non-NRGBA destination fill.
var fillUniforms sync.Map // color.NRGBA -> *image.Uniform

// fill paints area with a solid color using [draw.Src].
func fill(img draw.Image, area image.Rectangle, c color.Color) {
	if area.Empty() {
		return
	}
	col := nrgba(c)
	if dst, ok := img.(*image.NRGBA); ok {
		// [Renderer] always renders into *image.NRGBA. Writing Pix directly avoids
		// draw.Draw -> DrawMask -> Uniform.RGBA64At per pixel, which dominated
		// GIF frame time (~85% CPU) when fill ran once per cell plus backgrounds.
		b := dst.Bounds()
		area = area.Intersect(b)
		if area.Empty() {
			return
		}
		stride := dst.Stride
		for y := area.Min.Y; y < area.Max.Y; y++ {
			off := (y-b.Min.Y)*stride + (area.Min.X-b.Min.X)*4
			row := dst.Pix[off : off+(area.Dx()*4)]
			for i := 0; i < len(row); i += 4 {
				row[i] = col.R
				row[i+1] = col.G
				row[i+2] = col.B
				row[i+3] = col.A
			}
		}
		return
	}
	// Fallback for tests and custom draw.Image implementations.
	u, ok := fillUniforms.Load(col)
	if !ok {
		u = image.NewUniform(col)
		fillUniforms.Store(col, u)
	}
	draw.Draw(img, area, u.(*image.Uniform), image.Point{}, draw.Src)
}

// overlay blends a solid color over area using [draw.Over], for effects such as
// unfocused dimming that must preserve existing pixels underneath.
func overlay(img draw.Image, area image.Rectangle, c color.Color) {
	if area.Empty() {
		return
	}
	col := nrgba(c)
	// Reuse cached Uniforms; overlay only runs for focus dimming but shares the
	// same small set of semitransparent colors as decoration fills.
	u, ok := fillUniforms.Load(col)
	if !ok {
		u = image.NewUniform(col)
		fillUniforms.Store(col, u)
	}
	draw.Draw(img, area, u.(*image.Uniform), image.Point{}, draw.Over)
}

// fallbackGlyphTarget returns the vertical band for [glyphNerdIcon] layout.
func fallbackGlyphTarget(ctx Context, area image.Rectangle, cell *uv.Cell) image.Rectangle {
	if area.Empty() {
		return area
	}

	height := ctx.Metrics().IconHeightSingle.Int()
	if cell != nil && cell.Width > 1 {
		height = ctx.Metrics().IconHeight.Int()
	}
	height = clamp(height, 1, area.Dy())

	target := area
	target.Min.Y = area.Min.Y + (area.Dy()-height)/2
	target.Max.Y = target.Min.Y + height
	return target
}

// powerlineFallbackScale selects Ghostty-style stretch/fit_cover1 for Powerline codepoints.
func powerlineFallbackScale(glyph string) (fallbackScaleMode, bool) {
	r, n := utf8.DecodeRuneInString(glyph)
	if n == 0 || r == utf8.RuneError {
		return scaleDefault, false
	}
	if r >= '\ue0a0' && r <= '\ue0a3' || r == '\ue0cf' {
		return scaleFitCover1, false
	}
	if r < '\ue0b0' || r > '\ue0d7' {
		return scaleDefault, false
	}
	switch r {
	case '\ue0ce', '\ue0d0', '\ue0d1':
		return scaleFitCover1, false
	case '\ue0d2', '\ue0d6':
		return scaleStretch, false
	case '\ue0d4', '\ue0d7':
		return scaleStretch, true
	}
	return scaleStretch, (r-'\ue0b0')%4 >= 2
}

// fallbackGlyphNeedsScale is true when rasterized ink extends outside the target
// cell so [drawFallbackGlyphScaled] must downscale the glyph mask.
func fallbackGlyphNeedsScale(dr, area image.Rectangle) bool {
	if dr.Empty() || area.Empty() {
		return false
	}
	return dr.Max.X > area.Max.X || dr.Min.X < area.Min.X || dr.Max.Y > area.Max.Y || dr.Min.Y < area.Min.Y
}

// drawFallbackGlyphScaled draws a glyph from a symbol/cmap face into target by
// scaling its alpha mask when ink does not fit the cell after [glyphVerticalAdjust].
func drawFallbackGlyphScaled(dst draw.Image, target image.Rectangle, face font.Face, fg color.Color, glyph string, dot fixed.Point26_6, mode fallbackScaleMode, alignEnd bool) {
	r, n := utf8.DecodeRuneInString(glyph)
	if n == 0 || r == utf8.RuneError {
		return
	}
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() || mask == nil || dr.Dx() <= 0 || dr.Dy() <= 0 || target.Empty() {
		return
	}
	srcAlpha := image.NewAlpha(dr)
	draw.DrawMask(srcAlpha, dr, image.Opaque, image.Point{}, mask, maskp, draw.Src)

	var sx, sy float64
	var x0, y0 int
	switch mode {
	case scaleStretch:
		// Ghostty stretches Powerline separators slightly past the cell to hide
		// antialiasing seams (see nerd_font_attributes pad_left/right ≈ -0.03).
		const padX = 0.03
		const padY = 0.005
		tw := float64(target.Dx()) * (1 + 2*padX)
		th := float64(target.Dy()) * (1 + 2*padY)
		sx = tw / float64(dr.Dx())
		sy = th / float64(dr.Dy())
		dw := max(1, int(math.Ceil(float64(dr.Dx())*sx)))
		dh := max(1, int(math.Ceil(float64(dr.Dy())*sy)))
		scaledAlpha := image.NewAlpha(image.Rect(0, 0, dw, dh))
		xdraw.ApproxBiLinear.Scale(scaledAlpha, scaledAlpha.Bounds(), srcAlpha, dr, draw.Src, nil)

		bleedX := max(1, int(math.Round(float64(target.Dx())*padX)))
		bleedY := max(1, int(math.Round(float64(target.Dy())*padY)))
		y0 = target.Min.Y - bleedY + (target.Dy()+2*bleedY-dh)/2
		if alignEnd {
			x0 = target.Max.X + bleedX - dw
		} else {
			x0 = target.Min.X - bleedX
		}
		out := image.Rect(x0, y0, x0+dw, y0+dh)
		draw.DrawMask(dst, out, image.NewUniform(fg), image.Point{}, scaledAlpha, image.Point{}, draw.Over)
		return
	case scaleFitCover1:
		sx = min(float64(target.Dx())/float64(dr.Dx()), float64(target.Dy())/float64(dr.Dy()))
		sy = sx
	default:
		sx = min(1, float64(target.Dx())/float64(dr.Dx()))
		sy = min(1, float64(target.Dy())/float64(dr.Dy()))
	}
	dw := max(1, int(math.Round(float64(dr.Dx())*sx)))
	dh := max(1, int(math.Round(float64(dr.Dy())*sy)))
	scaledAlpha := image.NewAlpha(image.Rect(0, 0, dw, dh))
	xdraw.ApproxBiLinear.Scale(scaledAlpha, scaledAlpha.Bounds(), srcAlpha, dr, draw.Src, nil)

	switch mode {
	case scaleFitCover1:
		x0 = target.Min.X + (target.Dx()-dw)/2
		y0 = target.Min.Y + (target.Dy()-dh)/2
	default:
		x0 = target.Min.X + (target.Dx()-dw)/2
		y0 = target.Min.Y + (target.Dy()-dh)/2
	}
	out := image.Rect(x0, y0, x0+dw, y0+dh)
	draw.DrawMask(dst, out, image.NewUniform(fg), image.Point{}, scaledAlpha, image.Point{}, draw.Over)
}

// glyphRasterBounds returns the device rectangle for the first rune of glyph at dot.
func glyphRasterBounds(face font.Face, dot fixed.Point26_6, glyph string) (image.Rectangle, bool) {
	r, n := utf8.DecodeRuneInString(glyph)
	if n == 0 || r == utf8.RuneError {
		return image.Rectangle{}, false
	}
	dr, _, _, _, ok := face.Glyph(dot, r)
	if !ok || dr.Empty() {
		return image.Rectangle{}, false
	}
	return dr, true
}

// glyphVerticalAdjust returns a delta (in pixels) to add to the font dot Y so
// glyph ink fits inside area as much as possible. Primary-only faces skip this.
func glyphVerticalAdjust(face font.Face, dot fixed.Point26_6, glyph string, area image.Rectangle) int {
	if area.Empty() || glyph == "" {
		return 0
	}
	adj := 0
	for range 5 {
		cur := dot
		cur.Y += fixed.I(adj)
		dr, ok := glyphRasterBounds(face, cur, glyph)
		if !ok {
			return adj
		}
		delta := 0
		if dr.Max.Y > area.Max.Y {
			delta -= dr.Max.Y - area.Max.Y
		}
		if dr.Min.Y+delta < area.Min.Y {
			delta += area.Min.Y - (dr.Min.Y + delta)
		}
		if delta == 0 {
			return adj
		}
		adj += delta
	}
	return adj
}

// drawDecorations renders underline, strikethrough, and related lines for a cell
// using underline/strike metrics from [Context.Metrics]. Placement follows the
// same top-to-baseline model as Ghostty's font metrics.
//
// https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig
func drawDecorations(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell, fg color.Color) {
	if cell == nil {
		return
	}
	style := cell.Style
	decoration := fg
	if style.UnderlineColor != nil && style.Attrs&uv.AttrConceal == 0 {
		decoration = resolvePaletteColor(ctx, style.UnderlineColor)
		if style.Attrs&uv.AttrFaint != 0 {
			_, bg := ctx.CellColors(cell)
			decoration = blendColor(decoration, bg, ctx.cfg.faintFactor)
		}
	}

	if style.Underline != uv.UnderlineNone {
		drawUnderline(ctx, img, area, style.Underline, decoration)
	}
	if style.Attrs&uv.AttrStrikethrough != 0 {
		metrics := ctx.Metrics()
		y := clamp(area.Min.Y+metrics.StrikethroughPosition.Int(), area.Min.Y, area.Max.Y-1)
		fill(img, image.Rect(area.Min.X, y, area.Max.X, min(area.Max.Y, y+metrics.StrikethroughThickness.Int())), decoration)
	}
}

// periodicStart returns the first x >= min that lies on a global period boundary.
func periodicStart(min, period int) int {
	if period <= 0 {
		return min
	}
	if rem := min % period; rem != 0 {
		return min + period - rem
	}
	return min
}

// drawUnderline paints the requested underline style at Metrics-derived Y and
// thickness.
func drawUnderline(ctx Context, img draw.Image, area image.Rectangle, style uv.Underline, c color.Color) {
	metrics := ctx.Metrics()
	y := clamp(area.Min.Y+metrics.UnderlinePosition.Int(), area.Min.Y, area.Max.Y-1)
	thickness := metrics.UnderlineThickness.Int()
	clipX := ctx.GridBounds().Max.X
	switch style {
	case uv.UnderlineDouble:
		fill(img, image.Rect(area.Min.X, y, area.Max.X, min(area.Max.Y, y+thickness)), c)
		y2 := min(area.Max.Y-1, y+thickness*2)
		fill(img, image.Rect(area.Min.X, y2, area.Max.X, min(area.Max.Y, y2+thickness)), c)
	case uv.UnderlineDotted:
		step := max(2, thickness*2)
		for x := periodicStart(area.Min.X, step); x < area.Max.X; x += step {
			fill(img, image.Rect(x, y, min(clipX, x+thickness), min(area.Max.Y, y+thickness)), c)
		}
	case uv.UnderlineDashed:
		dash := max(2, area.Dx()/3)
		gap := max(1, thickness*2)
		period := dash + gap
		for x := area.Min.X; x < area.Max.X; {
			phase := x % period
			if phase < dash {
				end := min(clipX, x+dash-phase)
				fill(img, image.Rect(x, y, end, min(area.Max.Y, y+thickness)), c)
				x = end
				continue
			}
			x += period - phase
		}
	case uv.UnderlineCurly:
		pat := [8]int{0, 0, 1, 1, 0, 0, -1, -1}
		for x := area.Min.X; x < area.Max.X; x++ {
			yy := clamp(y+pat[x%8], area.Min.Y, area.Max.Y-1)
			fill(img, image.Rect(x, yy, x+1, min(area.Max.Y, yy+thickness)), c)
		}
	default:
		fill(img, image.Rect(area.Min.X, y, area.Max.X, min(area.Max.Y, y+thickness)), c)
	}
}

// textVisible reports whether a cell's foreground (glyph and decorations) should
// be drawn for the current blink phase. Non-blinking cells are always visible.
//
// Blinking cells delegate to [blinkVisible] with fixed half-cycle durations
// aligned to [Context.cfg.now]:
//   - [uv.AttrBlink]: 500ms on, 500ms off (1s full cycle)
//   - [uv.AttrRapidBlink]: 250ms on, 250ms off (500ms full cycle)
//
// Callers pin the phase with [WithNow]. Tests expect [uv.AttrBlink] text visible
// at the Unix epoch (time.Unix(0, 0)) and hidden one half-cycle later
// (time.Unix(0, int64(500*time.Millisecond))).
func textVisible(ctx Context, cell *uv.Cell) bool {
	if cell == nil {
		return true
	}
	attrs := cell.Style.Attrs
	switch {
	case attrs&uv.AttrRapidBlink != 0:
		return blinkVisible(ctx.cfg.now, 250*time.Millisecond)
	case attrs&uv.AttrBlink != 0:
		return blinkVisible(ctx.cfg.now, 500*time.Millisecond)
	default:
		return true
	}
}

// cursorVisible reports whether the cursor should be drawn for the current blink
// phase. When [EmulatorState.CursorBlink] is false, the cursor is always visible.
//
// The blink half-cycle comes from [frameConfig.cursorBlinkSpeed] (default
// [DefaultCursorBlinkSpeed]). Phase alignment follows [blinkVisible]; tests
// expect the cursor visible at time.Unix(0, 0) and hidden after one half-cycle
// (e.g. time.Unix(1, 0) with [WithCursorBlinkSpeed](time.Second)).
func cursorVisible(ctx Context) bool {
	if !ctx.cfg.state.CursorBlink {
		return true
	}
	return blinkVisible(ctx.cfg.now, ctx.cfg.cursorBlinkSpeed)
}

// blinkVisible reports whether a blinking element is in its on (visible) phase.
//
// Blink timing is derived from now.UnixNano() and halfCycle, where halfCycle is
// the duration of one on or off segment (not the full on+off cycle). The phase
// index uses integer division on nanoseconds:
//
//	phase = now.UnixNano() / int64(halfCycle)
//
// Truncation is toward zero, so each phase spans exactly halfCycle nanoseconds
// and boundaries fall on multiples of halfCycle since the Unix epoch. The element
// is visible when phase is even (phase%2 == 0) and hidden when odd. At
// time.Unix(0, 0) the on phase begins; after halfCycle elapses the off phase
// begins, and the pattern repeats every 2*halfCycle.
//
// When halfCycle <= 0, blinkVisible always returns true.
//
// [WithNow] supplies now for deterministic screenshots and GIF frames. Callers
// and tests should treat phase as epoch-aligned, not calendar-aligned.
func blinkVisible(now time.Time, halfCycle time.Duration) bool {
	if halfCycle <= 0 {
		return true
	}
	return now.UnixNano()/int64(halfCycle)%2 == 0
}
