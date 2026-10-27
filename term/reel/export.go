// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt"
)

// Export writes the outputs of p from tl, which is replayed into a fresh
// terminal: every event is applied in order, and each time the terminal
// looks different (outside synchronized output) is a frame: new program
// output, or a change of focus or colors.
//
// The frames of a timeline returned by [Timeline.Between] are those of its
// window, which opens on the terminal the events before it left. Frames are
// drawn as of their offset into the timeline, never the wall clock, so
// exporting one timeline twice writes the same bytes, as does exporting the
// asciicast file [Timeline.WriteTo] saved of it, but for the exceptions
// [Timeline] lists. The frames then run through p's effects into every frame
// output; effects that drop every frame are an error.
//
// Until the first frame, a terminal that looks like nothing was drawn on it
// is not a frame, which skips the capability queries programs start with. A
// recording whose window has no frame at all shows the terminal as it is at
// the end.
//
// Each output's path may hold variables, written {name}: {duration}, the
// length of the timeline's window rounded to 100ms (such as "4.2s"),
// {timestamp}, when the window started in UTC as 20060102T150405 (or now,
// when the timeline has no start time), {mark} in an output with Marks, and
// every name in [Profile.Vars]. Values are used as they are. Any other name,
// and two outputs with one path, are errors. Every output is checked, and
// every frame output created, before any frame is drawn, so a video without
// ffmpeg fails at once. Each output is written to a temporary file, moved
// into place once complete, so an output that fails leaves an existing file
// alone.
//
// An asciicast output is the timeline itself, written with
// [Timeline.WriteCast], with the [Profile.Title] and the palette the frames
// are drawn with in its header. When every output is asciicast, nothing is
// drawn and no renderer is built. It does not run through the effects, so
// a profile with a [Redactor] and an asciicast output is an error wrapping
// [ErrUnredacted]. A still of [File.Marks] runs through the Redactors
// alone (see [Redactor]).
//
// Canceling ctx stops the export, which then writes nothing.
func Export(ctx context.Context, tl *Timeline, p Profile) error {
	tl = tl.fixed(tl.header, -1)
	from, to := tl.Window()
	start := tl.started()
	if !start.IsZero() {
		start = start.Add(from)
	}
	vars, err := pathVars(p.Vars, start)
	if err != nil {
		return err
	}
	vars[varDuration] = (to - from).Round(100 * time.Millisecond).String()
	rec, err := newRecorder(ctx, replayRuntime, tl, false, p, vars)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			rec.abort()
		}
	}()
	err = rec.replayAll(ctx, tl, to)
	if err != nil {
		return err
	}
	done = true
	return rec.commit(tl)
}

// recorder writes a profile's outputs from the events of a timeline it is
// given one at a time. Once created, it must be committed or aborted.
type recorder struct {
	s       settings
	outs    *outputs
	castHdr Header
	// rast draws the frames, and closeRast closes it when the recorder
	// built it.
	rast      Rasterizer
	closeRast bool
	replay    *replay
	pipe      *pipeline
	// effects is the context the effects were started with, closed with
	// the recorder.
	effects *Context
	// stills redacts the stills, when there are both stills and
	// [Redactor] values.
	stills *stillRedaction
}

// stillRedaction runs the stills of a recording through its [Redactor]
// values, started for the stills alone.
type stillRedaction struct {
	// ctx is the context the Redactors were started with, and stages are
	// their stages.
	ctx    *Context
	stages []Stage
	// span is the timeline time a still plays for, a step of playback,
	// which the Redactors may look at past its offset.
	span time.Duration
	// held are the stills drawn, in order, held until the timeline is
	// known past their span.
	held []heldStill
}

// heldStill is a still drawn, and the sinks to write it to.
type heldStill struct {
	frame Frame
	label string
	sinks []sink.Sink
}

// newRecorder checks p and opens its outputs for tl, with a replay terminal
// from rt. live is set when tl grows while the recorder runs. It draws
// nothing when every output is asciicast.
func newRecorder(ctx context.Context, rt *vt.Runtime, tl *Timeline, live bool, p Profile, vars map[string]string) (*recorder, error) {
	s, err := p.settings()
	if err != nil {
		return nil, err
	}
	outs, err := planOutputs(p, vars)
	if err != nil {
		return nil, err
	}
	err = outs.checkRedaction(p.Effects)
	if err != nil {
		return nil, err
	}
	r := &recorder{s: s, outs: outs}
	opts := renderOptions(tl.header, p.Render)
	title := cmp.Or(p.Title, tl.header.Title)
	if len(outs.casts) > 0 {
		r.castHdr = tl.header
		r.castHdr.Title = title
		if p.Rasterizer == nil {
			// The palette the frames are drawn with. A custom rasterizer
			// keeps the timeline's.
			if r.castHdr.Palette, err = drawnPalette(opts); err != nil {
				return nil, err
			}
		}
	}
	if len(outs.files) == 0 {
		return r, nil
	}
	if err = r.openDrawn(ctx, rt, tl, live, p, opts, title); err != nil {
		r.abort()
		return nil, err
	}
	return r, nil
}

// openDrawn builds the rasterizer, opens the frame outputs, and starts the
// replay and the pipeline.
func (r *recorder) openDrawn(ctx context.Context, rt *vt.Runtime, tl *Timeline, live bool, p Profile, opts []render.Option, title string) error {
	r.rast = p.Rasterizer
	if r.rast == nil {
		rend, err := render.New(opts...)
		if err != nil {
			return err
		}
		r.rast, r.closeRast = rend, true
	}
	if err := r.outs.open(); err != nil {
		return err
	}
	from, _ := tl.Window()
	replay, err := newReplay(ctx, rt, tl.header, max(tl.from, 0), from)
	if err != nil {
		return err
	}
	r.replay = replay
	newContext := func() *Context {
		return &Context{
			Timeline:   tl,
			Live:       live,
			Rasterizer: r.rast,
			Layout:     r.rast.Layout(tl.header.Cols, tl.header.Rows),
			Render:     opts,
			Step:       r.s.step,
			Speed:      r.s.speed,
			Hold:       r.s.hold,
			Title:      title,
			Meta:       p.Meta,
		}
	}
	if r.outs.hasStills() {
		r.replay.mark = r.still
		if red := redactors(p.Effects); len(red) > 0 {
			// A still plays for no longer than its step.
			ctx := newContext()
			ctx.Hold = 0
			r.stills = &stillRedaction{ctx: ctx, span: time.Duration(float64(r.s.step) * r.s.speed)}
			if r.stills.stages, err = startEffects(red, ctx); err != nil {
				return err
			}
		}
	}
	if !r.outs.hasRecording() {
		return nil
	}
	r.effects = newContext()
	stages, err := startEffects(p.Effects, r.effects)
	if err != nil {
		return err
	}
	r.pipe = newPipeline(r.rast, r.s, stages, r.outs.sink)
	r.replay.frame = r.pipe.record
	return nil
}

// withoutNoMark returns err, or the errors it joins, without those that
// wrap [ErrNoMark].
func withoutNoMark(err error) error {
	errs := []error{err}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		errs = j.Unwrap()
	}
	return errors.Join(slices.DeleteFunc(slices.Clone(errs), func(e error) bool { return errors.Is(e, ErrNoMark) })...)
}

// startEffects starts each effect for a recording, leaving out those that
// return no stage.
func startEffects(effects []Effect, ctx *Context) ([]Stage, error) {
	var stages []Stage
	for _, e := range effects {
		st, err := e.Start(ctx)
		if err != nil {
			return nil, err
		}
		if st != nil {
			stages = append(stages, st)
		}
	}
	return stages, nil
}

// renderOptions returns the options the frames of a timeline that starts
// with h are drawn with: its palette, replaced by one opts sets.
func renderOptions(h Header, opts []render.Option) []render.Option {
	var indexed map[int]color.Color
	for i, c := range h.Palette {
		if c != nil {
			if indexed == nil {
				indexed = make(map[int]color.Color, len(h.Palette))
			}
			indexed[i] = c
		}
	}
	if indexed == nil {
		return opts
	}
	return append([]render.Option{render.WithPalette(render.Palette{Indexed: indexed})}, opts...)
}

// drawnPalette returns the 16 colors a renderer built from opts draws with,
// nil for its defaults.
func drawnPalette(opts []render.Option) ([16]color.Color, error) {
	var out [16]color.Color
	pal, err := render.PaletteOf(opts...)
	if err != nil {
		return out, err
	}
	for i := range out {
		out[i] = pal.Indexed[i]
	}
	return out, nil
}

// replayAll replays every event of tl, a fixed timeline whose window ends at
// end, after checking the stills' marks are in its window.
func (r *recorder) replayAll(ctx context.Context, tl *Timeline, end time.Duration) error {
	var marks []string
	for _, ev := range tl.events[max(tl.from, 0):] {
		if m, ok := ev.(Mark); ok {
			marks = append(marks, m.Label)
		}
	}
	if err := r.outs.missingMarks(marks); err != nil {
		return err
	}
	if r.replay != nil {
		for _, ev := range tl.events {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.apply(ev); err != nil {
				return err
			}
		}
	}
	return r.end(end)
}

func (r *recorder) apply(ev Event) error {
	if r.replay == nil {
		return nil
	}
	// The timeline is known up to ev, which a live one may not have held
	// when a still was drawn.
	if err := r.writeHeld(ev.At(), false); err != nil {
		return err
	}
	return r.replay.apply(ev)
}

// still writes the terminal as it is at m to the stills of its label not
// yet written. With Redactors, it is held until the timeline is known past
// the still's span, so the Redactors see the marks that fall in it, as
// they would for a frame of the recording.
func (r *recorder) still(m Mark) error {
	sinks := r.outs.stillsAt(m.Label)
	if len(sinks) == 0 {
		return nil
	}
	f := r.drawCurrent(m.At())
	if r.stills == nil {
		return writeStill(sinks, f, r.s.step)
	}
	r.stills.held = append(r.stills.held, heldStill{frame: f, label: m.Label, sinks: sinks})
	return nil
}

// drawCurrent returns the terminal as it is now, as of at, as a frame a
// step long.
func (r *recorder) drawCurrent(at time.Duration) Frame {
	cf := r.replay.current(at)
	b := cf.Screen.Bounds()
	img := image.NewNRGBA(r.rast.Layout(b.Dx(), b.Dy()).Image)
	r.rast.Draw(img, cf.Screen, cf.State, cf.At)
	return Frame{Image: img, Cells: &cf, At: cf.At, Delay: r.s.step}
}

// writeHeld redacts and writes the stills held whose span ends before
// offset at, or every one when all is set.
func (r *recorder) writeHeld(at time.Duration, all bool) error {
	sr := r.stills
	if sr == nil {
		return nil
	}
	for len(sr.held) > 0 && (all || sr.held[0].frame.At+sr.span < at) {
		h := sr.held[0]
		sr.held = sr.held[1:]
		f, ok := runAlone(sr.stages, h.frame)
		if !ok {
			return fmt.Errorf("reel: the redacting effects dropped the still at mark %q", h.label)
		}
		if err := writeStill(h.sinks, f, r.s.step); err != nil {
			return err
		}
	}
	return nil
}

// writeStill writes f, a still, to sinks.
func writeStill(sinks []sink.Sink, f Frame, step time.Duration) error {
	for _, s := range sinks {
		if err := s.WriteFrame(sink.Frame{Image: f.Image, Delay: step, Isolated: f.Isolated}); err != nil {
			return err
		}
	}
	return nil
}

// runAlone runs f through stages, giving each the frames the one before it
// emitted as a recording of their own, and returns the first frame the last
// emits, or false when none does.
func runAlone(stages []Stage, f Frame) (Frame, bool) {
	frames := []Frame{f}
	for _, run := range stages {
		var out []Frame
		for i, in := range frames {
			in.First, in.Last = i == 0, i == len(frames)-1
			run(in, func(e Frame) {
				if e.Image != nil {
					out = append(out, e)
				}
			})
		}
		frames = out
	}
	if len(frames) == 0 {
		return Frame{}, false
	}
	return frames[0], true
}

// end ends the recording at offset end, sending every frame on.
func (r *recorder) end(end time.Duration) error {
	if r.replay == nil {
		return nil
	}
	if err := r.replay.finish(); err != nil {
		return err
	}
	if err := r.writeHeld(0, true); err != nil {
		return err
	}
	if r.stills != nil && r.stills.ctx.Live && r.pipe == nil {
		// The Redactors have seen a live timeline up to the last still.
		// Without a recording to show them the rest, a frame of the end,
		// which is dropped, does, so they report the marks that never
		// came, and only those.
		runAlone(r.stills.stages, r.drawCurrent(end))
	}
	if r.pipe == nil {
		return nil
	}
	if err := r.pipe.finish(end); err != nil {
		return err
	}
	if r.pipe.written == 0 {
		return errors.New("reel: the effects dropped every frame")
	}
	return nil
}

// commit writes every output, with tl, under the recording's header, to the
// asciicast ones.
func (r *recorder) commit(tl *Timeline) error {
	// The terminal goes back first: writing a cut replays it.
	err := errors.Join(r.outs.close(), r.release())
	return errors.Join(err, r.outs.writeCasts(tl, r.castHdr))
}

func (r *recorder) abort() {
	r.outs.discard()
	_ = r.release()
}

// release returns the terminal, closes what the effects opened, and closes
// the rasterizer the recorder built.
func (r *recorder) release() error {
	var errs []error
	if r.stills != nil {
		err := r.stills.ctx.close()
		if r.effects != nil {
			// The recording's effects hold the same Redactors, which
			// report the same marks that never came.
			err = withoutNoMark(err)
		}
		errs = append(errs, err)
		r.stills = nil
	}
	if r.effects != nil {
		errs = append(errs, r.effects.close())
		r.effects = nil
	}
	if r.replay != nil {
		errs = append(errs, r.replay.close())
		r.replay = nil
	}
	if r.closeRast {
		errs = append(errs, r.rast.Close())
		r.closeRast = false
	}
	return errors.Join(errs...)
}
