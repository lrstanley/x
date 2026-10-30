// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"context"
	"image/color"
	"reflect"
	"testing"
	"time"
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
			case *Bell:
				f.Count = 1
			case *ClipboardWrite:
				f.Count = 1
			case *Prompt:
				f.Kind = PromptStart
			default:
				t.Fatalf("field %s has type %T the test does not set", typ.Field(i).Name, f)
			}
		case reflect.Pointer:
			switch v.Addr().Interface().(type) {
			case **Palette:
				var p Palette
				p.Set(1, color.NRGBA{R: 1, A: 0xff})
				v.Set(reflect.ValueOf(&p))
			case **Glyphs:
				v.Set(reflect.ValueOf(&Glyphs{}))
			default:
				t.Fatalf("field %s has a pointer type the test does not set", typ.Field(i).Name)
			}
		case reflect.Slice:
			switch v.Addr().Interface().(type) {
			case *[]ProgramStatus:
				v.Set(reflect.ValueOf([]ProgramStatus{{Title: "b"}}))
			case *[]Notification:
				v.Set(reflect.ValueOf([]Notification{{Title: "b"}}))
			case *[]PromptMark:
				v.Set(reflect.ValueOf([]PromptMark{{Row: 1}}))
			default:
				t.Fatalf("field %s has a slice type the test does not set", typ.Field(i).Name)
			}
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
		"pwd":        func(s *State) { s.WorkingDirectory = "/tmp" },
		"bell":       func(s *State) { s.Bell.Count++ },
		"clipboard":  func(s *State) { s.Clipboard.Count++ },
		"prompt":     func(s *State) { s.Prompt.At = time.Second },
		"notification": func(s *State) {
			s.Notifications = append(s.Notifications, Notification{Title: "n"})
		},
		"prompt marks": func(s *State) { s.PromptMarks = []PromptMark{{Row: 1}} },
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

// TestStateSameLook checks that SameLook is SameCommit, with focus, colors,
// and registered glyphs compared too.
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
		"palette":          func(s *State) { s.Palette = new(Palette) },
		"glyphs":           func(s *State) { s.Glyphs = new(Glyphs) },
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
	a.Palette, b.Palette = new(Palette), new(Palette)
	if !a.SameLook(b) {
		t.Error("palettes with the same colors changed the look")
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

// TestStateEventTimes checks that program events are stamped with the
// terminal's clock, that a state keeps the notifications it was taken with,
// and that a full reset clears them.
func TestStateEventTimes(t *testing.T) {
	t.Parallel()

	now := time.Duration(0)
	term, err := testRuntime.Open(context.Background(), 20, 2, WithClock(func() time.Duration { return now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	write := func(at time.Duration, s string) State {
		t.Helper()
		now = at
		if _, werr := term.Write([]byte(s)); werr != nil {
			t.Fatal(werr)
		}
		return term.State()
	}

	st := write(time.Second, "\a")
	if st.Bell != (Bell{Count: 1, At: time.Second}) {
		t.Errorf("Bell = %+v after one BEL at 1s", st.Bell)
	}
	before := write(2*time.Second, "\x1b]9;hello\x1b\\")
	if len(before.Notifications) != 1 || before.Notifications[0] != (Notification{At: 2 * time.Second, Body: "hello"}) {
		t.Errorf("Notifications = %+v", before.Notifications)
	}
	st = write(3*time.Second, "\x1b]777;notify;t;b\x1b\\\x1b]52;c;aGk=\x1b\\\x1b]133;A\x1b\\\x1b]7;file:///tmp\x1b\\")
	if len(before.Notifications) != 1 {
		t.Errorf("an earlier state's notifications grew to %+v", before.Notifications)
	}
	if want := (Notification{At: 3 * time.Second, Title: "t", Body: "b"}); len(st.Notifications) != 2 || st.Notifications[1] != want {
		t.Errorf("Notifications = %+v, want a second %+v", st.Notifications, want)
	}
	if want := (ClipboardWrite{Count: 1, Text: "hi", At: 3 * time.Second}); st.Clipboard != want {
		t.Errorf("Clipboard = %+v, want %+v", st.Clipboard, want)
	}
	if st.Prompt.Kind != PromptStart || st.Prompt.At != 3*time.Second {
		t.Errorf("Prompt = %+v, want start at 3s", st.Prompt)
	}
	if st.WorkingDirectory == "" {
		t.Error("WorkingDirectory is empty after OSC 7")
	}
	if st.SameCommit(before) {
		t.Error("program events did not make a new commit")
	}

	st = write(4*time.Second, "\x1bc")
	if st.Bell != (Bell{}) || st.Notifications != nil || st.Clipboard != (ClipboardWrite{}) || st.Prompt != (Prompt{}) {
		t.Errorf("RIS left events: %+v", st)
	}
}

// TestStatePalette checks that State reports the indexed colors the program
// set with OSC 4 until OSC 104 or a full reset clears them, sharing one
// Palette while they stay the same.
func TestStatePalette(t *testing.T) {
	t.Parallel()

	term := openTest(t, 8, 2)
	if p := term.State().Palette; p != nil {
		t.Fatalf("new terminal palette = %v, want nil", p)
	}
	// Setting an index to its default color, as the query replies with it,
	// is not a change.
	reply, err := term.WriteReplies([]byte("\x1b]4;1;?\x07"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(reply, []byte("\x1b]4;1;rgb:")) {
		t.Fatalf("OSC 4 query reply = %q", reply)
	}
	if _, err = term.Write(reply); err != nil {
		t.Fatal(err)
	}
	if p := term.State().Palette; p != nil {
		t.Fatalf("palette after setting a default color = %v, want nil", p)
	}
	if _, err = term.Write([]byte("\x1b]4;1;#010203;200;rgb:aa/bb/cc\x07")); err != nil {
		t.Fatal(err)
	}
	p := term.State().Palette
	want := map[uint8]color.NRGBA{
		1:   {R: 1, G: 2, B: 3, A: 0xff},
		200: {R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff},
	}
	for i := range 256 {
		c, ok := p.Color(uint8(i))
		if w, set := want[uint8(i)]; ok != set || c != w {
			t.Errorf("Palette.Color(%d) = %v, %v, want %v, %v", i, c, ok, w, set)
		}
	}
	if _, err = term.Write([]byte("text")); err != nil {
		t.Fatal(err)
	}
	if again := term.State().Palette; again != p {
		t.Error("output that changed no color replaced the palette")
	}
	if _, err = term.Write([]byte("\x1b]104;1\x07")); err != nil {
		t.Fatal(err)
	}
	if c, ok := term.State().Palette.Color(1); ok {
		t.Errorf("OSC 104;1 left color 1 = %v", c)
	}
	if _, err = term.Write([]byte("\x1b]104\x07")); err != nil {
		t.Fatal(err)
	}
	if p = term.State().Palette; p != nil {
		t.Errorf("OSC 104 left palette %v, want nil", p)
	}
	if _, err = term.Write([]byte("\x1b]4;1;#010203\x07")); err != nil {
		t.Fatal(err)
	}
	if term.State().Palette == nil {
		t.Fatal("OSC 4 set no color")
	}
	if err = term.Reset(); err != nil {
		t.Fatal(err)
	}
	if p = term.State().Palette; p != nil {
		t.Errorf("full reset left palette %v, want nil", p)
	}
}

// TestStateReverseColors checks that State reports DECSCNM, which looks
// different but changes no cell, until the program or a full reset clears
// it.
func TestStateReverseColors(t *testing.T) {
	t.Parallel()

	term := openTest(t, 8, 2)
	writeTerm(t, term, "\x1b[31mA")
	before := term.State()
	cell := *term.Screen().CellAt(0, 0)
	if before.ReverseColors {
		t.Fatal("new terminal has reverse colors")
	}
	writeTerm(t, term, "\x1b[?5h")
	st := term.State()
	if !st.ReverseColors {
		t.Fatal("mode 5 did not set ReverseColors")
	}
	if got := *term.Screen().CellAt(0, 0); got.Style.Attrs != cell.Style.Attrs || got.Style.Fg != cell.Style.Fg {
		t.Errorf("mode 5 changed the cell to %+v, want %+v", got, cell)
	}
	if st.SameLook(before) {
		t.Error("mode 5 looks the same")
	}
	writeTerm(t, term, "\x1b[?5l")
	if term.State().ReverseColors {
		t.Error("mode 5 reset left ReverseColors")
	}
	writeTerm(t, term, "\x1b[?5h")
	if err := term.Reset(); err != nil {
		t.Fatal(err)
	}
	if term.State().ReverseColors {
		t.Error("full reset left ReverseColors")
	}
}
