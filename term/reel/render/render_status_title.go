// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/charmbracelet/x/ansi"
	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	// statusSpin is one spinner frame.
	statusSpin = 100 * time.Millisecond

	// statusIdleIconTint is how far the idle icon blends from the window
	// background toward the foreground.
	statusIdleIconTint = 0.5
	// statusDotMin is the smallest record dot, in pixels, and statusDotGap
	// the space between dots.
	statusDotMin = 4
	statusDotGap = 3
)

// Title bar icons, Font Awesome glyphs from the bundled Symbols Nerd Font, so
// they share a weight. Done and error use the bare check and times, since
// the circled ones close up at title bar size.
const (
	statusIconIdle       = "" // fa-circle-o
	statusIconDone       = "" // fa-check
	statusIconError      = "" // fa-times
	statusIconBlocked    = "" // fa-pause-circle
	statusIconPermission = "" // fa-shield
	statusIconQuestion   = "" // fa-question-circle
	statusIconAuth       = "" // fa-key
)

// statusSpinner uses full 8-dot braille frames, so it is as tall at every
// step and centers on the title bar line.
var statusSpinner = [...]string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// statusPercent is the working percent text, padded to one width so the
// centered title does not shift as it counts up.
var statusPercent = func() (t [101]string) {
	for i := range t {
		t[i] = fmt.Sprintf("%3d%%", i)
	}
	return t
}()

// statusIconBox is the side, in pixels, of the square status icons are
// fitted into for window buttons button pixels across: just under one and a
// half buttons, so an icon reads a little larger than the buttons it lines
// up with.
func statusIconBox(button int) int {
	return button + button/2 - 1 + button%2
}

// statusLead is what the status title starts with: the state icon, the
// spinner, or the percent, and whether it is an icon.
func statusLead(drv vt.ProgramStatus, at time.Duration) (lead string, icon bool) {
	switch drv.State {
	case vt.ProgramWorking:
		if drv.Progress >= 0 {
			return statusPercent[units.Clamp(drv.Progress, 0, 100)], false
		}
		return statusSpinner[idraw.Cycle(at, statusSpin*time.Duration(len(statusSpinner)))/statusSpin], false
	case vt.ProgramDone:
		return statusIconDone, true
	case vt.ProgramError:
		return statusIconError, true
	case vt.ProgramBlocked:
		switch drv.Kind {
		case vt.ProgramKindPermission:
			return statusIconPermission, true
		case vt.ProgramKindQuestion:
			return statusIconQuestion, true
		case vt.ProgramKindAuth:
			return statusIconAuth, true
		case vt.ProgramKindNone:
		}
		return statusIconBlocked, true
	case vt.ProgramIdle:
	}
	return statusIconIdle, true
}

// statusLabel appends the title bar text for drv after lead to spans, in at
// most maxCells cells past the lead, and returns them with their width in
// cells. The message is a subtitle (see [renderFrame.appendSubtitle]): it
// goes first when there is not enough room, and then the title is
// truncated. The lead takes the state color.
func (f *renderFrame) statusLabel(spans []chromeSpan, drv vt.ProgramStatus, lead string, maxCells int) (out []chromeSpan, cells int) {
	fg, bg := f.foregroundNRGBA(nil), f.backgroundNRGBA(nil)
	accent := f.statusColor(drv.State)
	if drv.State == vt.ProgramIdle {
		accent = icol.BlendNRGBA(bg, fg, statusIdleIconTint)
	}
	spans = append(spans, chromeSpan{text: lead, col: accent, bold: true})
	cells = ansi.StringWidth(lead)
	room := maxCells - cells

	title := plainText(drv.Title)
	if title == "" {
		title = plainText(drv.App)
	}
	if title == "" || room <= 3 {
		return spans, cells
	}
	t, w := fitText(title, room-1)
	spans = append(spans, chromeSpan{text: " "}, chromeSpan{text: t, col: icol.BlendNRGBA(bg, fg, chromeTitleTint)})
	cells += 1 + w
	return f.appendSubtitle(spans, cells, maxCells-cells, plainText(drv.Message), fitText)
}

// drawStatusTitle draws the program status in place of the window title on
// l, centered in the room pixels the window buttons, button pixels across,
// leave in the title bar body. Each record other than the driver adds a dot
// in its own state color after the text, so a tree shows at a glance how
// many records there are and whether any of them also want attention.
func (d *Renderer) drawStatusTitle(l *chromeLine, body image.Rectangle, room, button int) {
	f := l.f
	v := &f.status.view
	cw, _ := d.chromeCtx.cell()
	l.box = statusIconBox(button)
	lead, icon := statusLead(v.drv, f.at)
	iconW := 0
	if icon {
		iconW = max(l.box-cw, 0)
	}
	leadW := ansi.StringWidth(lead)*cw + iconW
	if leadW > room {
		return
	}

	dot := max(statusDotMin, button/2)
	ndots := v.ndots
	if ndots > 0 {
		ndots = min(ndots, max(0, (room-leadW-cw+statusDotGap)/(dot+statusDotGap)))
	}
	dotsW := 0
	if ndots > 0 {
		dotsW = cw + ndots*(dot+statusDotGap) - statusDotGap
	}

	var buf [5]chromeSpan
	spans, cells := f.statusLabel(buf[:0], v.drv, lead, (room-dotsW-iconW)/cw)
	x := l.spans(body.Min.X+(body.Dx()-cells*cw-iconW-dotsW)/2, spans)
	if ndots == 0 {
		return
	}
	if d.status.dot == nil || d.status.dot.Rect.Dx() != dot {
		d.status.dot = idraw.CircleMask(dot)
	}
	x += cw
	top := int(math.Round(l.mid - float64(dot)/2))
	for _, s := range v.dots[:ndots] {
		dr := image.Rect(x, top, x+dot, top+dot)
		draw.DrawMask(l.dst, dr, idraw.Uniform(f.statusColor(s)), image.Point{}, d.status.dot, image.Point{}, draw.Over)
		x += dot + statusDotGap
	}
}
