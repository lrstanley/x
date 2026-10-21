// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"time"

	"github.com/lrstanley/x/term/still/export"
	"github.com/lrstanley/x/term/vt"
	"github.com/lrstanley/x/term/vt/screen"
)

// ExportFrame is one recorded frame on its way to the GIF encoder.
type ExportFrame struct {
	// Image is the frame's pixels. Neighboring frames may share it, so treat
	// it as read-only: draw an overlay on a copy, never in place.
	Image image.Image
	// At is the real time the frame appeared on the terminal. Frames a mutator
	// inserts may leave it zero.
	At time.Time
	// Delay is how long the frame plays, already scaled by [WithExportSpeed].
	// The encoder snaps it to the [WithExportFPS] grid, and anything shorter
	// than one step plays for one step.
	Delay time.Duration

	// src is the terminal the frame was drawn from. Frames a mutator returns
	// without one inherit it from the frame they replaced.
	src frameSource
}

// frameSource is the terminal state a frame was drawn from, so a card drawn
// beside it matches its size and window chrome. The zero value means
// unset; a terminal always has at least one column.
type frameSource struct {
	cols, rows int
	state      vt.Snapshot
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

	h   *Harness
	src frameSource
}

// ExportMutator changes the frames of a GIF recording between the terminal
// and the encoder. It receives one frame and returns the frames to record in
// its place: the frame unchanged to keep it, none to drop it, a modified copy
// to overlay or retime it, or several to insert cards or expand it.
//
// Mutators run on the test goroutine. One built for an option shared by
// parallel tests must not keep per-recording state; use
// [ExportContext.First] and [ExportContext.Last] instead.
//
// See [WithExportMutators].
type ExportMutator func(ec ExportContext, frame ExportFrame) []ExportFrame

// exportPipeline runs frames through the [ExportMutator] chain into out. Each
// stage holds back one frame, so it can tell its mutator which frame is last
// once the stages before it have finished. It runs on the test goroutine.
type exportPipeline struct {
	// base holds the fields of every [ExportContext] that do not depend on
	// the frame.
	base   ExportContext
	stages []mutatorStage
	out    chan<- export.Frame
}

type mutatorStage struct {
	mutate ExportMutator
	// held is the frame waiting to learn whether it is last. A nil Image
	// means none; frames without one are dropped on entry.
	held    ExportFrame
	started bool
}

// push hands f to stage i, or to the encoder past the last stage. The encoder
// treats a zero delay as wall time since its last read, so every frame,
// including ones a mutator inserted without a delay, plays for at least one
// step. That also keeps each distinct screen visible when several paints land
// within one tick.
func (p *exportPipeline) push(i int, f ExportFrame) {
	if f.Image == nil {
		return
	}
	if i == len(p.stages) {
		p.out <- export.Frame{Image: f.Image, Delay: max(f.Delay, p.base.Step)}
		return
	}
	s := &p.stages[i]
	if s.held.Image != nil {
		p.run(i, s.held, false, false)
	}
	s.held = f
}

// finish runs each stage's held frame as its last, in order, so every stage
// has received all of its input before its own last frame is decided.
func (p *exportPipeline) finish(failed bool) {
	for i := range p.stages {
		s := &p.stages[i]
		if f := s.held; f.Image != nil {
			s.held = ExportFrame{}
			p.run(i, f, true, failed)
		}
	}
}

func (p *exportPipeline) run(i int, f ExportFrame, last, failed bool) {
	s := &p.stages[i]
	ec := p.base
	ec.First, ec.Last, ec.Failed, ec.src = !s.started, last, failed, f.src
	s.started = true
	for _, out := range s.mutate(ec, f) {
		if out.src.cols == 0 {
			out.src = f.src
		}
		p.push(i+1, out)
	}
}

// introCard is the mutator behind [WithExportIntro].
func introCard(d time.Duration, lines ...CardLine) ExportMutator {
	return func(ec ExportContext, f ExportFrame) []ExportFrame {
		if !ec.First {
			return []ExportFrame{f}
		}
		if len(lines) == 0 {
			if logo := ec.steepLogo(d); logo != nil {
				return append(logo, f)
			}
		}
		card := lines
		if len(card) == 0 {
			card = ec.titleLines(CardLine{Text: "steep", Style: screen.Style{Attrs: screen.AttrBold}})
		}
		return []ExportFrame{{Image: ec.Card(card...), Delay: d}, f}
	}
}

// outroCard is the mutator behind [WithExportOutro].
func outroCard(d time.Duration, lines ...CardLine) ExportMutator {
	return func(ec ExportContext, f ExportFrame) []ExportFrame {
		if !ec.Last {
			return []ExportFrame{f}
		}
		card := lines
		switch {
		case len(card) > 0:
		case ec.Failed:
			card = ec.titleLines(CardLine{Text: "FAIL", Style: screen.Style{Fg: screen.Indexed(1), Attrs: screen.AttrBold}})
			card = append(card, ec.failureLines()...)
		default:
			card = ec.titleLines(CardLine{Text: "PASS", Style: screen.Style{Fg: screen.Indexed(2), Attrs: screen.AttrBold}})
		}
		return []ExportFrame{f, {Image: ec.Card(card...), Delay: d}}
	}
}
