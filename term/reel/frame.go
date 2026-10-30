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

// CellFrame is the terminal's screen and state at an offset into the
// [Timeline].
type CellFrame struct {
	// Screen is an immutable copy of the terminal's screen.
	Screen *vt.Buffer
	// State is the terminal's state, with Focused set from the timeline.
	State vt.State
	At    time.Duration
}

// Frame is one frame of a recording on its way from the [Rasterizer],
// through the [Effect] stages, to the sinks.
type Frame struct {
	// Image is the frame's pixels. Neighboring frames may share it, so treat
	// it as read-only: draw overlays on a copy.
	Image *image.NRGBA
	// Cells is the terminal the frame was drawn from, or nil for frames an
	// effect inserted, such as a card.
	Cells *CellFrame
	// At is the timeline offset the frame shows; inserted frames may leave
	// it zero. Frames start on the [Context.Step] grid step nearest At, so
	// At+Delay (unscaled) may be up to half a step off the next frame's At,
	// except after a Paused frame.
	At time.Duration
	// Delay is how long the frame plays. For terminal frames it is timeline
	// time scaled by [Profile.Speed], in whole steps. Sinks receive it
	// rounded to whole steps, at least one.
	Delay time.Duration
	// Paused marks a frame an effect inserted to pause playback, such as at
	// a mark: it shows the timeline at At for its whole Delay, unscaled by
	// speed, so the next frame also starts at At. Time-varying effects draw
	// a paused frame as of At and must not split it; effects that copy a
	// frame keep the flag.
	Paused bool
	// First and Last mark the first and last frame a stage is given (both
	// for a one-frame recording). The pipeline sets them for each stage.
	First, Last bool
	// Isolated gives the frame its own GIF palette (see
	// [sink.Frame.Isolated]). Set it on cards and on recolored terminal
	// frames, such as dimmed ones.
	Isolated bool
}

// Rasterizer draws a terminal screen into an image. [render.Renderer]
// implements it.
type Rasterizer interface {
	// Layout returns the image layout for a cols by rows screen.
	Layout(cols, rows int) render.Layout
	// Draw draws scr with st as of timeline offset at into dst at
	// dst.Rect.Min, overwriting the [render.Layout.Image] area for scr's
	// size.
	Draw(dst *image.NRGBA, scr vt.Screen, st vt.State, at time.Duration)
	Close() error
}

// Animator is implemented by a [Rasterizer] whose output changes over time
// while the terminal holds still, such as an indeterminate progress bar.
// A held frame is then played as several frames, each repainted for its
// time. [render.Renderer] implements it.
type Animator interface {
	// Animates reports whether images drawn with st change over time.
	Animates(st vt.State) bool
	// Changes reports whether an image drawn with st may differ between
	// offsets from and to.
	Changes(st vt.State, from, to time.Duration) bool
	// Repaint redraws the time-varying parts of dst, an image of scr with
	// st drawn as of offset from, as of offset at.
	Repaint(dst *image.NRGBA, scr vt.Screen, st vt.State, from, at time.Duration)
}

var _ Animator = (*render.Renderer)(nil)

// Effect transforms a recording's frames between the [Rasterizer] and the
// sinks, such as a key overlay or a title card. Each export calls Start
// once (and once more for stills if it is a [StillEffect]), so an effect
// shared by parallel recordings must keep per-recording state in its
// [Stage] and release resources with [Context.OnClose].
//
// A wrapping effect should have an Unwrap() Effect method so a wrapped
// [Concealer], [Cutter], or [StillEffect] is still found, as with
// [errors.As]. This does not apply to [Describer]: the wrapper must describe
// itself or is not described.
type Effect interface {
	// Start returns the stage that runs the effect for one recording, or nil
	// to leave the recording alone.
	Start(ctx *Context) (Stage, error)
}

// EffectKind is the [Annotation] kind the effects package describes its
// effects with.
const EffectKind = "reel.effect"

// Describer is implemented by an [Effect] that can describe itself as an
// [Annotation], so asciicast outputs of [Export] and [Record] record it and
// a later export of the file can apply it again.
type Describer interface {
	Effect
	// Describe returns the effect's annotation, which [AnnotationSchema]
	// must allow. An error wrapping [errors.ErrUnsupported] leaves the
	// effect out, such as for one with no serializable form.
	Describe() (Annotation, error)
}

// MarkChecker is implemented by a [Describer] that depends on marks, so an
// asciicast output never holds a description naming marks the file lacks:
// [Export] and [Record] leave such a description out.
type MarkChecker interface {
	Describer
	// CheckMarks returns a [NoMarkError] for each mark the effect needs that
	// tl's window lacks, or nil.
	CheckMarks(tl *Timeline) error
}

// Concealer is implemented by an [Effect] that hides part of what a
// recording shows, such as a secret. If any effect in a profile Conceals:
//
//   - every such effect must be listed before every effect that does not;
//   - no output may bypass the effects, as asciicast outputs do;
//   - it also runs on the stills of [File.Marks], like a [StillEffect].
//
// [Export] and [Record] fail before writing anything if the first two do
// not hold, wrapping [ErrRawOutput] for a bypassing output.
type Concealer interface {
	Effect
	// Conceals reports whether the effect hides anything.
	Conceals() bool
}

// Cutter is implemented by an [Effect] that cuts stretches of the timeline
// out of a recording, so they never play, such as setup a viewer need not
// see. An output that skips the effects, as an asciicast file of the
// timeline does, would still play them, so [Export] and [Record] return an
// error wrapping [ErrRawOutput] for a profile with an effect whose Cuts
// reports true and such an output, before they write anything. Unlike a
// [Concealer], a Cutter may come anywhere in the profile, and does not run
// on stills.
type Cutter interface {
	Effect
	// Cuts reports whether the effect cuts anything.
	Cuts() bool
}

// StillEffect is implemented by an [Effect] that also runs on the stills
// of [File.Marks]. Stills run through only these and [Concealer] effects,
// in profile order, started separately on their own [Context]. Each still
// is given as a one-frame recording ([Frame.First] and [Frame.Last] set,
// one [Context.Step] long). The first frame the last effect emits is the
// still, so a stage must emit before it returns; emitting nothing is an
// error.
type StillEffect interface {
	Effect
	// Stills reports whether the effect runs on stills.
	Stills() bool
}

// ErrRawOutput is wrapped by the error [Export] and [Record] return for a
// profile with a [Concealer] or [Cutter] and an output that bypasses
// effects, such as asciicast.
var ErrRawOutput = errors.New("reel: output skips the effects")

// Stage receives each frame of a recording in order and emits frames in its
// place: the frame itself to keep it, nothing to drop it, a changed copy,
// or several to insert or expand. Emitted frames move on immediately, and
// frames without an Image are dropped. A stage runs on one goroutine.
type Stage func(f Frame, emit func(Frame))

// Context describes a recording to the [Effect] stages it runs.
type Context struct {
	// Timeline is the timeline being recorded. For [Record] it holds the
	// events replayed so far and grows while recording; once stopped, it
	// holds exactly the events the recording covers.
	Timeline *Timeline
	// Live is set when Timeline grows while recording, as for [Record]. An
	// event an effect looks for, such as a [Mark], may then be appended
	// after Start, though always before the frame at its offset reaches a
	// stage. An effect that needs such an event should report a missing one
	// from a [Context.OnClose] function, which runs once Timeline is
	// complete. Without Live, Timeline is complete at Start, and a mark it
	// lacks is reported the same way.
	Live bool
	// Rasterizer draws the terminal frames; Layout is its layout for the
	// timeline's starting size.
	Rasterizer Rasterizer
	Layout     render.Layout
	// Render is the options terminal frames are drawn with, including the
	// timeline's palette, so effects can draw matching images.
	Render []render.Option
	// Step is the delay grid: every frame plays a whole number of steps, so
	// effects that expand a frame should split it on multiples of Step.
	Step time.Duration
	// Speed is [Profile.Speed].
	Speed float64
	// Title is [Profile.Title], or the timeline's title if unset.
	Title string
	// Meta is [Profile.Meta], passed through unread.
	Meta any

	closers []func() error
	values  map[any]any
}

// SetValue stores v under key for the rest of the recording, readable with
// [Context.Value] by effects started later and by all stages. As with
// [context.WithValue], key should be an unexported type to avoid
// collisions.
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

// OnClose registers fn to run after the last frame, whether the files are
// written or discarded. Functions run newest first; when the files are
// written, their errors are returned with the recording's. Call it from
// Start or a stage.
func (c *Context) OnClose(fn func() error) {
	c.closers = append(c.closers, fn)
}

// close runs the [Context.OnClose] functions, newest first, once.
func (c *Context) close() error {
	var errs []error
	for i := len(c.closers) - 1; i >= 0; i-- {
		errs = append(errs, c.closers[i]())
	}
	c.closers = nil
	return errors.Join(errs...)
}

// Sink writes a recording's frames to one output; see the sink package.
type Sink = sink.Sink
