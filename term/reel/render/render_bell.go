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

const (
	// bellFlash is how long the window flash after a BEL takes to fade out.
	bellFlash = 300 * time.Millisecond
	// bellFlashSteps is how many levels the flash fades through, so frames
	// within one level look the same and can be reused.
	bellFlashSteps = 6
	// bellFlashTint is how far the flash first blends the window toward the
	// foreground.
	bellFlashTint = 0.3
	// bellTitle is put before the title while the bell shows in the title
	// bar, as Ghostty does.
	bellTitle = "🔔"
)

// bellView is what the latest BEL draws at some offset: the flash level,
// from bellFlashSteps right after it down to 0 once faded, and whether the
// title bar shows bellTitle.
type bellView struct {
	flash uint8
	title bool
}

// shownBell is what the latest BEL in st draws at at, with [WithBell].
// titled is false while a program status takes the title's place, which
// hides the bell there.
func (d *Renderer) shownBell(st vt.State, at time.Duration, titled bool) bellView {
	age := at - st.Bell.At
	if !d.opts.Bell || st.Bell.Count == 0 || age < 0 {
		return bellView{}
	}
	var v bellView
	if age < bellFlash {
		v.flash = uint8(math.Ceil(float64(bellFlash-age) / float64(bellFlash) * bellFlashSteps))
	}
	v.title = titled && d.opts.WindowChrome && age < d.opts.BellDuration
	return v
}

// titled is title as the title bar shows it with v.
func (v bellView) titled(title string) string {
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
