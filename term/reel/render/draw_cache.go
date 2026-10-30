// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
)

// frameSnap is everything outside the cell grid that can change pixels
// between draws: the screen bounds and the [vt.State], as drawn at some
// offset. Draw skips a repaint only when this still matches the frame it
// already filled. Options, palette, fonts, and metrics are fixed by [New], so
// they are not tracked.
type frameSnap struct {
	screen image.Rectangle

	title, dir string     // only tracked with window chrome, which is the only thing that paints them
	ind        indicators // zero unless a progress bar, program status, or bell shows

	focused    bool
	alt        bool
	cursor     vt.Cursor
	scrollback int
	scrolled   int
	fg, bg     color.NRGBA
	reverse    bool
	cur        color.NRGBA // zero unless the state sets a cursor color
	curSet     bool
	palette    *vt.Palette    // by pointer: the emulator replaces it only when its colors change
	marks      *vt.PromptMark // by first element and count: the emulator replaces them only when they change
	markCount  int
}

// reuseFrame reports whether d.frame already shows scr with st at at. Blink
// is checked last, and only once per cell-content generation.
func (d *Renderer) reuseFrame(scr vt.Screen, st vt.State, at time.Duration) bool {
	if !d.frameReady || d.frame == nil || scr == nil {
		return false
	}
	buf, ok := scr.(*vt.Buffer)
	if !ok || !bufferClean(buf) {
		return false
	}
	gen := buf.Generation()
	if gen == 0 || gen != d.frameGen {
		return false
	}
	snap := d.currentSnap(buf.Bounds(), st, at)
	if snap != d.frameSnap || d.frame.Bounds() != d.drawBounds(snap.screen) {
		return false
	}
	return !d.cursorBlinks(snap) && !snap.ind.animates() && !d.cellsBlink(buf, gen)
}

// cursorBlinks reports whether s shows a cursor that is animating.
func (d *Renderer) cursorBlinks(s frameSnap) bool {
	return s.cursor.Visible && s.cursor.Blink && d.opts.CursorBlinkSpeed > 0
}

// rememberFrame records scr with st, drawn as of at, as what d.frame shows.
// lo..hi are the cell rows that changed since the previous remembered frame;
// they are copied to d.prev.
func (d *Renderer) rememberFrame(scr vt.Screen, st vt.State, lo, hi int, at time.Duration) {
	buf, ok := scr.(*vt.Buffer)
	if !ok || buf == nil {
		return
	}
	b, w := buf.Bounds(), buf.Width()
	if len(d.prev) != w*b.Dy() {
		d.prev, lo, hi = make([]vt.Cell, w*b.Dy()), 0, b.Dy()-1
	}
	for y := max(lo, 0); y <= min(hi, b.Dy()-1); y++ {
		for x := range w {
			d.prev[y*w+x] = *buf.CellAt(b.Min.X+x, b.Min.Y+y)
		}
	}
	d.frameGen = buf.Generation()
	d.frameSnap = d.currentSnap(buf.Bounds(), st, at)
	d.frameImages = len(buf.Images()) > 0
	d.frameReady = true
}

func (d *Renderer) cellsBlink(buf *vt.Buffer, gen uint64) bool {
	if d.blinkGen == gen {
		return d.blinkOn
	}
	on := buf.Blinks()
	d.blinkGen = gen
	d.blinkOn = on
	return on
}

func bufferClean(b *vt.Buffer) bool {
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
	return d.layout(screen.Dx(), screen.Dy()).Image
}

// currentSnap is the frameSnap of a screen of bounds drawn with st at at.
func (d *Renderer) currentSnap(bounds image.Rectangle, st vt.State, at time.Duration) frameSnap {
	s := frameSnap{
		screen:     bounds,
		ind:        d.shownIndicators(st, at),
		focused:    st.Focused,
		alt:        st.AltScreen,
		cursor:     st.Cursor,
		scrollback: st.ScrollbackLen,
		scrolled:   st.ScrollbackOffset,
		fg:         st.Foreground,
		bg:         st.Background,
		reverse:    st.ReverseColors,
		palette:    st.Palette,
	}
	if d.opts.WindowChrome {
		s.title, s.dir = st.Title, st.WorkingDirectory
	}
	if d.opts.PromptMarks && len(st.PromptMarks) > 0 {
		s.marks, s.markCount = &st.PromptMarks[0], len(st.PromptMarks)
	}
	if st.CursorColorSet {
		s.cur, s.curSet = st.CursorColor, true
	}
	return s
}

// drawIncremental repaints only the cell rows that differ from d.prev (plus
// the cursor's old and new rows) and the indicators that changed or animate
// into d.frame, for st as of at, reporting whether it could. Everything
// outside those bands is untouched.
func (d *Renderer) drawIncremental(scr vt.Screen, st vt.State, at time.Duration) bool {
	buf, ok := scr.(*vt.Buffer)
	if !ok || !d.frameReady || d.frame == nil || buf.Generation() == 0 || len(d.prev) != buf.Width()*buf.Height() ||
		d.frameImages || len(buf.Images()) > 0 {
		return false
	}
	now, old := d.currentSnap(buf.Bounds(), st, at), d.frameSnap
	if now.sansBanded() != old.sansBanded() || d.frame.Bounds() != d.drawBounds(now.screen) ||
		d.cursorBlinks(now) || d.cellsBlink(buf, buf.Generation()) {
		return false
	}
	lo, hi, ok := d.dirtyRows(buf)
	if !ok {
		return false
	}
	if now.cursor != old.cursor {
		for _, y := range [...]int{now.cursor.Y, old.cursor.Y} {
			if y >= 0 && y < buf.Height() {
				lo, hi = min(lo, y), max(hi, y)
			}
		}
	}
	if now.title != old.title || now.dir != old.dir {
		// The title bar sits above row 0, whose band reaches the window top.
		lo, hi = 0, max(hi, 0)
	}
	if hi < lo && now.ind == old.ind && !now.ind.animates() {
		d.rememberFrame(scr, st, 0, -1, at)
		return true
	}
	f := d.borrowRenderFrame(image.Point{}, scr, st, at)
	defer releaseRenderFrame(f)
	// The changed rows, then the indicator bands.
	ind := f.repaintBands(old.ind, now.ind)
	var bands [1 + len(ind)]image.Rectangle
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
	copy(bands[1:], ind[:])
	d.frameReady = false
	d.renderBands(d.frame, scr, f, bands[:]...)
	d.rememberFrame(scr, st, lo, hi, at)
	return true
}

// dirtyRows returns the first and last row of buf that differs from d.prev
// (hi < lo when none). ok is false when cells cannot be compared, which
// happens for non-comparable color types.
func (d *Renderer) dirtyRows(buf *vt.Buffer) (lo, hi int, ok bool) {
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

// sansBanded is s without the cursor, title bar text, and indicators, which
// drawIncremental repaints as bands instead of rebuilding the frame.
func (s frameSnap) sansBanded() frameSnap {
	s.cursor = vt.Cursor{}
	s.title, s.dir = "", ""
	s.ind = indicators{}
	return s
}
