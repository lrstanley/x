// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lrstanley/x/term/reel/internal/testutil"
)

// describer is a [MarkChecker] that describes itself as a, or fails with
// err, looks for the marks labeled marks, and changes no frame.
type describer struct {
	a     Annotation
	err   error
	marks []string
}

func (describer) Start(*Context) (Stage, error) { return nil, nil } //nolint:nilnil // It changes no frame.

func (d describer) Describe() (Annotation, error) { return d.a, d.err }

func (d describer) CheckMarks(tl *Timeline) error {
	var errs []error
	for _, label := range d.marks {
		if _, err := tl.Between(label, ""); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// castEvents returns the events of the asciicast file at path, one string
// each.
func castEvents(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for ev := range readCast(t, data).Events() {
		switch ev := ev.(type) {
		case Annotation:
			out = append(out, fmt.Sprintf("%v R %s %s", ev.At(), ev.Kind, ev.Data))
		case Output:
			out = append(out, fmt.Sprintf("%v o %q", ev.At(), ev.Data))
		case Mark:
			out = append(out, fmt.Sprintf("%v m %s", ev.At(), ev.Label))
		default:
			out = append(out, fmt.Sprintf("%v %T", ev.At(), ev))
		}
	}
	return out
}

// TestExportDescribesEffects checks that an asciicast output holds the
// descriptions of the profile's effects, at its start, in place of the
// timeline's annotations of their kinds and of [EffectKind], leaving out the
// effects that do not describe themselves.
func TestExportDescribesEffects(t *testing.T) {
	t.Parallel()
	note := func(atMS int, kind, data string) Annotation {
		return Annotation{event: at(atMS), Kind: kind, Data: jsontext.Value(data)}
	}
	tl := buildTimeline(Header{Cols: 4, Rows: 1},
		outAt(0, "a"),
		note(5, EffectKind, `{"effect":"keys","shown_ms":9}`),
		note(6, "test.fx", `{"n":1}`),
		note(7, "test.note", `{"n":1}`),
		Mark{event: at(8), Label: "m"},
		outAt(10, "b"),
	)
	effects := []Effect{
		describer{a: note(0, EffectKind, `{"effect":"keys","shown_ms":1}`)},
		describer{err: fmt.Errorf("a region of its own: %w", errors.ErrUnsupported)},
		failEffect{},
		wrapped{describer{a: note(0, "test.wrapped", "")}},
		describer{a: note(0, "test.fx", `{"n":2}`)},
	}
	want := []string{
		`0s R reel.effect {"effect":"keys","shown_ms":1}`,
		`0s R test.fx {"n":2}`,
		`0s o "a"`,
		`7ms R test.note {"n":1}`,
		`8ms m m`,
		`10ms o "b"`,
	}
	for _, v := range []CastVersion{CastV2, CastV3} {
		dir := t.TempDir()
		p := Profile{Outputs: []File{{Path: filepath.Join(dir, "a.cast"), Cast: v}}, Effects: effects}
		if err := Export(t.Context(), tl, p); err != nil {
			t.Fatal(err)
		}
		if got := castEvents(t, p.Outputs[0].Path); !slices.Equal(got, want) {
			t.Fatalf("v%d events:\n%s\nwant:\n%s", v, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		// Exported again with the same effects, the file is the same.
		first, err := os.ReadFile(p.Outputs[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		again := readCast(t, first)
		p.Outputs[0].Path = filepath.Join(dir, "b.cast")
		if err = Export(t.Context(), again, p); err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(p.Outputs[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("v%d: exported again:\n%s\nwant:\n%s", v, second, first)
		}
	}

	// A cut starts with the descriptions too.
	cut, err := tl.Between("m", "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cut.cast")
	if err = Export(t.Context(), cut, Profile{Outputs: MustFiles(path), Effects: effects}); err != nil {
		t.Fatal(err)
	}
	got := castEvents(t, path)
	if len(got) != 5 || !slices.Equal(got[:2], want[:2]) || got[3] != "0s m m" || got[4] != `2ms o "b"` {
		t.Fatalf("cut events:\n%s", strings.Join(got, "\n"))
	}

	// Without effects that describe themselves, the timeline's effects are
	// left out.
	path = filepath.Join(t.TempDir(), "none.cast")
	if err = Export(t.Context(), tl, Profile{Outputs: MustFiles(path)}); err != nil {
		t.Fatal(err)
	}
	if got = castEvents(t, path); !slices.Equal(got, []string{`0s o "a"`, `6ms R test.fx {"n":1}`, `7ms R test.note {"n":1}`, `8ms m m`, `10ms o "b"`}) {
		t.Fatalf("events without effects:\n%s", strings.Join(got, "\n"))
	}
}

func TestExportDescribeErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		effect Effect
		is     error
		want   string
	}{
		"error":   {describer{err: errEffect}, errEffect, "effect 0 (reel.describer)"},
		"invalid": {describer{a: Annotation{Kind: EffectKind, Data: jsontext.Value(`{"effect":"keys"}`)}}, ErrInvalidAnnotation, "effect 0"},
		"kind":    {describer{a: Annotation{Kind: "Bad"}}, ErrInvalidAnnotation, "effect 0"},
	} {
		dir := t.TempDir()
		p := Profile{Outputs: MustFiles(filepath.Join(dir, "a.cast"), filepath.Join(dir, "a.gif")), Rasterizer: testutil.Rasterizer{}, Effects: []Effect{tc.effect}}
		err := Export(t.Context(), buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a")), p)
		if !errors.Is(err, tc.is) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Export = %v, want an error containing %q", name, err, tc.want)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s: wrote %d files", name, len(entries))
		}
	}
	// Without an asciicast output, the effects are not described.
	p := Profile{Outputs: MustFiles(filepath.Join(t.TempDir(), "a.gif")), Rasterizer: testutil.Rasterizer{}, Effects: []Effect{describer{err: errEffect}}}
	if err := Export(t.Context(), buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a")), p); err != nil {
		t.Fatal(err)
	}
}

// TestExportEffectMarks checks that an asciicast output of a cut leaves out
// the description of a [MarkChecker] that looks for a mark outside it, which
// the file does not have, and reports the mark, and that the file exports
// again with the effects it describes. The effects package checks its own
// effects, which report their marks themselves when they run on frames.
func TestExportEffectMarks(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 2, Rows: 1},
		Mark{event: at(0), Label: "a"},
		outAt(1, "x"),
		Mark{event: at(2), Label: "b"},
		outAt(3, "y"),
		Mark{event: at(4), Label: "c"},
	)
	cut, err := tl.Between("b", "")
	if err != nil {
		t.Fatal(err)
	}
	keys := Annotation{Kind: EffectKind, Data: jsontext.Value(`{"effect":"keys","shown_ms":1}`)}
	for name, tc := range map[string]struct {
		marks []string
		ok    bool
	}{
		"none":    {nil, true},
		"inside":  {[]string{"b", "c"}, true},
		"outside": {[]string{"c", "a"}, false},
	} {
		effects := []Effect{describer{a: keys, marks: tc.marks}}
		dir := t.TempDir()
		path := filepath.Join(dir, "a.cast")
		p := Profile{Outputs: MustFiles(path), Effects: effects}
		err := Export(t.Context(), cut, p)
		switch labels, rest := MissingMarks(err); {
		case !tc.ok && (rest != nil || !slices.Equal(labels, []string{"a"})):
			t.Errorf("%s: Export = %v, want a NoMarkError for \"a\"", name, err)
			continue
		case tc.ok && err != nil:
			t.Errorf("%s: %v", name, err)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if described := strings.Contains(string(data), `\"effect\":\"keys\"`); described != tc.ok {
			t.Errorf("%s: the file describes the effect: %v, want %v", name, described, tc.ok)
		}
		if !tc.ok {
			continue
		}
		again := filepath.Join(t.TempDir(), "a.cast")
		if err = Export(t.Context(), readCast(t, data), Profile{Outputs: MustFiles(again), Effects: effects}); err != nil {
			t.Errorf("%s: exporting the file again: %v", name, err)
		}
	}
}

// TestRecordEffectMarks checks that the asciicast output of a recording
// leaves out the description of an effect whose marks never came, which
// Stop reports when the effects did not run on frames, which report it
// themselves.
func TestRecordEffectMarks(t *testing.T) {
	t.Parallel()
	keys := `{"effect":"keys","shown_ms":1}`
	effects := []Effect{
		describer{a: Annotation{Kind: EffectKind, Data: jsontext.Value(keys)}, marks: []string{"m"}},
		describer{a: Annotation{Kind: EffectKind, Data: jsontext.Value(`{"effect":"pause"}`)}, marks: []string{"never"}},
	}
	for _, drawn := range []bool{false, true} {
		s := openSession(t, 4, 1)
		dir := t.TempDir()
		path := filepath.Join(dir, "a.cast")
		p := Profile{Outputs: MustFiles(path), Effects: effects}
		if drawn {
			p.Outputs, p.Rasterizer = MustFiles(path, filepath.Join(dir, "a.gif")), testutil.Rasterizer{}
		}
		rec, err := Record(t.Context(), s, p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
		if err = s.Mark("m"); err != nil {
			t.Fatal(err)
		}
		err = rec.Stop()
		switch {
		case drawn && err != nil:
			// The describer does not look for its marks on frames.
			t.Fatalf("drawn: Stop = %v, want nil", err)
		case !drawn && (!errors.Is(err, ErrNoMark) || !strings.Contains(err.Error(), `"never"`)):
			t.Fatalf("Stop = %v, want ErrNoMark for \"never\"", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for a := range readCast(t, data).Annotations() {
			if a.Kind == EffectKind {
				got = append(got, string(a.Data))
			}
		}
		if want := []string{keys}; !slices.Equal(got, want) {
			t.Fatalf("drawn %v: effects = %q, want %q", drawn, got, want)
		}
	}
}
