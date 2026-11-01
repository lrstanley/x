// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package spec parses the effects that work on a region of the screen,
// written as a region and options for a flag or a tape command:
//
//	REGION[;KEY=VALUE]...
//
// A REGION is one of:
//
//	row=Y            a row, from 0
//	rows=Y0-Y1       rows Y0 to Y1, inclusive
//	cells=X0,Y0,X1,Y1 a rectangle of cells, X1 and Y1 exclusive
//	text=TEXT        each occurrence of TEXT
//	re=PATTERN       each match of a regular expression
//	env=NAME         each occurrence of the value of environment variable NAME
//
// The options of every effect are between=FROM,TO, to limit it to the window
// between two marks (either may be empty, for the start or end), and the
// effect's own:
//
//	highlight  ease=DUR dim=FACTOR outline=#RRGGBB[:PX] padding=PX
//	zoom       ease=DUR scale=FACTOR anchor=X,Y
//	redact     style=blur|block|replace:CHAR
//
// Values cannot hold ";", so neither can a region's text or pattern.
package spec

import (
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/themes"
	"github.com/lrstanley/x/term/reel/effects"
)

// Kinds are the effects [Effect] parses, by name.
var Kinds = map[string]func(region effects.Region, opts map[string]string) (reel.Effect, error){
	"highlight": highlight,
	"zoom":      zoom,
	"redact":    redact,
}

// Effect parses the spec of an effect of the kind named, one of [Kinds].
func Effect(kind, spec string) (reel.Effect, error) {
	build, ok := Kinds[kind]
	if !ok {
		return nil, fmt.Errorf("unknown effect %q", kind)
	}
	regionSpec, rest, _ := strings.Cut(spec, ";")
	region, err := Region(regionSpec)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", kind, spec, err)
	}
	opts := map[string]string{}
	for kv := range strings.SplitSeq(rest, ";") {
		if kv == "" {
			continue
		}
		k, v, isKV := strings.Cut(kv, "=")
		if !isKV {
			return nil, fmt.Errorf("%s %q: option %q is not KEY=VALUE", kind, spec, kv)
		}
		if _, dup := opts[k]; dup {
			return nil, fmt.Errorf("%s %q: option %q is repeated", kind, spec, k)
		}
		opts[k] = v
	}
	e, err := build(region, opts)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", kind, spec, err)
	}
	return e, nil
}

// Region parses a REGION.
func Region(s string) (effects.Region, error) {
	kind, v, ok := strings.Cut(s, "=")
	if !ok || v == "" {
		return nil, fmt.Errorf("region %q is not KIND=VALUE", s)
	}
	switch kind {
	case "row":
		y, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("row %q: %w", v, err)
		}
		return effects.Row(y), nil
	case "rows":
		ys, err := ints(v, "-", 2)
		if err != nil {
			return nil, fmt.Errorf("rows %q: %w", v, err)
		}
		return effects.Rows(ys[0], ys[1]), nil
	case "cells":
		n, err := ints(v, ",", 4)
		if err != nil {
			return nil, fmt.Errorf("cells %q: %w", v, err)
		}
		return effects.Cells(image.Rect(n[0], n[1], n[2], n[3])), nil
	case "text":
		return effects.Text(v), nil
	case "re":
		re, err := regexp.Compile(v)
		if err != nil {
			return nil, err
		}
		return effects.Regexp(re), nil
	case "env":
		return effects.Env(v), nil
	}
	return nil, fmt.Errorf("unknown region %q, want row, rows, cells, text, re, or env", kind)
}

// ints parses n whole numbers separated by sep.
func ints(s, sep string, n int) ([]int, error) {
	parts := strings.Split(s, sep)
	if len(parts) != n {
		return nil, fmt.Errorf("want %d numbers separated by %q", n, sep)
	}
	out := make([]int, n)
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// floats parses n numbers separated by sep.
func floats(s, sep string, n int) ([]float64, error) {
	parts := strings.Split(s, sep)
	if len(parts) != n {
		return nil, fmt.Errorf("want %d numbers separated by %q", n, sep)
	}
	out := make([]float64, n)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// options reads opts with the reader of each key; a key without one is an
// error.
func options[O any](opts map[string]string, readers map[string]func(v string) (O, error)) ([]O, error) {
	var out []O
	for k, v := range opts {
		read, ok := readers[k]
		if !ok {
			return nil, fmt.Errorf("unknown option %q", k)
		}
		o, err := read(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out = append(out, o)
	}
	return out, nil
}

// optBetween is the option every effect takes, to limit it to a window.
const optBetween = "between"

// between reads FROM,TO.
func between(v string) (effects.Timing, error) {
	from, to, ok := strings.Cut(v, ",")
	if !ok {
		return nil, fmt.Errorf("%q is not FROM,TO", v)
	}
	return effects.Between(from, to), nil
}

func ease(v string) (effects.Timing, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return nil, err
	}
	return effects.Ease(d), nil
}

func highlight(region effects.Region, opts map[string]string) (reel.Effect, error) {
	o, err := options(opts, map[string]func(string) (effects.HighlightOption, error){
		optBetween: func(v string) (effects.HighlightOption, error) { return between(v) },
		"ease":     func(v string) (effects.HighlightOption, error) { return ease(v) },
		"dim": func(v string) (effects.HighlightOption, error) {
			f, err := strconv.ParseFloat(v, 64)
			return effects.DimOthers(f), err
		},
		"padding": func(v string) (effects.HighlightOption, error) {
			px, err := strconv.Atoi(v)
			return effects.Padding(px), err
		},
		"outline": func(v string) (effects.HighlightOption, error) {
			hex, pxs, hasPx := strings.Cut(v, ":")
			c, err := themes.ParseColor(hex)
			if err != nil {
				return nil, err
			}
			px := 2
			if hasPx {
				if px, err = strconv.Atoi(pxs); err != nil {
					return nil, err
				}
			}
			return effects.Outline(c, px), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return effects.Highlight(region, o...), nil
}

func zoom(region effects.Region, opts map[string]string) (reel.Effect, error) {
	o, err := options(opts, map[string]func(string) (effects.ZoomOption, error){
		optBetween: func(v string) (effects.ZoomOption, error) { return between(v) },
		"ease":     func(v string) (effects.ZoomOption, error) { return ease(v) },
		"scale": func(v string) (effects.ZoomOption, error) {
			f, err := strconv.ParseFloat(v, 64)
			return effects.Scale(f), err
		},
		"anchor": func(v string) (effects.ZoomOption, error) {
			xy, err := floats(v, ",", 2)
			if err != nil {
				return nil, err
			}
			return effects.Anchor(xy[0], xy[1]), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return effects.Zoom(region, o...), nil
}

func redact(region effects.Region, opts map[string]string) (reel.Effect, error) {
	o, err := options(opts, map[string]func(string) (effects.RedactOption, error){
		optBetween: func(v string) (effects.RedactOption, error) { return between(v) },
		"style": func(v string) (effects.RedactOption, error) {
			switch {
			case v == "blur":
				return effects.Blur(), nil
			case v == "block":
				return effects.Block(), nil
			case strings.HasPrefix(v, "replace:") && len([]rune(v)) == len("replace:")+1:
				return effects.Replace([]rune(v)[len("replace:")]), nil
			}
			return nil, fmt.Errorf("style %q is not blur, block, or replace:CHAR", v)
		},
	})
	if err != nil {
		return nil, err
	}
	return effects.Redact(region, o...), nil
}
