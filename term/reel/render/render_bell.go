// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
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
func (d *Renderer) shownBell(st vt.State, at time.Duration) bellView {
	age := at - st.Bell.At
	if !d.opts.Bell || st.Bell.Count == 0 || age < 0 {
		return bellView{}
	}
	var v bellView
	if age < bellFlash {
		v.flash = uint8(math.Ceil(float64(bellFlash-age) / float64(bellFlash) * bellFlashSteps))
	}
	v.title = d.opts.WindowChrome && age < d.opts.BellDuration
	return v
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
