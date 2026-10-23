// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"

	icol "github.com/lrstanley/x/term/still/internal/color"
	"github.com/lrstanley/x/term/still/internal/fontset"
	"github.com/lrstanley/x/term/vt/screen"
)

// frameSnap is everything outside the cell grid that can change pixels,
// plus the font and metric inputs that paint those cells. Draw skips a
// repaint only when this still matches the frame it already filled.
type frameSnap struct {
	screen   image.Rectangle
	metrics  Metrics
	fonts    *fontset.Set
	boxThick bool

	scrollbar      bool
	border         Px
	margin         Px
	padding        Px
	bgOpacity      float64
	bgOpacityCells bool
	faint          float64
	focusDim       float64
	blinkSpeed     int64
	marginFill     color.NRGBA
	marginFillSet  bool

	palFg, palBg, palCur, palText, palMargin                color.NRGBA
	palFgSet, palBgSet, palCurSet, palTextSet, palMarginSet bool
	palIndex                                                uint64

	hasEmu               bool
	focused              bool
	alt                  bool
	curOn                bool
	curX, curY           int
	curStyle             screen.CursorShape
	curBlink             bool
	scrollback           int
	fg, bg, cur          color.NRGBA
	fgSet, bgSet, curSet bool
}

// reuseFrame reports whether d.frame already shows scr. Blink is checked
// last, and only once per cell-content generation.
func (d *Renderer) reuseFrame(scr screen.Screen) bool {
	if !d.frameReady || d.frame == nil || scr == nil {
		return false
	}
	buf, ok := scr.(*screen.Buffer)
	if !ok || !bufferClean(buf) {
		return false
	}
	gen := buf.Generation()
	if gen == 0 || gen != d.frameGen {
		return false
	}
	snap := d.currentSnap(buf.Bounds())
	if snap != d.frameSnap || d.frame.Bounds() != d.drawBounds(snap.screen) {
		return false
	}
	return !d.cursorBlinks(snap) && !d.cellsBlink(buf, gen)
}

// cursorBlinks reports whether s shows a cursor that is animating.
func (d *Renderer) cursorBlinks(s frameSnap) bool {
	return s.hasEmu && s.curOn && s.curBlink && d.opts.CursorBlinkSpeed > 0
}

// rememberFrame records scr as what d.frame shows. lo..hi are the cell rows
// that changed since the previous remembered frame; they are copied to d.prev.
func (d *Renderer) rememberFrame(scr screen.Screen, lo, hi int) {
	buf, ok := scr.(*screen.Buffer)
	if !ok || buf == nil {
		return
	}
	b, w := buf.Bounds(), buf.Width()
	if len(d.prev) != w*b.Dy() {
		d.prev, lo, hi = make([]screen.Cell, w*b.Dy()), 0, b.Dy()-1
	}
	for y := max(lo, 0); y <= min(hi, b.Dy()-1); y++ {
		for x := range w {
			d.prev[y*w+x] = *buf.CellAt(b.Min.X+x, b.Min.Y+y)
		}
	}
	d.frameGen = buf.Generation()
	d.frameSnap = d.currentSnap(buf.Bounds())
	d.frameReady = true
}

func (d *Renderer) cellsBlink(buf *screen.Buffer, gen uint64) bool {
	if d.blinkGen == gen {
		return d.blinkOn
	}
	on := buf.Blinks()
	d.blinkGen = gen
	d.blinkOn = on
	return on
}

func bufferClean(b *screen.Buffer) bool {
	if b == nil || b.Dirty == nil || len(b.Dirty) != b.Height() {
		return false
	}
	for _, row := range b.Dirty {
		if row {
			return false
		}
	}
	return true
}

// drawBounds is the origin-zero image rectangle Draw paints. It matches
// populateRenderFrame with a zero origin.
func (d *Renderer) drawBounds(screen image.Rectangle) image.Rectangle {
	cell := d.metrics.CellSize()
	margin := d.opts.Margin.Int()
	padding := d.opts.Padding.Int()
	bar := 0
	if d.opts.Scrollbar {
		bar = scrollbarWidthPx
	}
	w := screen.Dx()*cell.X + bar + 2*padding + 2*margin
	h := screen.Dy()*cell.Y + 2*padding + 2*margin
	return image.Rectangle{Max: image.Pt(w, h)}
}

func (d *Renderer) currentSnap(bounds image.Rectangle) frameSnap {
	s := frameSnap{
		screen:         bounds,
		metrics:        d.metrics,
		fonts:          d.fonts,
		boxThick:       d.boxThicknessOverride,
		scrollbar:      d.opts.Scrollbar,
		border:         d.opts.BorderRadius,
		margin:         d.opts.Margin,
		padding:        d.opts.Padding,
		bgOpacity:      d.opts.BackgroundOpacity,
		bgOpacityCells: d.opts.BackgroundOpacityCells,
		faint:          d.opts.FaintFactor,
		focusDim:       d.opts.FocusDimming,
		blinkSpeed:     int64(d.opts.CursorBlinkSpeed),
		palIndex:       indexedKey(d.palette.Indexed),
	}
	s.marginFill, s.marginFillSet = colorKey(d.opts.MarginFill)
	s.palFg, s.palFgSet = colorKey(d.palette.DefaultForeground)
	s.palBg, s.palBgSet = colorKey(d.palette.DefaultBackground)
	s.palCur, s.palCurSet = colorKey(d.palette.Cursor)
	s.palText, s.palTextSet = colorKey(d.palette.CursorText)
	s.palMargin, s.palMarginSet = colorKey(d.palette.Margin)
	if st := d.emulatorState; st != nil {
		s.hasEmu = true
		s.focused = st.Focused
		s.alt = st.AltScreen
		s.curOn = st.CursorVisible
		s.curX = st.CursorX
		s.curY = st.CursorY
		s.curStyle = st.CursorStyle
		s.curBlink = st.CursorBlink
		s.scrollback = st.ScrollbackCount
		s.fg, s.fgSet = colorKey(st.FgColor)
		s.bg, s.bgSet = colorKey(st.BgColor)
		s.cur, s.curSet = colorKey(st.CursorColor)
	}
	return s
}

func colorKey(c color.Color) (color.NRGBA, bool) {
	if c == nil {
		return color.NRGBA{}, false
	}
	return icol.NRGBA(c), true
}

func indexedKey(m map[int]color.Color) uint64 {
	if len(m) == 0 {
		return 0
	}
	var h uint64
	for k, c := range m {
		n, _ := colorKey(c)
		v := uint64(n.R) | uint64(n.G)<<8 | uint64(n.B)<<16 | uint64(n.A)<<24
		h += (uint64(k) + 1) * 0x9E3779B97F4A7C15
		h += v * 0xBF58476D1CE4E5B9
	}
	return h ^ uint64(len(m))
}

// drawIncremental repaints only the cell rows that differ from d.prev (plus
// the cursor's old and new rows) into d.frame, reporting whether it could.
// The band is repainted from the background up, so the effects that run
// after the cells (dimming, rounded mask, cursor, scrollbar) apply to fresh
// pixels, and everything outside the band is untouched.
func (d *Renderer) drawIncremental(scr screen.Screen) bool {
	buf, ok := scr.(*screen.Buffer)
	if !ok || !d.frameReady || d.frame == nil || buf.Generation() == 0 || len(d.prev) != buf.Width()*buf.Height() {
		return false
	}
	now, old := d.currentSnap(buf.Bounds()), d.frameSnap
	if now.sansCursor() != old.sansCursor() || d.frame.Bounds() != d.drawBounds(now.screen) ||
		d.cursorBlinks(now) || d.cellsBlink(buf, buf.Generation()) {
		return false
	}
	lo, hi, ok := d.dirtyRows(buf)
	if !ok {
		return false
	}
	if now.cursor() != old.cursor() {
		for _, y := range [...]int{now.curY, old.curY} {
			if y >= 0 && y < buf.Height() {
				lo, hi = min(lo, y), max(hi, y)
			}
		}
	}
	if hi < lo {
		d.rememberFrame(scr, 0, -1)
		return true
	}
	f := d.borrowRenderFrame(image.Point{}, scr)
	defer releaseRenderFrame(f)
	band := f.imageBounds
	if r0 := max(0, lo-1); r0 > 0 {
		band.Min.Y = f.gridBounds.Min.Y + r0*f.cellH
	}
	if r1 := min(buf.Height(), hi+2); r1 < buf.Height() {
		band.Max.Y = f.gridBounds.Min.Y + r1*f.cellH
	}
	d.frameReady = false
	d.render(d.frame.SubImage(band).(*image.NRGBA), scr, f)
	d.rememberFrame(scr, lo, hi)
	return true
}

// dirtyRows returns the first and last row of buf that differs from d.prev
// (hi < lo when none). ok is false when cells cannot be compared, which
// happens for non-comparable color types.
func (d *Renderer) dirtyRows(buf *screen.Buffer) (lo, hi int, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	b, w := buf.Bounds(), buf.Width()
	lo, hi = b.Dy(), -1
	for y := range b.Dy() {
		for x := range w {
			if d.prev[y*w+x] != *buf.CellAt(b.Min.X+x, b.Min.Y+y) {
				lo, hi = min(lo, y), y
				break
			}
		}
	}
	return lo, hi, true
}

type cursorSnap struct {
	on, blink bool
	x, y      int
	style     screen.CursorShape
}

func (s frameSnap) cursor() cursorSnap {
	return cursorSnap{s.curOn, s.curBlink, s.curX, s.curY, s.curStyle}
}

// sansCursor is s without the cursor fields, which drawIncremental handles by row.
func (s frameSnap) sansCursor() frameSnap {
	s.curOn, s.curBlink, s.curX, s.curY, s.curStyle = false, false, 0, 0, 0
	return s
}
