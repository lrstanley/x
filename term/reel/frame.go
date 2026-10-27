// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"image"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// CellFrame is the terminal as a recording shows it at one point: its screen
// and state, at an offset into the [Timeline].
type CellFrame struct {
	// Screen is a copy of the terminal's screen, which nothing changes.
	Screen *screen.Buffer
	// State is the terminal's state, with Focused set from the timeline.
	State screen.State
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
	// [sink.Frame.Isolated]), as cards should. Terminal frames leave it
	// false.
	Isolated bool
}

// Rasterizer draws a terminal screen into an image. [render.Renderer] is
// one.
type Rasterizer interface {
	// Layout returns the layout of an image of a cols by rows screen.
	Layout(cols, rows int) render.Layout
	// Draw draws scr with st, as of offset at into the timeline, into dst at
	// dst.Rect.Min, overwriting the [render.Layout.Image] of scr's size.
	Draw(dst *image.NRGBA, scr screen.Screen, st screen.State, at time.Duration)
	// Close releases the rasterizer's resources.
	Close() error
}

// Animator is implemented by a [Rasterizer] whose images move with the
// timeline while the terminal holds still, such as an indeterminate progress
// bar. A frame held on screen while they move is played as several frames,
// each repainted as of the time it stands for. [render.Renderer] is one.
type Animator interface {
	// Animates reports whether images drawn with st move with the timeline.
	Animates(st screen.State) bool
	// Changes reports whether an image drawn with st may look different at
	// offset to than at from.
	Changes(st screen.State, from, to time.Duration) bool
	// Repaint redraws the parts of dst, an image of scr with st, that move
	// with the timeline, as of offset at.
	Repaint(dst *image.NRGBA, scr screen.Screen, st screen.State, at time.Duration)
}

// Effect changes the frames of a recording between the [Rasterizer] and the
// sinks, such as an overlay of the keys pressed or a title card. Each export
// calls Start once, so an effect shared by recordings that run in parallel
// must keep its per-recording state in the [Stage] it returns.
type Effect interface {
	// Start returns the stage that runs the effect for one recording, or nil
	// to leave the recording alone.
	Start(ctx *Context) (Stage, error)
}

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
}

// Sink writes the frames of a recording into one output, see the sink
// package.
type Sink = sink.Sink
