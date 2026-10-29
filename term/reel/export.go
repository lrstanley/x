// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
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

// Export replays tl into a fresh terminal and writes the outputs of p. Each
// visible change (outside synchronized output) is a frame: program output,
// or a change of focus or colors. Frames run through p's effects into every
// frame output; effects that drop every frame are an error.
//
// For a timeline from [Timeline.Between], frames are those of its window,
// opening on the screen the earlier events left. Output is deterministic
// (see [Timeline]). [Header.SkipPristine] skips the bare terminal before
// the first paint. A window with no frames at all shows the terminal as it
// is at the end.
//
// Output paths may contain {name} variables: those of
// [Timeline.PathVars], {mark} for outputs with Marks or MarkMatch, and any name in
// [Profile.Vars]. Unknown names and
// duplicate paths are errors. All outputs are checked and created before
// any frame is drawn, so a missing ffmpeg fails at once. Each output is
// written to a temporary file and moved into place when complete, so a
// failure leaves an existing file untouched.
//
// A mark the profile looks for that the timeline's window does not have,
// such as one a test failed before it reached, does not stop the export:
// the outputs are written as a [Record] of the timeline would write them,
// whose marks never came (see Record), and Export then returns a
// [NoMarkError] for each. See [File] for what asciicast, ".snap", and
// ".json" outputs hold; when every output is one of those, nothing is
// drawn.
//
// Canceling ctx stops the export, which then writes nothing.
func Export(ctx context.Context, tl *Timeline, p Profile) error {
	tl = tl.fixed(tl.header, -1)
	_, to := tl.Window()
	vars, err := tl.PathVars(p.Vars)
	if err != nil {
		return err
	}
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

// recorder writes a profile's outputs from timeline events fed to it one at
// a time. Once created, it must be committed or aborted.
type recorder struct {
	s           settings
	outs        *outputs
	castHdr     Header
	castEffects []castEffect
	rast        Rasterizer
	// closeRast is set when the recorder built rast and must close it.
	closeRast bool
	replay    *replay
	pipe      *pipeline
	// view is what a live recording's effects see: the events of src
	// replayed so far, so they never see events appended after it stops.
	src, view *Timeline
	effects   *Context
	// stills is set when there are both stills and still-capable effects.
	stills *stillChain
}

// stillChain runs stills through the effects that apply to them, started
// separately from the recording's.
type stillChain struct {
	ctx    *Context
	stages []Stage
	// span is how long a still plays for (one playback step); effects may
	// look this far past its offset.
	span time.Duration
	// held are drawn stills waiting until the timeline is known past their
	// span.
	held []heldStill
}

// heldStill is a drawn still and the sinks to write it to.
type heldStill struct {
	frame Frame
	label string
	sinks []sink.Sink
}

// newRecorder checks p and opens its outputs for tl. live is set when tl
// grows while the recorder runs.
func newRecorder(ctx context.Context, rt *vt.Runtime, tl *Timeline, live bool, p Profile, vars map[string]string) (*recorder, error) {
	s, err := p.settings()
	if err != nil {
		return nil, err
	}
	var matchIn *Timeline
	if !live {
		matchIn = tl
	}
	outs, err := planOutputs(p, vars, matchIn)
	if err != nil {
		return nil, err
	}
	err = outs.checkEffects(p.Effects)
	if err != nil {
		return nil, err
	}
	r := &recorder{s: s, outs: outs}
	opts := renderOptions(tl.header, p.Render)
	title := cmp.Or(p.Title, tl.header.Title)
	if len(outs.casts) > 0 {
		r.castHdr = tl.header
		r.castHdr.Title = title
		if r.castEffects, err = describeEffects(p.Effects); err != nil {
			return nil, err
		}
		if p.Rasterizer == nil {
			// Record the drawn palette; a custom rasterizer keeps the
			// timeline's.
			if r.castHdr.Palette, err = drawnPalette(opts); err != nil {
				return nil, err
			}
		}
	}
	if len(outs.files) == 0 {
		return r, nil
	}
	if err = r.open(ctx, rt, tl, live, p, opts, title); err != nil {
		r.abort()
		return nil, err
	}
	return r, nil
}

// open starts the replay the file outputs are written from. For drawn
// outputs, it also builds the rasterizer, opens them, and starts the
// effects and pipeline.
func (r *recorder) open(ctx context.Context, rt *vt.Runtime, tl *Timeline, live bool, p Profile, opts []render.Option, title string) error {
	var err error
	if r.outs.hasDrawn() {
		if opts, err = p.fitRender(tl.header, opts); err != nil {
			return err
		}
		r.rast = p.Rasterizer
		if r.rast == nil {
			var rend *render.Renderer
			if rend, err = render.New(opts...); err != nil {
				return err
			}
			r.rast, r.closeRast = rend, true
		}
		err = r.outs.open()
		if err != nil {
			return err
		}
	}
	from, _ := tl.Window()
	if r.replay, err = newReplay(ctx, rt, tl.header, max(tl.from, 0), from); err != nil {
		return err
	}
	if r.outs.hasStills(false) {
		r.replay.mark = r.still
	}
	if r.rast == nil {
		return nil
	}
	if live {
		r.src, r.view = tl, newTimeline(tl.header)
		tl = r.view
	}
	newContext := func() *Context {
		return &Context{
			Timeline:   tl,
			Live:       live,
			Rasterizer: r.rast,
			Layout:     r.rast.Layout(tl.header.Cols, tl.header.Rows),
			Render:     opts,
			Step:       r.s.step,
			Speed:      r.s.speed,
			Title:      title,
			Meta:       p.Meta,
		}
	}
	if r.outs.hasStills(true) {
		if fx := stillEffects(p.Effects); len(fx) > 0 {
			ctx := newContext()
			r.stills = &stillChain{ctx: ctx, span: time.Duration(float64(r.s.step) * r.s.speed)}
			if r.stills.stages, err = startEffects(fx, ctx); err != nil {
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

// castEffect is an effect recorded in asciicast outputs, with its
// description.
type castEffect struct {
	effect Effect
	desc   Annotation
}

// describeEffects returns the validated descriptions of each [Describer]
// effect, skipping those that return [errors.ErrUnsupported].
func describeEffects(effects []Effect) ([]castEffect, error) {
	var out []castEffect
	for i, e := range effects {
		d, ok := e.(Describer)
		if !ok {
			continue
		}
		a, err := d.Describe()
		if err == nil {
			err = a.validate()
		}
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			continue
		case err != nil:
			return nil, fmt.Errorf("reel: effect %d (%T): %w", i, e, err)
		}
		out = append(out, castEffect{effect: e, desc: a})
	}
	return out, nil
}

// startEffects starts each effect, leaving out those that return no stage.
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

// renderOptions returns opts preceded by h's palette, so opts can override
// it.
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

// fitRender returns opts with the scale that fits a screen of h's size within
// p.Fit, or opts when it is not set.
func (p Profile) fitRender(h Header, opts []render.Option) ([]render.Option, error) {
	if p.Fit == (image.Point{}) {
		return opts, nil
	}
	if p.Rasterizer != nil {
		return nil, errors.New("reel: Fit cannot be set with a Rasterizer")
	}
	scale, err := render.FitScale(h.Cols, h.Rows, p.Fit.X, p.Fit.Y, opts...)
	if err != nil {
		return nil, fmt.Errorf("reel: Fit %v: %w", p.Fit, err)
	}
	return append(slices.Clip(opts), render.WithScale(scale)), nil
}

// drawnPalette returns the 16 colors a renderer built from opts draws with,
// nil for defaults.
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
// end.
func (r *recorder) replayAll(ctx context.Context, tl *Timeline, end time.Duration) error {
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
	if r.view != nil {
		r.view.follow(r.src, ev)
	}
	// The timeline is now known up to ev, so held stills before it can be
	// finished.
	if err := r.writeHeld(ev.At(), false); err != nil {
		return err
	}
	return r.replay.apply(ev)
}

// still writes the terminal at m to the unwritten stills for its label.
// With still effects, a drawn still is held until the timeline is known
// past the still's span, so effects see the marks in it as they would for
// a frame.
func (r *recorder) still(m Mark) error {
	stills := r.outs.take(func(f *fileOutput) bool { return f.still && f.mark == m.Label })
	if err := r.captureScreens(stills); err != nil {
		return err
	}
	var sinks []sink.Sink
	for _, f := range stills {
		if f.sink != nil {
			sinks = append(sinks, f.sink)
		}
	}
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

// drawCurrent draws the current terminal as a one-step frame at at.
func (r *recorder) drawCurrent(at time.Duration) Frame {
	cf := r.replay.current(at)
	b := cf.Screen.Bounds()
	img := image.NewNRGBA(r.rast.Layout(b.Dx(), b.Dy()).Image)
	r.rast.Draw(img, cf.Screen, cf.State, cf.At)
	return Frame{Image: img, Cells: &cf, At: cf.At, Delay: r.s.step}
}

// writeHeld runs effects on and writes the held stills whose span ends
// before at, or all of them when all is set.
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
			return fmt.Errorf("reel: the still effects dropped the still at mark %q", h.label)
		}
		if err := writeStill(h.sinks, f, r.s.step); err != nil {
			return err
		}
	}
	return nil
}

// writeStill writes the still f to sinks.
func writeStill(sinks []sink.Sink, f Frame, step time.Duration) error {
	for _, s := range sinks {
		if err := s.WriteFrame(sink.Frame{Image: f.Image, Delay: step, Isolated: f.Isolated}); err != nil {
			return err
		}
	}
	return nil
}

// runAlone runs f through stages, each seeing the previous stage's output
// as a whole recording, and returns the first frame the last stage emits,
// or false if none.
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

// captureScreens sets the data of the screen outputs among files from the
// replay's terminal.
func (r *recorder) captureScreens(files []*fileOutput) error {
	var text, screen []byte
	for _, f := range files {
		if f.format == formatSnap {
			if text == nil {
				text = []byte(r.replay.term.Text())
			}
			f.data = text
		}
		if f.format != formatJSON {
			continue
		}
		if screen == nil {
			data, err := json.Marshal(screenOf(r.replay.term, r.replay.focused), jsontext.Multiline(true))
			if err != nil {
				return fmt.Errorf("reel: output %q: %w", f.path, err)
			}
			data = append(data, '\n')
			screen = data
		}
		f.data = screen
	}
	return nil
}

// end flushes the recording at offset end.
func (r *recorder) end(end time.Duration) error {
	if r.replay == nil {
		return nil
	}
	if err := r.replay.animate(end, true); err != nil {
		return err
	}
	if err := r.replay.finish(); err != nil {
		return err
	}
	if err := r.captureScreens(r.outs.take(func(f *fileOutput) bool { return !f.still && f.isScreen() })); err != nil {
		return err
	}
	if err := r.writeHeld(0, true); err != nil {
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

// commit finishes every output, writing tl to the asciicast ones.
//
// Effects whose marks tl lacks are left out of the asciicast outputs. When
// no effects ran on frames to report those marks, commit reports them
// itself.
func (r *recorder) commit(tl *Timeline) error {
	report := r.effects == nil
	var descs []Annotation
	var missing []error
	for _, c := range r.castEffects {
		if mc, ok := c.effect.(MarkChecker); ok {
			if err := mc.CheckMarks(tl); err != nil {
				if report {
					missing = append(missing, err)
				}
				continue
			}
		}
		descs = append(descs, c.desc)
	}
	// Release the terminal first: writing a cut replays into one.
	err := errors.Join(r.outs.close(), r.release(), errors.Join(missing...))
	return errors.Join(err, r.outs.writeCasts(tl, r.castHdr, descs))
}

func (r *recorder) abort() {
	r.outs.discard()
	_ = r.release()
}

// release closes the effects, replay terminal, and owned rasterizer.
func (r *recorder) release() error {
	var errs []error
	if r.stills != nil {
		err := r.stills.ctx.close()
		if r.effects != nil {
			// The recording's effects report the same missing marks.
			_, err = MissingMarks(err)
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
