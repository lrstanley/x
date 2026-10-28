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
// Every field but Outputs has a usable zero value. A profile holds no state,
// so it can be shared by parallel recordings.
type Profile struct {
	// Outputs are the files to write; it must not be empty.
	Outputs []File
	// Render configures the renderer. A [render.WithPalette] palette
	// replaces the timeline's [Header.Palette].
	Render []render.Option
	// Fit, if set, fits frames within X by Y pixels by choosing the render
	// scale (see [render.FitScale]), so text stays sharp rather than the
	// image being resized; one direction may come out smaller. An X or Y of 0
	// sets no limit in that direction. A [Resize] to a larger terminal still
	// grows the frames. [render.WithScale] in Render, or a Rasterizer, is an
	// error.
	Fit image.Point
	// Rasterizer, if set, draws frames instead of a renderer built from
	// Render. It is not closed, and each recording uses it from one
	// goroutine.
	Rasterizer Rasterizer
	// Encode configures every frame output (see the sink package).
	Encode []sink.Option
	// Effects transform frames in order. See [Concealer] for ordering rules
	// and [Describer] for how asciicast outputs record them.
	Effects []Effect
	// FPS sets the delay grid: frames play whole steps of
	// 10ms × max(1, 100/FPS), the delays GIF can store. Zero means 20; it
	// must be at most [sink.MaxFrameRate]. Frames are not drawn at a fixed
	// rate: each visible change is a frame ending on the step nearest its
	// timeline end, and of changes on one step only the newest is shown.
	FPS int
	// Speed scales playback: 2 plays twice as fast. Zero means 1.
	Speed float64
	// Title names the recording, for effects such as a title card and for
	// asciicast outputs. Defaults to the timeline's [Header.Title].
	Title string
	// Meta is passed to effects in [Context.Meta]; reel never reads it.
	Meta any
	// Vars are extra path variables (see [Export]), by name without braces.
	Vars map[string]string
}

// File is one output a [Profile] writes.
//
// An asciicast (".cast") output is the timeline itself, written with
// [Timeline.WriteCast], with [Profile.Title] and the drawn palette in its
// header. It bypasses the effects, so it cannot be combined with a
// [Concealer] or [Cutter]. Instead, it records each [Describer] effect's description,
// in profile order, as an [Annotation] at the start of the file. These
// replace the timeline's existing annotations of [EffectKind] and of the
// described kinds, so re-exporting a file with its own effects does not
// duplicate them. Effects that are not Describers, or whose Describe
// returns an error wrapping [errors.ErrUnsupported], are left out. Before
// anything is written, it is an error for Describe to fail otherwise or
// for a description to fail [AnnotationSchema]; see [MarkChecker] for
// effects that name marks.
type File struct {
	// Path is where the file is written; it may contain variables (see
	// [Export]). The extension picks the format: ".cast" for asciicast,
	// otherwise any format [sink.File] supports, such as ".gif" or ".mp4".
	// Missing directories are created.
	Path string
	// Options are added to [Profile.Encode] for this output.
	Options []sink.Option
	// Marks, if set, makes the output one still per [Mark] label instead of
	// a recording; Path must contain {mark}. A repeated label uses its first
	// mark in the window. A missing one writes nothing, and is reported as a
	// [NoMarkError] once the other outputs are written. Stills run through
	// only [Concealer] and [StillEffect] effects.
	Marks []string
	// Cast is the asciicast version for a ".cast" Path; zero means
	// [CastV2]. Setting it on other paths is an error.
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

// Merge returns p with every non-zero field of o replacing p's. Slices are
// replaced, not appended to. Vars merge by name, with o winning.
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

// settings are a profile's timing with defaults applied.
type settings struct {
	step  time.Duration
	speed float64
}

// settings validates p's timing and applies defaults.
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
