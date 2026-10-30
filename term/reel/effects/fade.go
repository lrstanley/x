// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"image/color"
	"image/draw"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// fadeLevels is how many steps the [Keys] overlay fades out in. Each step
// is a frame that defeats duplicate frame merging, so keep it low.
const fadeLevels = 3

// fadeOpacity is an overlay's opacity at each fade level, opaque first.
var fadeOpacity = [fadeLevels + 1]uint8{255, 192, 128, 64}

// fade is how an overlay shown for shown fades on the delay grid of step:
// opaque for the first two thirds, then fainter over fadeLevels steps.
type fade struct {
	shown, step time.Duration
}

// start is when an overlay shown at offset at starts fade level, on the
// step grid. Level fadeLevels+1 is when it disappears.
func (fd fade) start(at time.Duration, level int) time.Duration {
	hold := fd.shown * 2 / 3
	return (at + hold + (fd.shown-hold)*time.Duration(level-1)/fadeLevels).Round(fd.step)
}

// level is the fade level at offset p of an overlay shown at offset at,
// where 0 is opaque and fadeLevels the faintest, or -1 when it is not
// shown.
func (fd fade) level(at, p time.Duration) int {
	if p < at || p >= fd.start(at, fadeLevels+1) {
		return -1
	}
	level := 0
	for level < fadeLevels && p >= fd.start(at, level+1) {
		level++
	}
	return level
}

// cuts returns the offsets into a delay where overlays shown at offsets ats
// appear or change fade level, starting with 0, sorted, and leaving the
// last piece of the delay at least a step.
func (fd fade) cuts(delay time.Duration, ats ...time.Duration) []time.Duration {
	var cuts []time.Duration
	for _, at := range ats {
		cuts = append(cuts, at)
		for level := 1; level <= fadeLevels+1; level++ {
			cuts = append(cuts, fd.start(at, level))
		}
	}
	return trimCuts(cuts, delay, fd.step)
}

// trimCuts sorts cuts, offsets into a delay on the step grid, and drops
// repeats and those outside the delay or that would leave its last piece
// shorter than a step. It keeps a cut at 0.
func trimCuts(cuts []time.Duration, delay, step time.Duration) []time.Duration {
	cuts = append(cuts, 0)
	slices.Sort(cuts)
	return slices.DeleteFunc(slices.Compact(cuts), func(c time.Duration) bool {
		return c < 0 || (c > 0 && delay-c < step)
	})
}

// drawFaded draws src over dst with its top left corner at at, at opacity
// alpha.
func drawFaded(dst, src *image.NRGBA, at image.Point, alpha uint8) {
	b := src.Bounds()
	mask := image.NewUniform(color.Alpha{A: alpha})
	draw.DrawMask(dst, b.Sub(b.Min).Add(at), src, b.Min, mask, image.Point{}, draw.Over)
}

// splitFrame emits f in pieces split at cuts (see [fade.cuts]), each with
// the image paint returns for the overlay state of its start. Neighboring
// pieces of one state are played as one. A paused frame
// ([reel.Frame.Paused]) shows one moment, so it is not split.
func splitFrame[S comparable](ctx *reel.Context, f reel.Frame, cuts []time.Duration, state func(time.Duration) S, paint func(S) *image.NRGBA, emit func(reel.Frame)) {
	if f.Paused {
		cuts = cuts[:1]
	}
	var out []reel.Frame
	var prev S
	for i, start := range cuts {
		end := f.Delay
		if i+1 < len(cuts) {
			end = cuts[i+1]
		}
		st := state(start)
		if len(out) > 0 && st == prev {
			out[len(out)-1].Delay += end - start
			continue
		}
		prev = st
		piece := f
		piece.At = f.At + time.Duration(float64(start)*ctx.Speed)
		piece.Delay = end - start
		piece.Image = paint(st)
		out = append(out, piece)
	}
	for _, piece := range out {
		emit(piece)
	}
}
