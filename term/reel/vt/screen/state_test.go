// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package screen

import (
	"image/color"
	"reflect"
	"testing"
)

// TestStateEqual checks that Equal notices a change to every field, so a
// field added later cannot be missed.
func TestStateEqual(t *testing.T) {
	t.Parallel()

	base := State{ProgramStatuses: []ProgramStatus{{Title: "a"}}}
	if !base.Equal(State{ProgramStatuses: []ProgramStatus{{Title: "a"}}}) {
		t.Fatal("equal states are not Equal")
	}
	typ := reflect.TypeFor[State]()
	for i := range typ.NumField() {
		other := base
		other.ProgramStatuses = []ProgramStatus{{Title: "a"}}
		v := reflect.ValueOf(&other).Elem().Field(i)
		switch v.Kind() { //nolint:exhaustive // State only uses these kinds.
		case reflect.String:
			v.SetString("x")
		case reflect.Bool:
			v.SetBool(true)
		case reflect.Int:
			v.SetInt(1)
		case reflect.Uint64:
			v.SetUint(1)
		case reflect.Struct:
			switch f := v.Addr().Interface().(type) {
			case *color.NRGBA:
				f.R = 1
			case *Cursor:
				f.X = 1
			case *Progress:
				f.Reported = true
			default:
				t.Fatalf("field %s has type %T the test does not set", typ.Field(i).Name, f)
			}
		case reflect.Slice:
			v.Set(reflect.ValueOf([]ProgramStatus{{Title: "b"}}))
		default:
			t.Fatalf("field %s has kind %v the test does not set", typ.Field(i).Name, v.Kind())
		}
		if base.Equal(other) {
			t.Errorf("Equal ignores %s", typ.Field(i).Name)
		}
	}
}

// TestStateSameCommit checks what SameCommit compares: a write that only
// changes progress, the program status records, or whether a report arrived
// when that hides progress is a new commit, while one that changes nothing
// drawn, or only what a harness sets around the program, is not.
func TestStateSameCommit(t *testing.T) {
	t.Parallel()

	a := State{Generation: 3, Title: "t", Progress: Progress{Reported: true, State: ProgressSet, Percent: 10}}
	if !a.SameCommit(a) {
		t.Fatal("a state is not the same commit as itself")
	}
	for name, change := range map[string]func(*State){
		"generation": func(s *State) { s.Generation++ },
		"title":      func(s *State) { s.Title = "u" },
		"alt screen": func(s *State) { s.AltScreen = true },
		"cursor":     func(s *State) { s.Cursor.X = 1 },
		"progress":   func(s *State) { s.Progress.Percent = 20 },
		"status":     func(s *State) { s.ProgramStatusGeneration = 1 },
		"reported":   func(s *State) { s.ProgramStatusReported = true },
	} {
		b := a
		change(&b)
		if a.SameCommit(b) {
			t.Errorf("states that differ only in %s are the same commit", name)
		}
	}
	b := a
	b.Focused = true
	b.Foreground = color.NRGBA{R: 1, A: 0xff}
	b.ScrollbackLen = 5
	if !a.SameCommit(b) {
		t.Error("focus, colors, and scrollback made a new commit")
	}

	quiet := State{Generation: 3}
	cleared := quiet
	cleared.ProgramStatusReported = true
	if !quiet.SameCommit(cleared) {
		t.Error("a bare clear with no records and no progress is a new commit")
	}
}

// TestStateSameLook checks that SameLook is SameCommit, with focus and colors
// compared too.
func TestStateSameLook(t *testing.T) {
	t.Parallel()

	a := State{Generation: 3, Title: "t", Focused: true}
	if !a.SameLook(a) {
		t.Fatal("a state does not look the same as itself")
	}
	for name, change := range map[string]func(*State){
		"generation":       func(s *State) { s.Generation++ },
		"focus":            func(s *State) { s.Focused = false },
		"foreground":       func(s *State) { s.Foreground = color.NRGBA{R: 1, A: 0xff} },
		"background":       func(s *State) { s.Background = color.NRGBA{G: 1, A: 0xff} },
		"cursor color":     func(s *State) { s.CursorColor = color.NRGBA{B: 1, A: 0xff} },
		"cursor color set": func(s *State) { s.CursorColorSet = true },
	} {
		b := a
		change(&b)
		if a.SameLook(b) {
			t.Errorf("states that differ only in %s look the same", name)
		}
	}
	b := a
	b.ScrollbackLen = 5
	if !a.SameLook(b) {
		t.Error("scrollback changed the look")
	}
}

func TestStateShownProgress(t *testing.T) {
	t.Parallel()

	set := Progress{Reported: true, State: ProgressSet, Percent: 40}
	if got := (State{Progress: set}).ShownProgress(); got != set {
		t.Errorf("ShownProgress() = %+v, want %+v", got, set)
	}
	if got := (State{Progress: Progress{Reported: true, Percent: 40}}).ShownProgress(); got != (Progress{}) {
		t.Errorf("removed progress shows %+v", got)
	}
	if got := (State{Progress: set, ProgramStatusReported: true}).ShownProgress(); got != (Progress{}) {
		t.Errorf("progress after an OSC 7501 report shows %+v", got)
	}
}

func TestStatusStrings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		got  interface{ String() string }
		want string
	}{
		{ProgressRemove, "remove"},
		{ProgressIndeterminate, "indeterminate"},
		{ProgressState(9), "unknown"},
		{ProgramIdle, "idle"},
		{ProgramWorking, "working"},
		{ProgramError, "error"},
		{ProgramState(9), "unknown"},
		{ProgramKindNone, ""},
		{ProgramKindPermission, "permission"},
		{ProgramKind(9), "unknown"},
	} {
		if got := tc.got.String(); got != tc.want {
			t.Errorf("%T(%d).String() = %q, want %q", tc.got, tc.got, got, tc.want)
		}
	}
}
