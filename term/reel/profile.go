// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"fmt"
	"image"
	"maps"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
)

// Profile configures how [Export] and [Record] turn a [Timeline] into files.
// The zero value of every field but Outputs is a usable default. A profile
// holds no state of its own, so one can be shared by recordings that run in
// parallel.
type Profile struct {
	// Outputs are the files to write, which must not be empty.
	Outputs []File
	// Render configures the renderer the frames are drawn with. A palette
	// set with [render.WithPalette] replaces the timeline's
	// [Header.Palette].
	Render []render.Option
	// Fit, when set, fits the frames within this many pixels wide (X) and
	// tall (Y), by choosing the render scale, so text stays sharp rather than
	// the image being resized. It is the largest scale at which the image of
	// the timeline's [Header] size fits (see [render.FitScale]), so one
	// direction may be smaller, and [render.WithScale] in Render is an error.
	// An X or Y of 0 sets no limit in that direction. A [Resize] to a larger
	// terminal still grows the frames. It is an error with a Rasterizer,
	// which draws its own size.
	Fit image.Point
	// Rasterizer, when set, draws the frames instead of a renderer built
	// from Render. It is not closed, and is drawn with from one goroutine
	// per recording.
	Rasterizer Rasterizer
	// Encode configures every frame output (see the sink package).
	Encode []sink.Option
	// Effects change the frames between the rasterizer and the frame
	// outputs, in order. A [Concealer] must come before every effect that
	// is not one. Asciicast outputs record those that describe themselves
	// (see [Describer]).
	Effects []Effect
	// FPS sets the delay grid: every frame plays for a whole number of
	// steps of 10ms × max(1, 100/FPS), the delays GIF can store. Zero is
	// 20, and it must be at most [sink.MaxFrameRate]. Frames are not drawn
	// at a fixed rate: each change the terminal shows is a frame, which
	// ends on the step nearest its end in the timeline, so a recording
	// plays as long as its timeline from the first frame, to within a step.
	// Of changes that fall on one step, only the newest is shown.
	FPS int
	// Speed scales playback: 2 plays twice as fast. Zero is 1.
	Speed float64
	// Title names the recording, for effects such as a title card, and for
	// asciicast outputs. Without it, the timeline's [Header.Title] is used.
	Title string
	// Meta is anything a caller's effects need, passed along in
	// [Context.Meta]. reel never reads it.
	Meta any
	// Vars are extra path variables (see [Export]), by name without braces.
	Vars map[string]string
}

// File is one output a [Profile] writes. It is not called Output, which is
// the event of what a program wrote.
type File struct {
	// Path is where the file is written, which may hold variables (see
	// [Export]). Its extension picks the format: ".cast" for an asciicast
	// file of the timeline, with the profile's effects as annotations (see
	// Export), and otherwise one [sink.File] writes, such as ".gif" or
	// ".mp4". Missing directories are created.
	Path string
	// Options are added to [Profile.Encode] for this output.
	Options []sink.Option
	// Marks, when set, makes the output a still of the terminal at each of
	// these [Mark] labels instead of a recording, one file per label, whose
	// Path must hold {mark}. A label that repeats is shown at its first mark
	// in the timeline's window, and one that is not there is an error
	// wrapping [ErrNoMark]. Stills run through the [Concealer] and
	// [StillEffect] effects, and no other.
	Marks []string
	// Cast is the asciicast version a ".cast" Path is written as. Zero is
	// [CastV2]. It is an error on other paths.
	Cast CastVersion
}

// Files returns a [File] for each path.
func Files(paths ...string) []File {
	out := make([]File, len(paths))
	for i, p := range paths {
		out[i].Path = p
	}
	return out
}

// Merge returns p with every field that o sets, such as defaults overridden
// by a test's own profile. A field of o that is not its zero value replaces
// p's, slices included: a set slice replaces p's rather than adding to it.
// Vars merge by name, with o's value winning.
func (p Profile) Merge(o Profile) Profile {
	if o.Outputs != nil {
		p.Outputs = o.Outputs
	}
	if o.Render != nil {
		p.Render = o.Render
	}
	if o.Fit != (image.Point{}) {
		p.Fit = o.Fit
	}
	if o.Rasterizer != nil {
		p.Rasterizer = o.Rasterizer
	}
	if o.Encode != nil {
		p.Encode = o.Encode
	}
	if o.Effects != nil {
		p.Effects = o.Effects
	}
	if o.FPS != 0 {
		p.FPS = o.FPS
	}
	if o.Speed != 0 {
		p.Speed = o.Speed
	}
	if o.Title != "" {
		p.Title = o.Title
	}
	if o.Meta != nil {
		p.Meta = o.Meta
	}
	if o.Vars != nil {
		vars := maps.Clone(p.Vars)
		if vars == nil {
			vars = make(map[string]string, len(o.Vars))
		}
		maps.Copy(vars, o.Vars)
		p.Vars = vars
	}
	return p
}

const defaultFPS = 20

// settings are a profile's timing, with its defaults applied.
type settings struct {
	step  time.Duration
	speed float64
}

// settings checks p's timing, and returns it with defaults applied.
func (p Profile) settings() (settings, error) {
	var s settings
	fps := p.FPS
	if fps == 0 {
		fps = defaultFPS
	}
	if fps < 1 || fps > sink.MaxFrameRate {
		return s, fmt.Errorf("reel: FPS %d out of range [1, %d]", p.FPS, sink.MaxFrameRate)
	}
	s.step = time.Duration(max(1, 100/fps)) * 10 * time.Millisecond
	s.speed = p.Speed
	if s.speed == 0 {
		s.speed = 1
	}
	if !(s.speed > 0) || math.IsInf(s.speed, 0) { // Also catches NaN.
		return s, fmt.Errorf("reel: speed %v must be positive and finite", p.Speed)
	}
	return s, nil
}
