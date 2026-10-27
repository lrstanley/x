// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"context"
	"errors"
	"image"
	"image/color"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt"
)

// Export writes the outputs of p from tl, which is replayed into a fresh
// terminal: every event is applied in order, and each time the terminal
// looks different (outside synchronized output) is a frame: new program
// output, or a change of focus or colors.
// The frames of a timeline returned by [Timeline.Between] are those of its
// window, which opens on the terminal the events before it left. Frames are
// drawn as of their offset into the timeline, never the wall clock, so
// exporting one timeline twice writes the same bytes. The frames then run
// through p's effects into every frame output; effects that drop every frame
// are an error.
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
// drawn and no renderer is built.
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
	rec, err := newRecorder(ctx, replayRuntime, tl, p, vars)
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
}

// newRecorder checks p and opens its outputs for tl, with a replay terminal
// from rt. It draws nothing when every output is asciicast.
func newRecorder(ctx context.Context, rt *vt.Runtime, tl *Timeline, p Profile, vars map[string]string) (*recorder, error) {
	s, err := p.settings()
	if err != nil {
		return nil, err
	}
	outs, err := planOutputs(p, vars)
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
	if err = r.openDrawn(ctx, rt, tl, p, opts, title); err != nil {
		r.abort()
		return nil, err
	}
	return r, nil
}

// openDrawn builds the rasterizer, opens the frame outputs, and starts the
// replay and the pipeline.
func (r *recorder) openDrawn(ctx context.Context, rt *vt.Runtime, tl *Timeline, p Profile, opts []render.Option, title string) error {
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
	if r.outs.hasStills() {
		r.replay.mark = r.still
	}
	if !r.outs.hasRecording() {
		return nil
	}
	stages, err := startEffects(p.Effects, &Context{
		Timeline:   tl,
		Rasterizer: r.rast,
		Layout:     r.rast.Layout(tl.header.Cols, tl.header.Rows),
		Render:     opts,
		Step:       r.s.step,
		Speed:      r.s.speed,
		Title:      title,
		Meta:       p.Meta,
	})
	if err != nil {
		return err
	}
	r.pipe = newPipeline(r.rast, r.s, stages, r.outs.sink)
	r.replay.frame = r.pipe.record
	return nil
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
	return r.replay.apply(ev)
}

// still writes the terminal as it is at m to the stills of its label not
// yet written.
func (r *recorder) still(m Mark) error {
	sinks := r.outs.stillsAt(m.Label)
	if len(sinks) == 0 {
		return nil
	}
	cf := r.replay.current(m.At())
	b := cf.Screen.Bounds()
	img := image.NewNRGBA(r.rast.Layout(b.Dx(), b.Dy()).Image)
	r.rast.Draw(img, cf.Screen, cf.State, cf.At)
	for _, s := range sinks {
		if err := s.WriteFrame(sink.Frame{Image: img, Delay: r.s.step}); err != nil {
			return err
		}
	}
	return nil
}

// end ends the recording at offset end, sending every frame on.
func (r *recorder) end(end time.Duration) error {
	if r.replay == nil {
		return nil
	}
	if err := r.replay.finish(); err != nil {
		return err
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

// release returns the terminal and closes the rasterizer the recorder built.
func (r *recorder) release() error {
	var errs []error
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
