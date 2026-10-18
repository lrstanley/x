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
	if snap.hasEmu && snap.curOn && snap.curBlink && d.opts.CursorBlinkSpeed > 0 {
		return false
	}
	return !d.cellsBlink(buf, gen)
}

func (d *Renderer) rememberFrame(scr screen.Screen) {
	buf, ok := scr.(*screen.Buffer)
	if !ok || buf == nil {
		return
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
