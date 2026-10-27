// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lrstanley/x/term/reel/sink"
)

// castExt is the extension of asciicast outputs.
const castExt = ".cast"

// outputs are a profile's outputs, with their paths expanded, for one
// recording.
type outputs struct {
	casts []castOutput
	// files are the recordings and the per-mark stills.
	files []fileOutput
	// sink writes to every recording once they are open.
	sink sink.Sink
}

type castOutput struct {
	path    string
	version CastVersion
}

type fileOutput struct {
	path string
	opts []sink.Option
	// still is set for the still of the first mark labeled mark, and
	// written once it has its frame.
	still   bool
	mark    string
	written bool
	sink    sink.Sink
}

func (f *fileOutput) errNoMark() error {
	return fmt.Errorf("%w: %q for %s", ErrNoMark, f.mark, f.path)
}

// planOutputs expands the paths of p's outputs with vars.
func planOutputs(p Profile, vars map[string]string) (*outputs, error) {
	if len(p.Outputs) == 0 {
		return nil, errors.New("reel: profile has no outputs")
	}
	o := &outputs{}
	seen := make(map[string]bool, len(p.Outputs))
	add := func(path string) error {
		clean := filepath.Clean(path)
		if seen[clean] {
			return fmt.Errorf("reel: two outputs write %q", path)
		}
		seen[clean] = true
		return nil
	}
	for _, out := range p.Outputs {
		opts := append(slices.Clip(p.Encode), out.Options...)
		isCast := strings.EqualFold(filepath.Ext(out.Path), castExt)
		switch {
		case out.Cast != 0 && !isCast:
			return nil, fmt.Errorf("reel: output %q: Cast is set on a path that is not %s", out.Path, castExt)
		case len(out.Marks) > 0 && isCast:
			return nil, fmt.Errorf("reel: output %q: a %s output cannot have Marks", out.Path, castExt)
		case len(out.Marks) > 0 && !strings.Contains(out.Path, "{"+varMark+"}"):
			return nil, fmt.Errorf("reel: output %q: a path with Marks must hold {%s}", out.Path, varMark)
		}
		if len(out.Marks) == 0 {
			path, err := expandPath(out.Path, vars)
			if err != nil {
				return nil, err
			}
			err = add(path)
			if err != nil {
				return nil, err
			}
			if isCast {
				o.casts = append(o.casts, castOutput{path: path, version: cmp.Or(out.Cast, CastV3)})
			} else {
				o.files = append(o.files, fileOutput{path: path, opts: opts})
			}
			continue
		}
		markVars := maps.Clone(vars)
		for _, label := range out.Marks {
			markVars[varMark] = label
			path, err := expandPath(out.Path, markVars)
			if err != nil {
				return nil, err
			}
			err = add(path)
			if err != nil {
				return nil, err
			}
			o.files = append(o.files, fileOutput{path: path, opts: opts, still: true, mark: label})
		}
	}
	return o, nil
}

// checkRedaction checks that every [Redactor] among effects comes before
// every other effect, and when there is one, that no output is written
// without the effects.
func (o *outputs) checkRedaction(effects []Effect) error {
	n := len(redactors(effects))
	for i := n + 1; i < len(effects); i++ {
		if redacts(effects[i]) {
			return fmt.Errorf("reel: effect %d (%T) redacts, so it must come before effect %d (%T), which does not", i, effects[i], n, effects[n])
		}
	}
	if n > 0 && len(o.casts) > 0 {
		return fmt.Errorf("%w: %q is an asciicast file, the timeline as recorded", ErrUnredacted, o.casts[0].path)
	}
	return nil
}

// redactors returns the effects effects starts with that are a [Redactor]
// that redacts.
func redactors(effects []Effect) []Effect {
	n := 0
	for n < len(effects) && redacts(effects[n]) {
		n++
	}
	return effects[:n]
}

// redacts reports whether e is a [Redactor] that redacts.
func redacts(e Effect) bool {
	r, ok := e.(Redactor)
	return ok && r.Redacts()
}

// hasRecording reports whether any output records frames, rather than
// stills.
func (o *outputs) hasRecording() bool {
	return slices.ContainsFunc(o.files, func(f fileOutput) bool { return !f.still })
}

// hasStills reports whether any output is a still.
func (o *outputs) hasStills() bool {
	return slices.ContainsFunc(o.files, func(f fileOutput) bool { return f.still })
}

// missingMarks returns an error wrapping [ErrNoMark] for each still whose
// label is not among marks, or nil.
func (o *outputs) missingMarks(marks []string) error {
	var errs []error
	for i := range o.files {
		if f := &o.files[i]; f.still && !slices.Contains(marks, f.mark) {
			errs = append(errs, f.errNoMark())
		}
	}
	return errors.Join(errs...)
}

// open creates every recording and still, and is discarded when one fails.
func (o *outputs) open() error {
	var recordings []sink.Sink
	for i := range o.files {
		f := &o.files[i]
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

// stillsAt returns the stills of label not yet written, marking them
// written.
func (o *outputs) stillsAt(label string) []sink.Sink {
	var out []sink.Sink
	for i := range o.files {
		if f := &o.files[i]; f.still && f.mark == label && !f.written {
			f.written = true
			out = append(out, f.sink)
		}
	}
	return out
}

// close finishes every open output. Stills whose mark never came write
// nothing, and are reported as errors wrapping [ErrNoMark].
func (o *outputs) close() error {
	var errs []error
	for i := range o.files {
		f := &o.files[i]
		if f.sink != nil {
			errs = append(errs, f.sink.Close())
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

// writeCasts writes tl, with header h, to every asciicast output. A cut is
// replayed once for all of them. Each is written to a temporary file beside
// it, renamed into place once complete.
func (o *outputs) writeCasts(tl *Timeline, h Header) error {
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
	for _, c := range o.casts {
		if err := writeCastFile(c.path, tl, c.version); err != nil {
			return fmt.Errorf("reel: output %q: %w", c.path, err)
		}
	}
	return nil
}

func writeCastFile(path string, tl *Timeline, v CastVersion) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tl.WriteCast(f, v)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}
