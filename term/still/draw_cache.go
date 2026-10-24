// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"time"

	icol "github.com/lrstanley/x/term/still/internal/color"
	"github.com/lrstanley/x/term/vt/screen"
)

// frameSnap is everything outside the cell grid that can change pixels
// between draws: the screen bounds and the [EmulatorState]. Draw skips a
// repaint only when this still matches the frame it already filled. Options,
// palette, fonts, and metrics are fixed by [New], so they are not tracked.
type frameSnap struct {
	screen image.Rectangle

	title string     // only tracked with window chrome, which is the only thing that paints it
	ind   indicators // zero unless a progress bar or program status is drawn

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

// reuseFrame reports whether d.frame already shows scr at now. Blink is
// checked last, and only once per cell-content generation.
func (d *Renderer) reuseFrame(scr screen.Screen, now time.Time) bool {
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
	snap := d.currentSnap(buf.Bounds(), now)
	if snap != d.frameSnap || d.frame.Bounds() != d.drawBounds(snap.screen) {
		return false
	}
	return !d.cursorBlinks(snap) && !snap.ind.animates() && !d.cellsBlink(buf, gen)
}

// cursorBlinks reports whether s shows a cursor that is animating.
func (d *Renderer) cursorBlinks(s frameSnap) bool {
	return s.hasEmu && s.curOn && s.curBlink && d.opts.CursorBlinkSpeed > 0
}

// rememberFrame records scr, drawn as of now, as what d.frame shows. lo..hi
// are the cell rows that changed since the previous remembered frame; they are
// copied to d.prev.
func (d *Renderer) rememberFrame(scr screen.Screen, lo, hi int, now time.Time) {
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
	d.frameSnap = d.currentSnap(buf.Bounds(), now)
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
	return image.Rectangle{Max: d.imageSize(screen)}
}

// currentSnap is the frameSnap of a screen of bounds drawn now.
func (d *Renderer) currentSnap(bounds image.Rectangle, now time.Time) frameSnap {
	s := frameSnap{
		screen: bounds,
		ind:    d.shownIndicators(d.emulatorState, now),
	}
	if st := d.emulatorState; st != nil {
		s.hasEmu = true
		if d.opts.WindowChrome {
			s.title = st.Title
		}
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

// drawIncremental repaints only the cell rows that differ from d.prev (plus
// the cursor's old and new rows) and the indicators that changed or animate
// into d.frame, as of at, reporting whether it could. Everything outside
// those bands is untouched.
func (d *Renderer) drawIncremental(scr screen.Screen, at time.Time) bool {
	buf, ok := scr.(*screen.Buffer)
	if !ok || !d.frameReady || d.frame == nil || buf.Generation() == 0 || len(d.prev) != buf.Width()*buf.Height() {
		return false
	}
	now, old := d.currentSnap(buf.Bounds(), at), d.frameSnap
	if now.sansBanded() != old.sansBanded() || d.frame.Bounds() != d.drawBounds(now.screen) ||
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
	if now.title != old.title {
		// The title bar sits above row 0, whose band reaches the window top.
		lo, hi = 0, max(hi, 0)
	}
	if hi < lo && now.ind == old.ind && !now.ind.animates() {
		d.rememberFrame(scr, 0, -1, at)
		return true
	}
	f := d.borrowRenderFrame(image.Point{}, scr, at)
	defer releaseRenderFrame(f)
	// The changed rows, then the indicator bands.
	var bands [6]image.Rectangle
	if hi >= lo {
		band := f.imageBounds
		if r0 := max(0, lo-1); r0 > 0 {
			band.Min.Y = f.gridBounds.Min.Y + r0*f.cellH
		}
		if r1 := min(buf.Height(), hi+2); r1 < buf.Height() {
			band.Max.Y = f.gridBounds.Min.Y + r1*f.cellH
		}
		bands[0] = band
	}
	ind := f.repaintBands(old.ind, now.ind)
	copy(bands[1:], ind[:])
	d.frameReady = false
	d.renderBands(d.frame, scr, f, bands[:]...)
	d.rememberFrame(scr, lo, hi, at)
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

// sansBanded is s without the cursor, title, progress, and program status,
// which drawIncremental repaints as bands instead of rebuilding the frame.
func (s frameSnap) sansBanded() frameSnap {
	s.curOn, s.curBlink, s.curX, s.curY, s.curStyle = false, false, 0, 0, 0
	s.title = ""
	s.ind = indicators{}
	return s
}
