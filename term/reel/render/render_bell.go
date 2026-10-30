// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/draw"
	"math"
	"time"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/vt"
)

// After a BEL, the window flashes bellFlashTint of the way toward the
// foreground and fades out over bellFlash, in bellFlashSteps levels so
// frames within one level can be reused. bellTitle goes before the title, as
// in Ghostty.
const (
	bellFlash      = 300 * time.Millisecond
	bellFlashSteps = 6
	bellFlashTint  = 0.3
	bellTitle      = "🔔"
)

// bellView is what the latest BEL draws at some offset: the flash level,
// from bellFlashSteps right after it down to 0 once faded, and whether the
// title bar shows bellTitle.
type bellView struct {
	flash uint8
	title bool
}

// shownBell is what the latest BEL in st draws at at, with [WithBell]. A
// shown program status takes the title's place, which hides the bell there.
func (d *Renderer) shownBell(st vt.State, at time.Duration, status statusView) bellView {
	age := at - st.Bell.At
	if !d.opts.Bell || st.Bell.Count == 0 || age < 0 {
		return bellView{}
	}
	var v bellView
	if age < bellFlash {
		v.flash = uint8(math.Ceil(float64(bellFlash-age) / float64(bellFlash) * bellFlashSteps))
	}
	v.title = !status.shown && d.opts.WindowChrome && age < d.opts.BellDuration
	return v
}

// titleText is title as the title bar shows it with v.
func (v bellView) titleText(title string) string {
	switch {
	case !v.title:
		return title
	case title == "":
		return bellTitle
	default:
		return bellTitle + " " + title
	}
}

// bellBand is the part of the image to repaint when the bell drawn goes
// from old to now: the window while the flash changes, else the title bar
// while its bell does, else nothing.
func (f *renderFrame) bellBand(old, now bellView) image.Rectangle {
	switch {
	case old.flash != now.flash:
		return f.windowBounds
	case old.title != now.title:
		return f.chromeBounds
	}
	return image.Rectangle{}
}

// drawBellFlash blends the window toward the foreground, by how far the
// flash has yet to fade.
func drawBellFlash(dst draw.Image, f *renderFrame) {
	if f.bell.flash == 0 {
		return
	}
	k := bellFlashTint * float64(f.bell.flash) / bellFlashSteps
	idraw.Overlay(dst, f.windowBounds, icol.ApplyAlphaNRGBA(f.foregroundNRGBA(nil), k))
}
