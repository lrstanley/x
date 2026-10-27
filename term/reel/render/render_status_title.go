// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/charmbracelet/x/ansi"
	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

const (
	// statusSpin is one spinner frame.
	statusSpin = 100 * time.Millisecond

	// statusIdleIconTint is how far the idle icon blends from the window
	// background toward the foreground.
	statusIdleIconTint = 0.5
	// How far the message, and the dash before it, blend toward the
	// foreground. The title uses chromeTitleTint.
	statusMessageTint = 0.45
	statusDashTint    = 0.3
	// statusMessageMin is the fewest cells left for the message before it is
	// dropped instead of truncated.
	statusMessageMin = 12

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

// statusSpan is a run of title bar text in one color.
type statusSpan struct {
	text string
	col  color.NRGBA
	bold bool
}

// statusLead is what the status title starts with: the state icon, the
// spinner, or the percent, and whether it is an icon.
func statusLead(drv screen.ProgramStatus, at time.Duration) (lead string, icon bool) {
	switch drv.State {
	case screen.ProgramWorking:
		if drv.Progress >= 0 {
			return statusPercent[units.Clamp(drv.Progress, 0, 100)], false
		}
		return statusSpinner[idraw.Cycle(at, statusSpin*time.Duration(len(statusSpinner)))/statusSpin], false
	case screen.ProgramDone:
		return statusIconDone, true
	case screen.ProgramError:
		return statusIconError, true
	case screen.ProgramBlocked:
		switch drv.Kind {
		case screen.ProgramKindPermission:
			return statusIconPermission, true
		case screen.ProgramKindQuestion:
			return statusIconQuestion, true
		case screen.ProgramKindAuth:
			return statusIconAuth, true
		case screen.ProgramKindNone:
		}
		return statusIconBlocked, true
	case screen.ProgramIdle:
	}
	return statusIconIdle, true
}

// statusLabel appends the title bar text for drv after lead to spans, in at
// most maxCells cells past the lead, and returns them with their width in
// cells. The message goes first when there is not enough room: it is
// truncated, then dropped, and then the title is truncated. The lead takes
// the state color, the title the normal title tint, and the message is
// dimmer, so the bar stays calm.
func (f *renderFrame) statusLabel(spans []statusSpan, drv screen.ProgramStatus, lead string, maxCells int) (out []statusSpan, cells int) {
	fg, bg := f.foregroundNRGBA(nil), f.backgroundNRGBA(nil)
	accent := f.statusColor(drv.State)
	if drv.State == screen.ProgramIdle {
		accent = icol.BlendNRGBA(bg, fg, statusIdleIconTint)
	}
	spans = append(spans, statusSpan{text: lead, col: accent, bold: true})
	cells = ansi.StringWidth(lead)
	room := maxCells - cells

	title := plainText(drv.Title)
	if title == "" {
		title = plainText(drv.App)
	}
	hasTitle := title != "" && room > 3
	if hasTitle {
		t, w := fitText(title, room-1)
		spans = append(spans, statusSpan{text: " "}, statusSpan{text: t, col: icol.BlendNRGBA(bg, fg, chromeTitleTint)})
		cells, room = cells+1+w, room-1-w
	}
	const dash = " — "
	if !hasTitle || room < statusMessageMin {
		return spans, cells
	}
	if msg := plainText(drv.Message); msg != "" {
		m, w := fitText(msg, room-ansi.StringWidth(dash))
		spans = append(spans,
			statusSpan{text: dash, col: icol.BlendNRGBA(bg, fg, statusDashTint)},
			statusSpan{text: m, col: icol.BlendNRGBA(bg, fg, statusMessageTint)},
		)
		cells += ansi.StringWidth(dash) + w
	}
	return spans, cells
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

	var buf [5]statusSpan
	spans, cells := f.statusLabel(buf[:0], v.drv, lead, (room-dotsW-iconW)/cw)
	x := body.Min.X + (body.Dx()-cells*cw-iconW-dotsW)/2
	for _, s := range spans {
		x = l.text(x, s.text, s.col, s.bold)
	}
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
