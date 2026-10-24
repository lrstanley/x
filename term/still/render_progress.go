// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"time"

	icol "github.com/lrstanley/x/term/still/internal/color"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/still/units"
)

// Progress bar colors when the palette does not set them.
var (
	defaultProgress      = color.NRGBA{R: 0x0a, G: 0x84, B: 0xff, A: 0xff}
	defaultProgressError = color.NRGBA{R: 0xff, G: 0x45, B: 0x3a, A: 0xff}
	defaultProgressPause = color.NRGBA{R: 0xff, G: 0x9f, B: 0x0a, A: 0xff}
)

const (
	// progressSweep is how long the indeterminate segment takes to cross the
	// window once.
	progressSweep = 1500 * time.Millisecond
	// progressTrack is the opacity of the full-width track behind the
	// indeterminate segment.
	progressTrack = 0.3
	// The indeterminate segment is this fraction of the window wide at the
	// edges, growing by progressSegGrow at the middle of its sweep.
	progressSegMin  = 0.15
	progressSegGrow = 0.25
	// progressTail is the fraction of the segment, from its trailing end, that
	// fades in. The leading end fades over progressHead bar heights.
	progressTail = 0.7
	progressHead = 1.5
)

// shownProgress is the progress the bar draws for st: none when the bar is
// disabled, there is no state, or its state draws nothing.
func (d *Renderer) shownProgress(st *EmulatorState) types.Progress {
	if !d.opts.ProgressBar || st == nil || st.Progress.State == types.ProgressRemove || st.Progress.State > types.ProgressPause {
		return types.Progress{}
	}
	return st.Progress
}

// progressAnimates reports whether p is drawn as a moving segment, which
// changes with the clock: indeterminate, or an error with no percent.
func progressAnimates(p types.Progress) bool {
	return p.State == types.ProgressIndeterminate || (p.State == types.ProgressError && p.Percent < 0)
}

// layoutProgress sets the progress f draws and the strip it is drawn in: the
// bottom of the title bar over its hairline, or the top edge of the window
// without chrome. Either way it overlays existing pixels instead of moving the
// grid. The strip is empty when the bar is disabled.
func (d *Renderer) layoutProgress(f *renderFrame) {
	f.progress = d.shownProgress(d.emulatorState)
	if !d.opts.ProgressBar {
		return
	}
	h := d.opts.ProgressBarHeight.Int()
	r := f.windowBounds
	if !f.chromeBounds.Empty() {
		r = f.chromeBounds
		r.Min.Y = max(r.Min.Y, r.Max.Y-h)
	} else {
		r.Max.Y = min(r.Max.Y, r.Min.Y+h)
	}
	f.progressBounds = r
}

// progressBand is the full-width rows of the image the bar's strip covers:
// what a repaint of the bar has to redraw (see [Renderer.RepaintIndicators]),
// since the strip can overlay the margin fill, the window background, the
// title bar, or the top of row 0. It is empty when the bar is disabled.
func (f *renderFrame) progressBand() image.Rectangle {
	band := f.imageBounds
	band.Min.Y, band.Max.Y = f.progressBounds.Min.Y, f.progressBounds.Max.Y
	return band
}

func (f *renderFrame) progressColor(state types.ProgressState) color.NRGBA {
	c, def := f.palette.Progress, defaultProgress
	switch state {
	case types.ProgressError:
		c, def = f.palette.ProgressError, defaultProgressError
	case types.ProgressPause:
		c, def = f.palette.ProgressPause, defaultProgressPause
	case types.ProgressRemove, types.ProgressSet, types.ProgressIndeterminate:
	}
	return paletteNRGBA(c, def)
}

// paletteNRGBA is the palette color c, or def when the palette leaves it
// unset.
func paletteNRGBA(c color.Color, def color.NRGBA) color.NRGBA {
	if c == nil {
		return def
	}
	return icol.NRGBA(c)
}

// drawProgress paints the OSC 9;4 progress bar. A percent draws a bar from
// the left edge with no track, and pause with no percent fills the strip.
// Indeterminate, and error with no percent, draw a faint track with a segment
// that sweeps left to right on the [WithClock] time, widening toward the
// middle, with a tail that fades out behind it.
func (d *Renderer) drawProgress(dst draw.Image, f *renderFrame) {
	bar, p := f.progressBounds, f.progress
	if p.State == types.ProgressRemove || !bar.Overlaps(dst.Bounds()) {
		return
	}

	w := bar.Dx()
	d.progressRow = slices.Grow(d.progressRow[:0], w)[:w]
	row := d.progressRow
	if progressAnimates(p) {
		progressSweepRow(row, f.now, float64(bar.Dy()))
	} else {
		pct := p.Percent
		if pct < 0 && p.State == types.ProgressPause {
			pct = 100
		}
		fill := float64(w) * float64(units.Clamp(pct, 0, 100)) / 100
		for i := range row {
			row[i] = coverage(fill - float64(i))
		}
	}
	idraw.OverCoverage(dst, bar, f.progressColor(p.State), row)
}

// progressSweepRow writes the indeterminate track and segment into row at
// time now. headPx scales the leading fade with the bar height.
func progressSweepRow(row []uint8, now time.Time, headPx float64) {
	w := float64(len(row))
	u := float64(now.UnixNano()%int64(progressSweep)) / float64(progressSweep)

	// The head eases from the left edge until the tail clears the right edge,
	// then wraps. The segment is narrowest at both ends of the sweep, so it
	// enters and leaves at the same width.
	seg := w * (progressSegMin + progressSegGrow*math.Sin(math.Pi*u))
	head := easeInOut(u) * (w + w*progressSegMin)
	tail := head - seg
	lead := headPx * progressHead

	for i := range row {
		x := float64(i) + 0.5
		a := 0.0
		if x > tail && x < head {
			a = smoothstep((x-tail)/(seg*progressTail)) * min(1, (head-x)/lead)
		}
		row[i] = uint8(math.Round(255 * (1 - (1-progressTrack)*(1-a))))
	}
}

func coverage(v float64) uint8 {
	return uint8(math.Round(255 * units.Clamp(v, 0, 1)))
}

func smoothstep(t float64) float64 {
	t = units.Clamp(t, 0, 1)
	return t * t * (3 - 2*t)
}

// easeInOut is a cubic ease-in-out on 0..1.
func easeInOut(t float64) float64 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	t = -2*t + 2
	return 1 - t*t*t/2
}
