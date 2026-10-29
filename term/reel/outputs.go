// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lrstanley/x/term/reel/sink"
)

// Formats of the outputs reel writes itself rather than through
// [sink.File], named as sink names its own: by extension.
const (
	formatCast sink.Format = "cast"
	formatSnap sink.Format = "snap"
	formatJSON sink.Format = "json"
)

// formatOf returns the format of an output at path, by its extension.
func formatOf(name string) (sink.Format, error) {
	f := sink.Format(strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")))
	if f == formatCast || f == formatSnap || f == formatJSON {
		return f, nil
	}
	return sink.FormatOf(name)
}

// outputs are a profile's outputs, with paths expanded, for one recording.
type outputs struct {
	casts []castOutput
	// files are the recordings, the per-mark stills, and the screen
	// outputs.
	files []fileOutput
	// sink tees to every recording once open.
	sink sink.Sink
}

type castOutput struct {
	path    string
	version CastVersion
}

type fileOutput struct {
	path   string
	format sink.Format
	opts   []sink.Option
	// still is set for the still of the first mark labeled mark.
	still   bool
	mark    string
	written bool
	// sink is a drawn output's, once open; data is a screen output's.
	sink sink.Sink
	data []byte
}

// isScreen reports whether f holds the screen's text or [Screen] rather than
// images.
func (f *fileOutput) isScreen() bool { return isScreenFormat(f.format) }

func isScreenFormat(f sink.Format) bool { return f == formatSnap || f == formatJSON }

func (f *fileOutput) errNoMark() error {
	return fmt.Errorf("%w for %s", &NoMarkError{Label: f.mark}, f.path)
}

// planOutputs expands the paths of p's outputs with vars. tl is the
// timeline [File.MarkMatch] patterns match the marks of, or nil for a
// recording of a timeline still growing, which cannot have them.
func planOutputs(p Profile, vars map[string]string, tl *Timeline) (*outputs, error) {
	if len(p.Outputs) == 0 {
		return nil, errors.New("reel: profile has no outputs")
	}
	o := &outputs{}
	seen := make(map[string]bool, len(p.Outputs))
	add := func(dst string) error {
		clean := filepath.Clean(dst)
		if seen[clean] {
			return fmt.Errorf("reel: two outputs write %q", dst)
		}
		seen[clean] = true
		return nil
	}
	for _, out := range p.Outputs {
		format, err := formatOf(out.Path)
		if err != nil {
			return nil, fmt.Errorf("reel: output %q: %w", out.Path, err)
		}
		err = out.checkMarkPath()
		if err != nil {
			return nil, err
		}
		perMark := len(out.Marks) > 0 || len(out.MarkMatch) > 0
		switch {
		case out.Cast != 0 && format != formatCast:
			return nil, fmt.Errorf("reel: output %q: Cast is set on a path that is not .%s", out.Path, formatCast)
		case perMark && format == formatCast:
			return nil, fmt.Errorf("reel: output %q: a .%s output cannot have Marks or MarkMatch", out.Path, formatCast)
		case len(out.MarkMatch) > 0 && tl == nil:
			return nil, fmt.Errorf("reel: output %q: MarkMatch needs a finished timeline; use Export", out.Path)
		case isScreenFormat(format) && len(out.Options) > 0:
			return nil, fmt.Errorf("reel: output %q: a .%s output takes no Options", out.Path, format)
		}
		opts := append(slices.Clip(p.Encode), out.Options...)
		if !perMark {
			var dst string
			dst, err = ExpandPath(out.Path, vars)
			if err != nil {
				return nil, err
			}
			err = add(dst)
			if err != nil {
				return nil, err
			}
			if format == formatCast {
				o.casts = append(o.casts, castOutput{path: dst, version: cmp.Or(out.Cast, CastV2)})
			} else {
				o.files = append(o.files, fileOutput{path: dst, format: format, opts: opts})
			}
			continue
		}
		labels, err := markLabels(out, tl)
		if err != nil {
			return nil, err
		}
		markVars := maps.Clone(vars)
		for _, label := range labels {
			markVars[varMark] = label
			var dst string
			dst, err = ExpandPath(out.Path, markVars)
			if err != nil {
				return nil, err
			}
			err = add(dst)
			if err != nil {
				return nil, err
			}
			o.files = append(o.files, fileOutput{path: dst, format: format, opts: opts, still: true, mark: label})
		}
	}
	return o, nil
}

// checkMarkPath reports an error if f has Marks or MarkMatch but its path
// does not hold {mark}.
func (f File) checkMarkPath() error {
	if (len(f.Marks) > 0 || len(f.MarkMatch) > 0) && !strings.Contains(f.Path, "{"+varMark+"}") {
		return fmt.Errorf("reel: output %q: a path with Marks or MarkMatch must hold {%s}", f.Path, varMark)
	}
	return nil
}

// markLabels returns the labels of out's stills: its Marks, then the labels
// of tl's window that its MarkMatch patterns match, oldest first, each once.
// A matched label must be a file name, so it cannot move the still out of
// its directory.
func markLabels(out File, tl *Timeline) ([]string, error) {
	labels := slices.Clone(out.Marks)
	if len(out.MarkMatch) == 0 {
		return labels, nil
	}
	for _, pattern := range out.MarkMatch {
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("reel: output %q: MarkMatch %q: %w", out.Path, pattern, err)
		}
	}
	seen := make(map[string]bool, len(labels))
	for _, label := range labels {
		seen[label] = true
	}
	for ev := range tl.EventsFrom(max(tl.from, 0)) {
		m, ok := ev.(Mark)
		if !ok || m.Label == "" || seen[m.Label] || !slices.ContainsFunc(out.MarkMatch, func(p string) bool {
			matched, _ := path.Match(p, m.Label)
			return matched
		}) {
			continue
		}
		if m.Label == "." || m.Label == ".." || strings.ContainsAny(m.Label, `/\`) {
			return nil, fmt.Errorf("reel: output %q: mark %q matches MarkMatch, but is not a file name", out.Path, m.Label)
		}
		seen[m.Label] = true
		labels = append(labels, m.Label)
	}
	return labels, nil
}

// checkEffects enforces the ordering and output rules of [Concealer] and
// [Cutter].
func (o *outputs) checkEffects(effects []Effect) error {
	n := 0
	for n < len(effects) && conceals(effects[n]) {
		n++
	}
	for i := n + 1; i < len(effects); i++ {
		if conceals(effects[i]) {
			return fmt.Errorf("reel: effect %d (%T) conceals, so it must come before effect %d (%T), which does not", i, effects[i], n, effects[n])
		}
	}
	if len(o.casts) > 0 && (n > 0 || slices.ContainsFunc(effects, cuts)) {
		return fmt.Errorf("%w: %q is an asciicast file, the timeline as recorded", ErrRawOutput, o.casts[0].path)
	}
	return nil
}

// conceals reports whether e is a [Concealer] that conceals.
func conceals(e Effect) bool {
	c, ok := effectAs[Concealer](e)
	return ok && c.Conceals()
}

// cuts reports whether e is a [Cutter] that cuts.
func cuts(e Effect) bool {
	c, ok := effectAs[Cutter](e)
	return ok && c.Cuts()
}

// effectAs returns the first effect in e's Unwrap chain (e included) that is
// a T.
func effectAs[T Effect](e Effect) (T, bool) {
	for e != nil {
		if t, ok := e.(T); ok {
			return t, true
		}
		u, ok := e.(interface{ Unwrap() Effect })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	var zero T
	return zero, false
}

// stillEffects returns, in order, the effects that run on stills: active
// concealers and [StillEffect]s whose Stills reports true.
func stillEffects(effects []Effect) []Effect {
	var out []Effect
	for _, e := range effects {
		if s, ok := effectAs[StillEffect](e); conceals(e) || ok && s.Stills() {
			out = append(out, e)
		}
	}
	return out
}

// hasRecording reports whether any output is a recording rather than a
// still.
func (o *outputs) hasRecording() bool {
	return slices.ContainsFunc(o.files, func(f fileOutput) bool { return !f.still && !f.isScreen() })
}

// hasDrawn reports whether any output is drawn.
func (o *outputs) hasDrawn() bool {
	return slices.ContainsFunc(o.files, func(f fileOutput) bool { return !f.isScreen() })
}

// hasStills reports whether any output is a still, drawn or not when drawn
// is false.
func (o *outputs) hasStills(drawn bool) bool {
	return slices.ContainsFunc(o.files, func(f fileOutput) bool { return f.still && (!drawn || !f.isScreen()) })
}

// open creates every drawn output. On error, the caller discards o.
func (o *outputs) open() error {
	var recordings []sink.Sink
	for i := range o.files {
		f := &o.files[i]
		if f.isScreen() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o750); err != nil {
			return err
		}
		s, err := sink.File(f.path, f.opts...)
		if err != nil {
			return fmt.Errorf("reel: output %q: %w", f.path, err)
		}
		f.sink = s
		if !f.still {
			recordings = append(recordings, s)
		}
	}
	o.sink = sink.Tee(recordings...)
	return nil
}

// take returns the outputs not yet written for which match reports true,
// marking them written.
func (o *outputs) take(match func(*fileOutput) bool) []*fileOutput {
	var out []*fileOutput
	for i := range o.files {
		if f := &o.files[i]; !f.written && match(f) {
			f.written = true
			out = append(out, f)
		}
	}
	return out
}

// close finishes every output, writing the screen outputs. Stills whose
// mark never came write nothing, and are reported as a [NoMarkError] each.
func (o *outputs) close() error {
	var errs []error
	for i := range o.files {
		f := &o.files[i]
		switch {
		case f.sink != nil:
			errs = append(errs, f.sink.Close())
		case f.isScreen() && f.written:
			if err := writeFileAtomic(f.path, f.data); err != nil {
				errs = append(errs, fmt.Errorf("reel: output %q: %w", f.path, err))
			}
		}
		if f.still && !f.written {
			errs = append(errs, f.errNoMark())
		}
	}
	return errors.Join(errs...)
}

// discard abandons every open output.
func (o *outputs) discard() {
	for _, f := range o.files {
		if f.sink != nil {
			_ = f.sink.Discard()
		}
	}
}

// writeCasts writes tl with header h to every asciicast output, with the
// effect descriptions at the start replacing tl's annotations of
// [EffectKind] and of the described kinds. Each file is written to a
// temporary file and renamed into place.
func (o *outputs) writeCasts(tl *Timeline, h Header, effects []Annotation) error {
	if len(o.casts) == 0 {
		return nil
	}
	tl = tl.fixed(h, -1)
	if tl.from >= 0 {
		var err error
		if tl, err = tl.cut(); err != nil {
			return err
		}
	}
	kinds := map[string]bool{EffectKind: true}
	events := make([]Event, 0, len(effects)+tl.Len())
	for _, a := range effects {
		kinds[a.Kind] = true
		events = append(events, a.stamp(0))
	}
	for _, ev := range tl.snapshot() {
		if a, ok := ev.(Annotation); !ok || !kinds[a.Kind] {
			events = append(events, ev)
		}
	}
	tl = &Timeline{header: tl.header, start: tl.started(), events: events, from: -1}
	for _, c := range o.casts {
		if err := writeCastFile(c.path, tl, c.version); err != nil {
			return fmt.Errorf("reel: output %q: %w", c.path, err)
		}
	}
	return nil
}

func writeCastFile(name string, tl *Timeline, v CastVersion) error {
	return writeFileWith(name, func(w io.Writer) error {
		_, err := tl.WriteCast(w, v)
		return err
	})
}

// writeFileAtomic writes data to name.
func writeFileAtomic(name string, data []byte) error {
	return writeFileWith(name, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// writeFileWith writes name with write, through a temporary file renamed
// into place, so a failure leaves an existing file untouched.
//
//nolint:gosec // name is an output path its caller chose to write.
func writeFileWith(name string, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), name)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}
