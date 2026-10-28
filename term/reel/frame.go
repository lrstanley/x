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
	// Delay, unscaled, may be up to half a step off the next frame's At,
	// except for a paused frame (see Paused).
	At time.Duration
	// Delay is how long the frame plays. For a terminal frame it is
	// timeline time scaled by [Profile.Speed], a whole number of steps of
	// [Context.Step]. The sinks are given it rounded to whole steps, at
	// least one.
	Delay time.Duration
	// Paused is set on a frame an effect inserted to pause playback, such
	// as at a mark: it shows the timeline at At for its whole Delay, which
	// the speed does not scale, so the frame after it starts at At too.
	// Effects that change with the timeline draw a paused frame as of At,
	// and do not split it. Effects that copy a frame keep it.
	Paused bool
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
// calls Start once, or for a [StillEffect], once more for the stills, so an
// effect shared by recordings that run in parallel must keep its
// per-recording state in the [Stage] it returns, and close what it opens
// for one with [Context.OnClose]. The effects package has some.
//
// An effect that wraps another, such as to log it, should have an
// Unwrap() Effect method that returns it, so a [Concealer] or
// [StillEffect] it wraps is still found, as [errors.As] finds an error.
type Effect interface {
	// Start returns the stage that runs the effect for one recording, or nil
	// to leave the recording alone.
	Start(ctx *Context) (Stage, error)
}

// Concealer is implemented by an [Effect] that hides part of what a
// recording shows, such as a secret. It can only hide what passes through
// it, so in a profile with an effect whose Conceals reports true, every
// such effect must come before every effect that does not, and no output
// may skip the effects, as an asciicast file of the timeline does. [Export]
// and [Record] return an error for either before they write anything: one
// wrapping [ErrRawOutput] for an output that skips the effects. A
// Concealer runs on stills too, as a [StillEffect] does.
type Concealer interface {
	Effect
	// Conceals reports whether the effect hides anything.
	Conceals() bool
}

// StillEffect is implemented by an [Effect] that also runs on the stills
// of [File.Marks], which run through these effects and the [Concealer]
// effects alone, in the profile's order. They are started once more, for
// the stills, on a [Context] of their own, and each still is
// given to the first as a recording of its one frame, with [Frame.First]
// and [Frame.Last] set, a [Context.Step] long. It is given once the
// timeline is known past that step, which in a live recording may be after
// later stills are drawn. The first frame the last emits for it is the
// still, so such an effect must emit each frame it is given before it
// returns; emitting nothing for a still is an error. The errors these
// effects report on close that wrap [ErrNoMark] are left to the
// recording's own stages, when there are any.
type StillEffect interface {
	Effect
	// Stills reports whether the effect runs on stills.
	Stills() bool
}

// ErrRawOutput is wrapped by the error [Export] and [Record] return for a
// profile with a [Concealer] and an output that skips the effects, such as
// an asciicast file, which would show what it hides.
var ErrRawOutput = errors.New("reel: output skips the effects")

// Stage receives each frame of a recording in order, and emits the frames to
// record in its place: the frame unchanged to keep it, nothing to drop it, a
// changed copy to overlay or retime it, or several to insert cards or expand
// it. Each emitted frame moves on as soon as it is emitted. Frames without an
// Image are dropped. A stage runs on one goroutine, and is given its first
// and last frame with [Frame.First] and [Frame.Last] set.
type Stage func(f Frame, emit func(Frame))

// Context describes a recording to the [Effect] stages it runs.
type Context struct {
	// Timeline is the timeline being recorded. For [Record] it holds the
	// session's events the recording has replayed so far, and grows while
	// it runs; once the recording stops, it holds every event the recording
	// covers, and none appended after.
	Timeline *Timeline
	// Live is set when Timeline grows while the recording runs, as for
	// [Record]: an event an effect looks for, such as a [Mark], may be
	// appended after Start, though always before a stage is given the frame
	// that plays at its offset. An effect then looks for it as frames
	// arrive, and reports one that never came with a function it registered
	// with [Context.OnClose], which runs once Timeline is complete, so it
	// can look once more for those appended after the last frame. Without
	// Live, the timeline is complete at Start.
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
