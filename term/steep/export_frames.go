// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"sync/atomic"
	"time"

	"github.com/lrstanley/x/term/still/export"
	"github.com/lrstanley/x/term/vt"
)

// ExportFrame is one recorded frame on its way to the encoders (the GIF and any
// video).
type ExportFrame struct {
	// Image is the frame's pixels, an *image.NRGBA for recorded frames and
	// cards. Neighboring frames may share it, so treat it as read-only: draw
	// an overlay on a copy, never in place. Return *image.NRGBA or
	// *image.RGBA images; the encoder maps other types to the GIF palette
	// much more slowly.
	Image image.Image
	// At is the real time the frame appeared on the terminal. Frames a mutator
	// inserts may leave it zero.
	At time.Time
	// Delay is how long the frame plays, already scaled by [WithExportSpeed].
	// The encoder snaps it to the [WithExportFPS] grid, and anything shorter
	// than one step plays for one step.
	Delay time.Duration
	// Isolated palettizes this frame on its own GIF palette (see
	// [export.Frame.Isolated]). Intro cards, the logo, and the outro set it.
	// A mutator can set it on frames it inserts. Terminal frames leave it false.
	Isolated bool
}

// frameSource is the terminal state a frame was drawn from, so a card drawn
// beside it matches its size and window chrome.
type frameSource struct {
	cols, rows int
	state      vt.Snapshot
	// held keeps the frame's indicators (the progress bar and program
	// status) moving while it is held. It is nil unless they move with the
	// clock.
	held *heldIndicators
}

// ExportContext tells an [ExportMutator] where a frame sits in the recording.
type ExportContext struct {
	// First is true for the first frame the mutator sees, and Last for the
	// final one. A recording with a single frame sets both. The last frame is
	// the last screen the test observed, not the program's exit repaint.
	First, Last bool
	// Failed is the test result. It is only set on the last frame, which is
	// mutated from [Harness.Close]; earlier frames are mutated mid-test.
	Failed bool
	// Mode is the recording's [ExportMode]. [ExportOnFailure] recordings are
	// only kept when the test fails, so a mutator can skip them from the first
	// frame on.
	Mode ExportMode
	// Step is one tick of the [WithExportFPS] grid, the shortest a frame
	// plays. A mutator that expands one frame into several (for example, to
	// fade an overlay over a static screen) should split it on multiples of
	// Step.
	Step time.Duration

	h     *Harness
	cards *cardRenderer
	src   frameSource
}

// ExportMutator changes the frames of a GIF recording between the terminal
// and the encoder. It receives one frame and emits the frames to record in
// its place, in order: the frame unchanged to keep it, nothing to drop it, a
// modified copy to overlay or retime it, or several to insert cards or expand
// it. Each emitted frame moves on as soon as it is emitted, so a mutator that
// inserts many need not hold them all. Frames without an Image are dropped.
//
// A frame held on screen with an animated progress bar or program status
// reaches mutators as several frames, each with those repainted so they keep
// moving.
//
// Mutators run on the test goroutine. One built for an option shared by
// parallel tests must not keep per-recording state; use
// [ExportContext.First] and [ExportContext.Last] instead.
//
// See [WithExportMutators].
type ExportMutator func(ec ExportContext, frame ExportFrame, emit func(ExportFrame))

// exportPipeline runs recorded frames through the [ExportMutator] chain into
// every channel of out. It holds back the newest recorded frame until the next
// one (or finish) says how long it was on screen, and each stage holds back one
// frame so it can tell its mutator which frame is last once the stages before
// it have finished. It runs on the test goroutine.
type exportPipeline struct {
	// base holds the fields of every [ExportContext] that do not depend on
	// the frame.
	base ExportContext
	// speed scales real elapsed time into playback time (2 plays twice as
	// fast).
	speed  float64
	newest sourcedFrame
	stages []mutatorStage
	// out holds one channel per recorded output, each of which receives every
	// frame.
	out []chan<- export.Frame
}

// sourcedFrame is a frame with the terminal it was drawn from. Frames a
// mutator emits take the source of the frame it was given. A nil Image
// means none: the pipeline drops frames without one.
type sourcedFrame struct {
	ExportFrame
	src frameSource
}

type mutatorStage struct {
	mutate ExportMutator
	// held is the frame waiting to learn whether it is last.
	held    sourcedFrame
	started bool
}

// sweepStep is the shortest time between the frames export draws to keep an
// animated progress bar or program status moving while the screen holds
// still. 20 fps keeps them smooth without adding many frames, whatever
// [WithExportFPS] is.
const sweepStep = 50 * time.Millisecond

// record makes img, painted at at, the newest frame, and sends the previous
// one into the chain with the time it was on screen.
func (p *exportPipeline) record(img image.Image, at time.Time, src frameSource) {
	p.release(at)
	p.newest = sourcedFrame{ExportFrame{Image: img, At: at}, src}
}

// release sends the newest frame into the chain, playing for the real time
// between its paint and until, scaled by the playback speed. The floor of one
// step keeps paints that land out of order from reaching mutators with a
// negative delay.
func (p *exportPipeline) release(until time.Time) {
	f := p.newest
	if f.Image == nil {
		return
	}
	p.newest = sourcedFrame{}
	f.Delay = max(time.Duration(float64(until.Sub(f.At))/p.speed), p.base.Step)
	base, ok := f.Image.(*image.NRGBA)
	step := p.base.Step
	if step > 0 {
		step = sweepStep + (step-sweepStep%step)%step
	}
	if f.src.held == nil || !ok || step <= 0 || f.Delay < 2*step {
		p.push(0, f)
		return
	}
	// The indicators move with the clock, so holding one image would freeze
	// them. Play the hold as frames sweepStep apart instead, rounded up to a
	// multiple of the step grid, each a copy of the frame with the indicators
	// repainted as of the real time it stands for. A step that would look like
	// the one before, as a blocked status does between its breathing steps,
	// extends it instead of being drawn. The last frame takes the remainder.
	// The encoder may recycle f.Image once it is pushed, so the copies come
	// from a clone.
	base = cloneNRGBA(base)
	start, left := f.At, f.Delay
	var shown sourcedFrame
	for k := 0; left > 0; k++ {
		cur := f
		cur.At = start.Add(time.Duration(float64(k) * float64(step) * p.speed))
		cur.Delay = step
		if left < 2*step {
			cur.Delay = left
		}
		left -= cur.Delay
		if k > 0 && !f.src.held.change(shown.At, cur.At) {
			shown.Delay += cur.Delay
			continue
		}
		if k > 0 {
			next := newNRGBA(base.Rect)
			copy(next.Pix, base.Pix)
			f.src.held.repaint(next, cur.At)
			cur.Image = next
		}
		p.push(0, shown)
		shown = cur
	}
	p.push(0, shown)
}

// push hands f to stage i, or to the encoder past the last stage. The encoder
// treats a zero delay as wall time since its last read, so every frame,
// including ones a mutator inserted without a delay, plays for at least one
// step. That also keeps each distinct screen visible when several paints land
// within one tick.
func (p *exportPipeline) push(i int, f sourcedFrame) {
	if f.Image == nil {
		return
	}
	if i == len(p.stages) {
		out := export.Frame{Image: f.Image, Delay: max(f.Delay, p.base.Step), Isolated: f.Isolated}
		if len(p.stages) == 0 {
			// Without mutators nothing else shares the image, so the encoders
			// may recycle it once all of them are done. Mutators can emit one
			// image for several frames.
			out.Done = doneAfter(func() { releaseNRGBA(f.Image) }, len(p.out))
		}
		for _, ch := range p.out {
			ch <- out
		}
		return
	}
	s := &p.stages[i]
	if s.held.Image != nil {
		p.run(i, s.held, false, false)
	}
	s.held = f
}

// finish ends the newest frame at now, then runs each stage's held frame as
// its last, in order, so every stage has received all of its input before its
// own last frame is decided.
func (p *exportPipeline) finish(now time.Time, failed bool) {
	p.release(now)
	for i := range p.stages {
		s := &p.stages[i]
		if f := s.held; f.Image != nil {
			s.held = sourcedFrame{}
			p.run(i, f, true, failed)
		}
	}
}

func (p *exportPipeline) run(i int, f sourcedFrame, last, failed bool) {
	s := &p.stages[i]
	ec := p.base
	ec.First, ec.Last, ec.Failed, ec.src = !s.started, last, failed, f.src
	s.started = true
	s.mutate(ec, f.ExportFrame, func(out ExportFrame) {
		p.push(i+1, sourcedFrame{out, f.src})
	})
}

// doneAfter returns a function that calls done on its n-th call. Each of n
// encoders sharing one frame gets it as [export.Frame.Done], so the image is
// only released after the last encoder has finished with it. The encoders may
// call it from different goroutines.
func doneAfter(done func(), n int) func() {
	if n <= 1 {
		return done
	}
	var left atomic.Int32
	left.Store(int32(n)) //nolint:gosec // The number of outputs is at most four.
	return func() {
		if left.Add(-1) == 0 {
			done()
		}
	}
}
