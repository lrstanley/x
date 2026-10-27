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

// sweepStep is the shortest time between the frames drawn to keep moving
// indicators (see [Animator]) moving while the screen holds still. 20 fps
// keeps them smooth without adding many frames, whatever [Profile.FPS] is.
const sweepStep = 50 * time.Millisecond

// maxFreeImages bounds the images a pipeline keeps for reuse.
const maxFreeImages = 3

// pipeline turns the frames of a replay into images, runs them through the
// [Effect] stages, and writes them to out. It holds back the newest frame
// until the next one (or finish) says how long it was on screen, and each
// stage holds back one frame so it can tell its stage which frame is last
// once the stages before it have finished. It runs on one goroutine.
//
// Frames play on the delay grid: each frame's end, its offset from the first
// frame scaled by the speed, is rounded to a whole step, and its delay is
// the time from the end of the one before. Rounding the ends rather than
// each delay carries the error forward, so a recording plays as long as its
// timeline from the first frame, scaled, to within half a step, or a step
// when the last frame takes its minimum of one. A frame whose end rounds to
// its start is not shown: the frame after it, the newest state, starts at
// the same step instead.
type pipeline struct {
	rast Rasterizer
	// anim is rast as an [Animator], or nil.
	anim Animator
	settings
	// sweep is sweepStep rounded up to a whole number of steps.
	sweep time.Duration

	newest Frame
	// origin is the offset of the first frame, which the grid counts from,
	// and shown is the end of the frames sent on so far, on the grid.
	origin  time.Duration
	started bool
	shown   time.Duration

	stages []stageState
	out    sink.Sink
	// written counts the frames written to out, and err is the first error
	// writing one. Frames are dropped after it.
	written int
	err     error
	// free holds images written to out, for reuse. Only frames that no
	// stage could have kept are reused.
	free []*image.NRGBA
}

// stageState is one [Stage], with the frame waiting to learn whether it is
// last.
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

// record draws cf as the newest frame, and sends the previous one on with
// the time it was on screen. It returns the first error writing a frame.
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

// finish ends the newest frame at end, extended by the hold, then runs each
// stage's held frame as its last, in order, so every stage has received all
// of its input before its own last frame is decided. It returns the first
// error writing a frame.
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

// release sends the newest frame on, ending at offset until on the grid,
// or drops it when that is the step it starts on. The last frame plays for
// at least one step, then the hold.
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
		// The next frame replaces it. No stage has seen its image.
		p.reuse(f.Image)
		return
	}
	f.Delay, p.shown = end-p.shown, end
	if last {
		f.Delay += p.hold
	}
	if p.anim == nil || f.Delay < 2*p.sweep || !p.anim.Animates(f.Cells.State) {
		p.push(0, f)
		return
	}
	p.sweepHold(f)
}

// sweepHold sends on f, a frame held while its indicators move with the
// timeline, which holding one image would freeze. It plays the hold as
// frames p.sweep apart instead, each a copy of f with the indicators
// repainted as of the timeline time it stands for. A step that would look
// like the one before extends it instead of being drawn. The last frame takes
// the remainder. f's own image may be reused once it is written, so the
// copies come from a clone.
func (p *pipeline) sweepHold(f Frame) {
	step := p.sweep
	base := p.newImage(f.Image.Rect)
	copy(base.Pix, f.Image.Pix)
	cells := *f.Cells
	start, left := f.At, f.Delay
	var shown Frame
	for k := 0; left > 0; k++ {
		cur := f
		cur.At = start + time.Duration(float64(k)*float64(step)*p.speed)
		cur.Delay = step
		if left < 2*step {
			cur.Delay = left
		}
		left -= cur.Delay
		if k > 0 && !p.anim.Changes(cells.State, shown.At, cur.At) {
			shown.Delay += cur.Delay
			continue
		}
		if k > 0 {
			next := p.newImage(base.Rect)
			copy(next.Pix, base.Pix)
			p.anim.Repaint(next, cells.Screen, cells.State, cur.At)
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

// write sends f to out, its delay rounded to whole steps, at least one. The
// pipeline's own frames are on the grid already, but for the hold; frames
// an effect made may not be.
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
		// Without stages nothing else holds the image.
		p.reuse(f.Image)
	}
}

// newImage returns an image covering r, reusing a written one when one is
// large enough. Its pixels are stale: the caller must paint every one.
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
