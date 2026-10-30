// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"image"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel/sink"
)

// sweepStep is the minimum spacing of frames drawn to animate [Animator]
// indicators while the screen holds still, independent of [Profile.FPS]:
// 20 fps is smooth without adding many frames.
const sweepStep = 50 * time.Millisecond

// maxFreeImages bounds the images a pipeline keeps for reuse.
const maxFreeImages = 3

// pipeline draws a replay's frames, runs them through the [Effect] stages,
// and writes them to out, on one goroutine. It holds back the newest frame
// until the next one (or finish) fixes its duration, and each stage holds
// back one frame so it can be told which is last.
//
// Frame ends (scaled offsets from the first frame) are rounded to the step
// grid, and delays are differences of ends. Rounding ends rather than
// delays keeps the error from accumulating, so total length matches the
// scaled timeline to within half a step (a step if the last frame is
// stretched to its minimum). A frame whose end rounds to its start is
// dropped in favor of the newer one.
type pipeline struct {
	rast Rasterizer
	// anim is rast as an [Animator], or nil.
	anim Animator
	settings
	// sweep is sweepStep rounded up to a whole number of steps.
	sweep time.Duration

	newest Frame
	// origin is the first frame's offset, where the grid starts; shown is
	// the grid end of the frames sent on so far.
	origin  time.Duration
	started bool
	shown   time.Duration

	stages []stageState
	out    sink.Sink
	// written counts frames written to out; err is the first write error,
	// after which frames are dropped.
	written int
	err     error
	// free holds images for reuse. Only images no stage could have kept are
	// put here.
	free []*image.NRGBA
}

// stageState is a [Stage] and the frame it holds back until it is known
// whether it is last.
type stageState struct {
	run     Stage
	held    Frame
	started bool
}

func newPipeline(rast Rasterizer, s settings, stages []Stage, out sink.Sink) *pipeline {
	p := &pipeline{rast: rast, settings: s, out: out}
	p.sweep = sweepStep + (s.step-sweepStep%s.step)%s.step
	p.anim, _ = rast.(Animator)
	for _, st := range stages {
		p.stages = append(p.stages, stageState{run: st})
	}
	return p
}

// record draws cf as the newest frame and sends the previous one on. It
// returns the first write error.
func (p *pipeline) record(cf CellFrame) error {
	b := cf.Screen.Bounds()
	img := p.newImage(p.rast.Layout(b.Dx(), b.Dy()).Image)
	p.rast.Draw(img, cf.Screen, cf.State, cf.At)
	if !p.started {
		p.origin, p.started = cf.At, true
	}
	p.release(cf.At, false)
	p.newest = Frame{Image: img, Cells: &cf, At: cf.At}
	return p.err
}

// finish ends the newest frame at end, then flushes each stage's held frame
// as its last, in stage order so each stage has all its input first. It
// returns the first write error.
func (p *pipeline) finish(end time.Duration) error {
	p.release(end, true)
	for i := range p.stages {
		s := &p.stages[i]
		if f := s.held; f.Image != nil {
			s.held = Frame{}
			p.run(i, f, true)
		}
	}
	return p.err
}

// release sends the newest frame on, ending at until on the grid, or drops
// it if that is the step it starts on. The last frame plays at least one
// step.
func (p *pipeline) release(until time.Duration, last bool) {
	f := p.newest
	if f.Image == nil {
		return
	}
	p.newest = Frame{}
	steps := math.Round(float64(until-p.origin) / p.speed / float64(p.step))
	end := time.Duration(steps) * p.step
	if last {
		end = max(end, p.shown+p.step)
	}
	if end <= p.shown {
		// No stage has seen the image, so it can be reused.
		p.reuse(f.Image)
		return
	}
	f.Delay, p.shown = end-p.shown, end
	if p.anim == nil || f.Delay < 2*p.sweep || !p.anim.Animates(f.Cells.State) {
		p.push(0, f)
		return
	}
	p.sweepHold(f)
}

// sweepHold sends on f, a long-held frame with animated indicators, as
// frames p.sweep apart, each repainted for its time. Steps that would look
// unchanged extend the previous frame instead, and the last takes the
// remainder. Copies come from a clone because f's image may be reused once
// written.
func (p *pipeline) sweepHold(f Frame) {
	step := p.sweep
	base := p.newImage(f.Image.Rect)
	copy(base.Pix, f.Image.Pix)
	cells := *f.Cells
	start, left := f.At, f.Delay
	var shown Frame
	var played time.Duration
	for k := 0; left > 0; k++ {
		cur := f
		cur.At = start + time.Duration(float64(played)*p.speed)
		cur.Delay = step
		if left < 2*step {
			cur.Delay = left
		}
		left -= cur.Delay
		played += cur.Delay
		if k > 0 && !p.anim.Changes(cells.State, shown.At, cur.At) {
			shown.Delay += cur.Delay
			continue
		}
		if k > 0 {
			next := p.newImage(base.Rect)
			copy(next.Pix, base.Pix)
			p.anim.Repaint(next, cells.Screen, cells.State, start, cur.At)
			at := cells
			at.At = cur.At
			cur.Image, cur.Cells = next, &at
		}
		p.push(0, shown)
		shown = cur
	}
	p.push(0, shown)
	p.reuse(base)
}

// push hands f to stage i, or past the last stage to out.
func (p *pipeline) push(i int, f Frame) {
	if f.Image == nil {
		return
	}
	if i == len(p.stages) {
		p.write(f)
		return
	}
	s := &p.stages[i]
	if s.held.Image != nil {
		p.run(i, s.held, false)
	}
	s.held = f
}

func (p *pipeline) run(i int, f Frame, last bool) {
	s := &p.stages[i]
	f.First, f.Last = !s.started, last
	s.started = true
	s.run(f, func(out Frame) { p.push(i+1, out) })
}

// write sends f to out with its delay rounded to whole steps, at least one,
// since effect-made frames may be off the grid.
func (p *pipeline) write(f Frame) {
	if p.err != nil {
		return
	}
	steps := max(1, math.Round(float64(f.Delay)/float64(p.step)))
	p.err = p.out.WriteFrame(sink.Frame{
		Image:    f.Image,
		Delay:    time.Duration(steps) * p.step,
		Isolated: f.Isolated,
	})
	p.written++
	if len(p.stages) == 0 {
		// Without stages, nothing else holds the image.
		p.reuse(f.Image)
	}
}

// newImage returns an image covering r, reusing a free one if large
// enough. Its pixels are stale: the caller must paint every one.
func (p *pipeline) newImage(r image.Rectangle) *image.NRGBA {
	n := r.Dx() * r.Dy() * 4
	for i, img := range p.free {
		if cap(img.Pix) >= n {
			p.free = append(p.free[:i], p.free[i+1:]...)
			return &image.NRGBA{Pix: img.Pix[:n], Stride: r.Dx() * 4, Rect: r}
		}
	}
	return image.NewNRGBA(r)
}

// reuse keeps img for [pipeline.newImage]. img must not be used again.
func (p *pipeline) reuse(img *image.NRGBA) {
	if len(p.free) < maxFreeImages {
		p.free = append(p.free, img)
	}
}
