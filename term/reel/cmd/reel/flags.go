// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/look"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/spec"
	"github.com/lrstanley/x/term/reel/effects"
)

// OutputFlags are the files a command writes.
type OutputFlags struct {
	Outputs []string          `short:"o" name:"output" placeholder:"FILE" help:"File to write, repeatable; the extension picks the format (.gif, .mp4, .webm, .webp, .png with ?marks=, .cast, .snap, .json). Takes query options, such as out.gif?max-bytes=5MB&loop=once, and {variables}, such as {duration}."`
	Vars    map[string]string `name:"var" placeholder:"NAME=VALUE" help:"Extra {NAME} path variable, repeatable."`
}

func (o OutputFlags) files() ([]reel.File, error) {
	files := make([]reel.File, 0, len(o.Outputs))
	for _, raw := range o.Outputs {
		f, err := reel.ParseFile(raw)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// EffectFlags are the effects a command's recordings get, beyond those of
// [look.Look].
type EffectFlags struct {
	Intro        time.Duration `name:"intro" placeholder:"DUR" help:"Open on a title card for this long."`
	IntroLines   []string      `name:"intro-line" placeholder:"TEXT" help:"Line of the intro card, repeatable; defaults to the title."`
	Outro        time.Duration `name:"outro" placeholder:"DUR" help:"End on a card for this long."`
	OutroLines   []string      `name:"outro-line" placeholder:"TEXT" help:"Line of the outro card, repeatable."`
	Pause        bool          `name:"pause" help:"Pause for a second at the end, with a badge."`
	PauseAt      []string      `name:"pause-at" placeholder:"MARK" help:"Pause for a second at a mark, repeatable."`
	HoldLast     time.Duration `name:"hold-last" placeholder:"DUR" help:"Hold the last frame for this long."`
	Mouse        bool          `name:"mouse" help:"Draw the mouse pointer, clicks, and drags."`
	Hide         []string      `name:"hide" placeholder:"FROM,TO" help:"Cut the window between two marks, repeatable; either may be empty for the start or end."`
	Highlight    []string      `name:"highlight" placeholder:"SPEC" help:"Highlight a region, repeatable, such as 'rows=2-4;dim=0.4;between=a,b'. ${spec}"`
	Zoom         []string      `name:"zoom" placeholder:"SPEC" help:"Zoom into a region, repeatable, such as 're=ERROR.*;scale=2'; see --highlight for SPEC."`
	Redact       []string      `name:"redact" placeholder:"SPEC" help:"Redact a region, repeatable, such as 'env=TOKEN;style=blur'; see --highlight for SPEC."`
	RedactEnv    []string      `name:"redact-env" placeholder:"NAME" help:"Redact the value of an environment variable wherever it shows, repeatable."`
	RedactRegexp []string      `name:"redact-regexp" placeholder:"PATTERN" help:"Redact the matches of a regular expression, repeatable."`
}

func (e EffectFlags) effects() ([]reel.Effect, error) {
	var fx []reel.Effect
	if e.Intro > 0 {
		fx = append(fx, effects.Intro(e.Intro, lines(e.IntroLines)...))
	}
	if e.Outro > 0 {
		fx = append(fx, effects.Outro(e.Outro, lines(e.OutroLines)...))
	}
	if e.Pause || len(e.PauseAt) > 0 {
		var opts []effects.PauseOption
		if e.Pause {
			opts = append(opts, effects.PauseAtEnd(time.Second))
		}
		if len(e.PauseAt) > 0 {
			opts = append(opts, effects.PauseAtMarks(time.Second, e.PauseAt...))
		}
		fx = append(fx, effects.Pause(opts...))
	}
	if e.HoldLast > 0 {
		fx = append(fx, effects.HoldLast(e.HoldLast))
	}
	if e.Mouse {
		fx = append(fx, effects.Mouse())
	}
	if len(e.Hide) > 0 {
		rules := make([]effects.HideRule, 0, len(e.Hide))
		for _, h := range e.Hide {
			from, to, ok := strings.Cut(h, ",")
			if !ok {
				return nil, fmt.Errorf("--hide %q is not FROM,TO", h)
			}
			rules = append(rules, effects.HideBetween(from, to))
		}
		fx = append(fx, effects.Hide(rules...))
	}
	for _, kind := range []struct {
		name  string
		specs []string
	}{{"highlight", e.Highlight}, {"zoom", e.Zoom}, {"redact", e.Redact}} {
		for _, s := range kind.specs {
			effect, err := spec.Effect(kind.name, s)
			if err != nil {
				return nil, fmt.Errorf("--%s: %w", kind.name, err)
			}
			fx = append(fx, effect)
		}
	}
	for _, name := range e.RedactEnv {
		if os.Getenv(name) == "" {
			warnf("--redact-env %s: the variable is empty or unset, so there is nothing to redact", name)
		}
		fx = append(fx, effects.Redact(effects.Env(name)))
	}
	for _, pattern := range e.RedactRegexp {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("--redact-regexp: %w", err)
		}
		fx = append(fx, effects.Redact(effects.Regexp(re)))
	}
	return fx, nil
}

func lines(texts []string) []effects.Line {
	out := make([]effects.Line, len(texts))
	for i, t := range texts {
		out[i] = effects.Line{Text: t}
	}
	return out
}

// RenderFlags are the flags of commands that render recordings of a
// timeline they did not size: how it looks, its effects, and its outputs.
type RenderFlags struct {
	look.Look   `embed:""`
	EffectFlags `embed:""`
	OutputFlags `embed:""`
}

// profile returns the profile the flags describe, with base effects first,
// such as those an asciicast file holds.
func (f RenderFlags) profile(base ...reel.Effect) (reel.Profile, error) {
	if len(f.Outputs) == 0 {
		return reel.Profile{}, errors.New("no outputs; add one with -o, such as -o demo.gif")
	}
	if err := f.Validate(); err != nil {
		return reel.Profile{}, err
	}
	var fontSize float64
	if f.FontSize != nil {
		fontSize = *f.FontSize
	}
	opts, warnings, err := f.Render(fontSize)
	if err != nil {
		return reel.Profile{}, err
	}
	for _, w := range warnings {
		warnf("%s", w)
	}
	outputs, err := f.files()
	if err != nil {
		return reel.Profile{}, err
	}
	fx, err := f.effects()
	if err != nil {
		return reel.Profile{}, err
	}
	p := reel.Profile{Outputs: outputs, Render: opts, Vars: f.Vars}
	f.Profile(&p)
	p.Effects = look.Arrange(slices.Concat(base, fx, f.Effects()))
	return p, nil
}
