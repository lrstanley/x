// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// thisLine returns the line it is called from.
func thisLine() int {
	_, _, line, _ := runtime.Caller(1)
	return line
}

// markAndAnnotate places a mark and an annotation, as a test helper that
// wraps a session does.
func markAndAnnotate(t *testing.T, s *Session) {
	t.Helper()
	if err := s.Mark("m"); err != nil {
		t.Fatal(err)
	}
	if err := s.Annotate(Annotation{Kind: "test.note"}); err != nil {
		t.Fatal(err)
	}
}

// sources returns the source of each mark and annotation of tl, by "mark"
// or kind, and fails the test when one has none.
func sources(t *testing.T, tl *Timeline) map[string]Source {
	t.Helper()
	out := map[string]Source{}
	for ev := range tl.Events() {
		var key string
		var src *Source
		switch ev := ev.(type) {
		case Mark:
			key, src = "mark", ev.Source
		case Annotation:
			key, src = ev.Kind, ev.Source
		default:
			continue
		}
		if src == nil {
			t.Fatalf("%s has no source: %+v", key, ev)
		}
		out[key] = *src
	}
	return out
}

func TestSessionSourceInTest(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2, WithSource(true))
	line := thisLine() + 1
	markAndAnnotate(t, s)

	want := Source{File: "source_test.go", Line: line, Func: reelPackage + ".TestSessionSourceInTest"}
	got := sources(t, s.Timeline())
	if len(got) != 2 || got["mark"] != want || got["test.note"] != want {
		t.Fatalf("sources = %+v, want %+v for the mark and the note", got, want)
	}
	if pkg := want.Package(); pkg != reelPackage {
		t.Errorf("Package() = %q, want %q", pkg, reelPackage)
	}

	// A window cut at the mark keeps its source.
	cut, err := s.Timeline().Between("", "m")
	if err != nil {
		t.Fatal(err)
	}
	if got := sources(t, cut)["mark"]; got != want {
		t.Fatalf("cut mark source = %+v, want %+v", got, want)
	}

	// A subtest's source is its own function.
	t.Run("sub", func(t *testing.T) {
		t.Parallel()
		sub := openTest(t, 10, 2, WithSource(true))
		subLine := thisLine() + 1
		markAndAnnotate(t, sub)
		got := sources(t, sub.Timeline())["mark"]
		if got.File != "source_test.go" || got.Line != subLine ||
			!strings.HasPrefix(got.Func, reelPackage+".TestSessionSourceInTest.func") {
			t.Fatalf("subtest source = %+v, want line %d of the subtest's function", got, subLine)
		}
	})
}

// TestSessionSourceGoroutine checks that, with no test function on the
// stack, the source is the innermost frame outside reel: the helper.
func TestSessionSourceGoroutine(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2, WithSource(true))
	done := make(chan struct{})
	go func() {
		defer close(done)
		markAndAnnotate(t, s)
	}()
	<-done
	got := sources(t, s.Timeline())["mark"]
	if got.Func != reelPackage+".markAndAnnotate" || got.File != "source_test.go" {
		t.Fatalf("source = %+v, want markAndAnnotate", got)
	}
}

func TestSessionSourceOff(t *testing.T) {
	t.Parallel()
	for _, opts := range [][]SessionOption{nil, {WithSource(false)}} {
		s := openTest(t, 10, 2, opts...)
		markAndAnnotate(t, s)
		for m := range s.Timeline().Marks() {
			if m.Source != nil {
				t.Fatalf("mark %s has a source without WithSource", m.Label)
			}
		}
		for a := range s.Timeline().Annotations() {
			if a.Source != nil {
				t.Fatalf("annotation %s has a source without WithSource", a.Kind)
			}
		}
	}
}

func TestSessionSourceKept(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2, WithSource(true))
	own := &Source{File: "x.go", Line: 3}
	if err := s.Annotate(Annotation{Kind: "test.note", Source: own}); err != nil {
		t.Fatal(err)
	}
	if got := sources(t, s.Timeline())["test.note"]; got != *own {
		t.Fatalf("source = %+v, want the annotation's own", got)
	}

	// "source" is reserved, as "kind" is: data must not hold it, whether
	// sources are recorded or not.
	a, err := newAnnotation("test.note", map[string]any{"source": map[string]any{"file": "x.go", "line": 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, sess := range []*Session{s, openTest(t, 10, 2)} {
		if err = sess.Annotate(a); !errors.Is(err, ErrInvalidAnnotation) {
			t.Fatalf("Annotate of data with a source member = %v, want ErrInvalidAnnotation", err)
		}
	}
	// A source that does not fit the schema is rejected.
	if err = s.Annotate(Annotation{Kind: "test.note", Source: &Source{File: "x.go"}}); !errors.Is(err, ErrInvalidAnnotation) {
		t.Fatalf("Annotate of a source without a line = %v, want ErrInvalidAnnotation", err)
	}
}

// TestSessionSourceCast checks that sources survive an asciicast file, and
// fit the schema as it reads them back: a mark's as a [MarkKind]
// annotation, which reads back as the mark's source.
func TestSessionSourceCast(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2, WithSource(true))
	markAndAnnotate(t, s)
	data := writeCast(t, s.Timeline(), CastV2)
	if !bytes.Contains(data, []byte(`[0.000,"R","{\"kind\":\"reel.mark\"`)) {
		t.Fatalf("cast has no reel.mark annotation:\n%s", data)
	}
	tl := readCast(t, data)
	got, want := sources(t, tl), sources(t, s.Timeline())
	if len(got) != 2 || got["mark"] != want["mark"] || got["test.note"] != want["test.note"] {
		t.Fatalf("read sources = %+v, want %+v", got, want)
	}
	for a := range tl.Annotations() {
		if a.Kind == MarkKind {
			t.Fatalf("read timeline keeps the %s annotation of its mark", MarkKind)
		}
	}

	// Writing what was read writes the same file.
	if again := writeCast(t, tl, CastV2); !bytes.Equal(again, data) {
		t.Fatalf("rewritten cast:\n%s\nwant\n%s", again, data)
	}

	if err := s.Annotate(markAnnotation("m", nil)); !errors.Is(err, ErrInvalidAnnotation) {
		t.Fatalf("Annotate of a %s annotation = %v, want ErrInvalidAnnotation", MarkKind, err)
	}
}

// TestReadMarkSourceUnmatched checks that a [MarkKind] annotation that is
// not of the mark just before it, at its offset, reads as an annotation.
func TestReadMarkSourceUnmatched(t *testing.T) {
	t.Parallel()
	const head = `{"version":2,"width":10,"height":2}` + "\n"
	for name, events := range map[string]string{
		"label":  `[0.5,"m","a"]` + "\n" + `[0.5,"R","{\"kind\":\"reel.mark\",\"source\":{\"file\":\"x.go\",\"line\":1},\"label\":\"b\"}"]`,
		"offset": `[0.5,"m","a"]` + "\n" + `[0.6,"R","{\"kind\":\"reel.mark\",\"source\":{\"file\":\"x.go\",\"line\":1},\"label\":\"a\"}"]`,
		"first":  `[0.5,"R","{\"kind\":\"reel.mark\",\"source\":{\"file\":\"x.go\",\"line\":1},\"label\":\"a\"}"]`,
	} {
		tl := readCast(t, []byte(head+events+"\n"))
		var marks, notes int
		for ev := range tl.Events() {
			switch ev := ev.(type) {
			case Mark:
				marks++
				if ev.Source != nil {
					t.Errorf("%s: mark has source %+v", name, ev.Source)
				}
			case Annotation:
				notes++
			}
		}
		if notes != 1 {
			t.Errorf("%s: %d marks and %d annotations, want the annotation kept", name, marks, notes)
		}
	}
}

func TestSourceModule(t *testing.T) {
	t.Parallel()
	path, dir, ok := SourceModule()
	if !ok || path != reelPackage || dir != "." {
		t.Fatalf("SourceModule() = %q, %q, %v; want %q, \".\"", path, dir, ok, reelPackage)
	}
	_, file, _, _ := runtime.Caller(0)
	for _, tt := range []struct{ file, fn, want string }{
		{file, reelPackage + ".TestSourceModule", "source_test.go"},
		{reelPackage + "/effects/x.go", reelPackage + "/effects.F", "effects/x.go"},
		{"/home/u/go/pkg/mod/gopkg.in/yaml.v3@v3.0.1/yaml.go", "gopkg.in/yaml%2ev3.Marshal", "gopkg.in/yaml.v3/yaml.go"},
		{"/usr/lib/go/src/testing/testing.go", "testing.tRunner", "testing/testing.go"},
	} {
		if got := sourceFile(tt.file, tt.fn); got != tt.want {
			t.Errorf("sourceFile(%q, %q) = %q, want %q", tt.file, tt.fn, got, tt.want)
		}
	}
}

func TestModulePath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ gomod, want string }{
		{"module example.com/a\n\ngo 1.27\n", "example.com/a"},
		{"// module comment\nmodule \"example.com/q\" // c\n", "example.com/q"},
		{"modules x\n\tmodule\texample.com/t\n", "example.com/t"},
		{"go 1.27\n", ""},
	} {
		if got := modulePath([]byte(tt.gomod)); got != tt.want {
			t.Errorf("modulePath(%q) = %q, want %q", tt.gomod, got, tt.want)
		}
	}
}
