// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"image"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt"
)

// CellFrame is the terminal as a recording shows it at one point: its screen
// and state, at an offset into the [Timeline].
type CellFrame struct {
	// Screen is a copy of the terminal's screen, which nothing changes.
	Screen *vt.Buffer
	// State is the terminal's state, with Focused set from the timeline.
	State vt.State
	// At is the offset into the timeline the terminal looked like this.
	At time.Duration
}

// Frame is one frame of a recording on its way from the [Rasterizer],
// through the [Effect] stages, to the sinks.
type Frame struct {
	// Image is the frame's pixels. Neighboring frames may share it, so treat
	// it as read-only: draw an overlay on a copy, never in place.
	Image *image.NRGBA
	// Cells is the terminal the frame was drawn from. It is nil for frames
	// an effect inserted, such as a card.
	Cells *CellFrame
	// At is the offset into the timeline the frame shows. A held frame the
	// replay redraws to keep its indicators moving is at the offset each
	// redraw stands for. Frames an effect inserts may leave it zero. A
	// frame starts playing on the delay grid step nearest At, so At plus
	// Delay, unscaled, may be up to half a step off the next frame's At.
	At time.Duration
	// Delay is how long the frame plays. For a terminal frame it is
	// timeline time scaled by [Profile.Speed], a whole number of steps of
	// [Context.Step], plus the [Profile.Hold] for the last frame, which the
	// speed does not scale. The sinks are given it rounded to whole steps,
	// at least one.
	Delay time.Duration
	// First and Last are set for the first and last frame a stage is given.
	// A recording of one frame sets both. The pipeline sets them on the way
	// into each stage, so an effect need not.
	First, Last bool
	// Isolated palettizes the frame on its own GIF palette (see
	// [sink.Frame.Isolated]), as cards should, and terminal frames an
	// effect recolors, such as by dimming them. Terminal frames as drawn
	// leave it false.
	Isolated bool
}

// Rasterizer draws a terminal screen into an image. [render.Renderer] is
// one.
type Rasterizer interface {
	// Layout returns the layout of an image of a cols by rows screen.
	Layout(cols, rows int) render.Layout
	// Draw draws scr with st, as of offset at into the timeline, into dst at
	// dst.Rect.Min, overwriting the [render.Layout.Image] of scr's size.
	Draw(dst *image.NRGBA, scr vt.Screen, st vt.State, at time.Duration)
	// Close releases the rasterizer's resources.
	Close() error
}

// Animator is implemented by a [Rasterizer] whose images move with the
// timeline while the terminal holds still, such as an indeterminate progress
// bar. A frame held on screen while they move is played as several frames,
// each repainted as of the time it stands for. [render.Renderer] is one.
type Animator interface {
	// Animates reports whether images drawn with st move with the timeline.
	Animates(st vt.State) bool
	// Changes reports whether an image drawn with st may look different at
	// offset to than at from.
	Changes(st vt.State, from, to time.Duration) bool
	// Repaint redraws the parts of dst, an image of scr with st, that move
	// with the timeline, as of offset at.
	Repaint(dst *image.NRGBA, scr vt.Screen, st vt.State, at time.Duration)
}

// Effect changes the frames of a recording between the [Rasterizer] and the
// sinks, such as an overlay of the keys pressed or a title card. Each export
// calls Start once, or for a [Redactor], once more for the stills, so an
// effect shared by recordings that run in parallel must keep its
// per-recording state in the [Stage] it returns, and close what it opens
// for one with [Context.OnClose]. The effects package has some.
type Effect interface {
	// Start returns the stage that runs the effect for one recording, or nil
	// to leave the recording alone.
	Start(ctx *Context) (Stage, error)
}

// Redactor is implemented by an [Effect] that hides part of what a
// recording shows, such as a secret, like the effects package's Redact. It
// can only hide what passes through it, so a profile with an effect whose
// Redacts reports true must list it before every effect that does not, and
// may not have an asciicast output, which is the timeline as recorded,
// input included, and never runs through the effects. [Export] and [Record]
// return an error for either, before they write anything: one wrapping
// [ErrUnredacted] for an asciicast output.
//
// Stills of [File.Marks] run through the Redactors, and no other effect:
// they are started once more, for the stills, on a [Context] of their own
// without a Hold, and each still is given to the first as a recording of
// its one frame, with [Frame.First] and [Frame.Last] set, a [Context.Step]
// long. It is given once the timeline is known past that step, which in a
// live recording may be after later stills are drawn. The first frame the
// last Redactor emits for it is the still, so a Redactor's stage must emit
// each frame it is given, hidden, before it returns. One that emits nothing
// for a still is an error. In a live recording without a recording output,
// the stills' stages are also given a frame of the end, a step long, whose
// output is dropped, so they see the whole timeline. The errors they
// report on close that wrap [ErrNoMark] are left to the recording's own
// stages, when there are any.
type Redactor interface {
	Effect
	// Redacts reports whether the effect hides anything.
	Redacts() bool
}

// ErrUnredacted is wrapped by the error [Export] and [Record] return for a
// profile with a [Redactor] and an asciicast output, which would show what
// it hides.
var ErrUnredacted = errors.New("reel: output cannot be redacted")

// Stage receives each frame of a recording in order, and emits the frames to
// record in its place: the frame unchanged to keep it, nothing to drop it, a
// changed copy to overlay or retime it, or several to insert cards or expand
// it. Each emitted frame moves on as soon as it is emitted. Frames without an
// Image are dropped. A stage runs on one goroutine, and is given its first
// and last frame with [Frame.First] and [Frame.Last] set.
type Stage func(f Frame, emit func(Frame))

// Context describes a recording to the [Effect] stages it runs.
type Context struct {
	// Timeline is the timeline being recorded. For [Record] it grows while
	// the recording runs.
	Timeline *Timeline
	// Live is set when Timeline grows while the recording runs, as for
	// [Record]: an event an effect looks for, such as a [Mark], may be
	// appended after Start, though always before a stage is given the frame
	// that plays at its offset. An effect then looks for it as frames
	// arrive, and reports one that never came with a function it registered
	// with [Context.OnClose]. Without Live, the timeline is complete at
	// Start.
	Live bool
	// Rasterizer draws the terminal frames, and Layout is its layout for
	// the timeline's starting size.
	Rasterizer Rasterizer
	Layout     render.Layout
	// Render is the options the terminal frames are drawn with, the
	// timeline's palette included, so an effect can draw a matching image
	// with a renderer of its own.
	Render []render.Option
	// Step is the delay grid: every frame plays for a whole number of steps.
	// An effect that expands one frame into several should split it on
	// multiples of Step.
	Step time.Duration
	// Speed is the playback speed, [Profile.Speed].
	Speed float64
	// Hold is how long the last frame plays past its own time,
	// [Profile.Hold] with its default applied: zero for none.
	Hold time.Duration
	// Title is [Profile.Title], or the timeline's title without one.
	Title string
	// Meta is [Profile.Meta], which reel passes along without reading.
	Meta any

	// closers are the functions [Context.OnClose] registered.
	closers []func() error
	// values are those [Context.SetValue] stored.
	values map[any]any
}

// SetValue stores v under key for the rest of the recording, for the
// effects started after the one that stores it, and the stages of all of
// them, to read with [Context.Value], such as a change one effect makes to
// what another draws. As with [context.WithValue], key should be of an
// unexported type of the package that stores it, so packages never collide.
func (c *Context) SetValue(key, v any) {
	if c.values == nil {
		c.values = make(map[any]any)
	}
	c.values[key] = v
}

// Value returns the value [Context.SetValue] stored under key, or nil.
func (c *Context) Value(key any) any {
	return c.values[key]
}

// OnClose registers fn to run once the recording ends, whether its files are
// written or discarded, such as to close a renderer an effect's stage built.
// It is for an effect's Start and its stage to call. The functions run after
// the last frame, newest first, and when the files are written, their errors
// are returned with the recording's.
func (c *Context) OnClose(fn func() error) {
	c.closers = append(c.closers, fn)
}

// close runs the functions registered with [Context.OnClose], newest first,
// once.
func (c *Context) close() error {
	var errs []error
	for i := len(c.closers) - 1; i >= 0; i-- {
		errs = append(errs, c.closers[i]())
	}
	c.closers = nil
	return errors.Join(errs...)
}

// Sink writes the frames of a recording into one output, see the sink
// package.
type Sink = sink.Sink
