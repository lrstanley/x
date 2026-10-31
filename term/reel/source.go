// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MarkKind is the [Annotation.Kind] that holds the [Source] of a [Mark] in
// an asciicast file: [Timeline.WriteTo] writes one just after each mark
// with a source, and [ReadTimeline] reads it back as that mark's source. A
// timeline never holds one, so [Session.Annotate] rejects it.
const MarkKind = "reel.mark"

// reelPackage is the import path of reel, whose packages, and those under
// it, [Caller] always skips.
var reelPackage = reflect.TypeFor[Session]().PkgPath()

// Source is a place in Go source code, such as the code that placed a mark
// or an annotation (see [WithSource]).
type Source struct {
	// File is the path of the source file, with forward slashes: relative to
	// the root of the module [SourceModule] names when the file is in it,
	// and otherwise the import path of the function's package joined with
	// the file's name, so paths hold no home or build directories.
	File string `json:"file"`
	// Line is the line in File, counted from 1.
	Line int `json:"line"`
	// Func is the function, qualified by its package's import path, as Go's
	// runtime names it, such as "example.com/app.TestLogin.func1".
	Func string `json:"func,omitzero"`
}

// Package returns the import path of the package of s.Func.
func (s Source) Package() string { return funcPackage(s.Func) }

// funcPackage returns the import path of the package of a function named
// as the runtime names it, where dots in the last element of the path are
// escaped as "%2e".
func funcPackage(fn string) string {
	slash := strings.LastIndexByte(fn, '/')
	if dot := strings.IndexByte(fn[slash+1:], '.'); dot >= 0 {
		fn = fn[:slash+1+dot]
	}
	return strings.ReplaceAll(fn, "%2e", ".")
}

// inPackage reports whether pkg is prefix or under it.
func inPackage(pkg, prefix string) bool {
	rest, ok := strings.CutPrefix(pkg, prefix)
	return ok && (rest == "" || rest[0] == '/')
}

// Caller returns the source of the code that called into reel, or into
// the packages of skip, each of which covers the packages under it. Frames
// of the runtime, reflection, and the standard library's internal packages
// are skipped too, while frames in "_test.go" files never are.
//
// Inside a test, it returns the outermost frame left below the testing
// package, which is the test's own function, or the function a subtest
// runs: the line in the test that led to the call, through any of the
// test's helpers, as [testing.TB.Helper] reports it for helpers that call
// it. Elsewhere, such as on a goroutine a test starts, it returns the
// innermost frame left. ok is false when no frame is left.
//
// It walks the stack, so it should only be called when its result is used.
func Caller(skip ...string) (src Source, ok bool) {
	pcs := make([]uintptr, 32)
	for n := runtime.Callers(2, pcs); ; n = runtime.Callers(2, pcs) {
		if n < len(pcs) {
			pcs = pcs[:n]
			break
		}
		pcs = make([]uintptr, 2*len(pcs))
	}
	frames := runtime.CallersFrames(pcs)
	var found, outer runtime.Frame
	for more := true; more; {
		var f runtime.Frame
		f, more = frames.Next()
		pkg := funcPackage(f.Function)
		if inPackage(pkg, "testing") {
			// Inside a test: its function is the outermost frame left.
			found = outer
			break
		}
		if !skipFrame(f, pkg, skip) {
			if found.Function == "" {
				found = f
			}
			outer = f
		}
	}
	if found.Function == "" {
		return Source{}, false
	}
	return sourceOf(found), true
}

// skipFrame reports whether [Caller] skips f, a frame of a function in
// pkg.
func skipFrame(f runtime.Frame, pkg string, skip []string) bool {
	if f.Function == "" {
		return true
	}
	if strings.HasSuffix(f.File, "_test.go") {
		return false
	}
	if pkg == "runtime" || pkg == "reflect" || inPackage(pkg, "internal") || inPackage(pkg, reelPackage) {
		return true
	}
	for _, p := range skip {
		if inPackage(pkg, p) {
			return true
		}
	}
	return false
}

// sourceOf returns the [Source] of f.
func sourceOf(f runtime.Frame) Source {
	return Source{File: sourceFile(f.File, f.Function), Line: f.Line, Func: f.Function}
}

// sourceFile returns the [Source.File] of file, a path from a frame of
// function fn.
func sourceFile(file, fn string) string {
	if m := sourceModule(); m.path != "" {
		if rest, ok := strings.CutPrefix(file, m.root+"/"); ok {
			return rest
		}
		// Built with -trimpath: the module path stands in for its root.
		if rest, ok := strings.CutPrefix(file, m.path+"/"); ok {
			return rest
		}
	}
	return path.Join(funcPackage(fn), path.Base(file))
}

// module is the Go module the working directory is in.
type module struct {
	// path is the module's path, or empty outside a module.
	path string
	// root is the module's root directory, with forward slashes.
	root string
	// dir is the working directory relative to root, with forward slashes.
	dir string
}

// sourceModule finds the go.mod file in the working directory or above it,
// once, since a test's working directory is its package's directory.
var sourceModule = sync.OnceValue(func() module {
	wd, err := os.Getwd()
	if err != nil {
		return module{}
	}
	for dir := wd; ; {
		if data, readErr := os.ReadFile(filepath.Join(dir, "go.mod")); readErr == nil {
			rel, relErr := filepath.Rel(dir, wd)
			if relErr != nil {
				return module{}
			}
			return module{path: modulePath(data), root: filepath.ToSlash(dir), dir: filepath.ToSlash(rel)}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return module{}
		}
		dir = parent
	}
})

// modulePath returns the module path of a go.mod file, or "" when it has
// none.
func modulePath(gomod []byte) string {
	for line := range strings.Lines(string(gomod)) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module")
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t' && rest[0] != '"') {
			continue
		}
		rest, _, _ = strings.Cut(rest, "//")
		rest = strings.TrimSpace(rest)
		if p, err := strconv.Unquote(rest); err == nil {
			return p
		}
		return rest
	}
	return ""
}

// SourceModule returns the path of the Go module that [Source.File] paths
// are relative to, the one the working directory is in, and the working
// directory relative to its root, "." at the root. A test's working
// directory is its package's directory, so dir joined with a snapshot path
// a test names is relative to the root too. ok is false outside a module.
func SourceModule() (modPath, dir string, ok bool) {
	m := sourceModule()
	return m.path, m.dir, m.path != ""
}

// markNote is the data of a [MarkKind] annotation.
type markNote struct {
	Label string `json:"label"`
}

// markAnnotation returns the [MarkKind] annotation of the mark labeled
// label, placed by src.
func markAnnotation(label string, src *Source) Annotation {
	a, err := newAnnotation(MarkKind, markNote{Label: label})
	if err != nil {
		panic(err) // A markNote always encodes.
	}
	a.Source = src
	return a
}

// attachMarkSource sets the [Mark.Source] of the timeline's last event to
// that of a, read from a file at offset at, when a is the [MarkKind]
// annotation of that mark, at its offset. It reports whether it did, so a
// is not appended. It is for [ReadTimeline], before the timeline is shared.
func (t *Timeline) attachMarkSource(a Annotation, at time.Duration) bool {
	if a.Kind != MarkKind || a.Source == nil || len(t.events) == 0 {
		return false
	}
	m, ok := t.events[len(t.events)-1].(Mark)
	var note markNote
	if !ok || m.Source != nil || m.At() != eventOffset(at) ||
		a.Decode(&note) != nil || note.Label != m.Label {
		return false
	}
	m.Source = a.Source
	t.events[len(t.events)-1] = m
	return true
}
