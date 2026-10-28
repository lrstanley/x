// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"slices"
	"testing"

	"github.com/lrstanley/x/term/reel/vt"
)

// keyboardModes are what a program can ask for that changes how keys are
// encoded.
var keyboardModes = []struct {
	name string
	seq  string
}{
	{"legacy", ""},
	{"kitty disambiguate", "\x1b[>1u"},
	{"kitty all keys", "\x1b[>31u"},
	{"modify other keys", "\x1b[>4;2m"},
}

// lastStrokes runs send and returns the strokes of the one input it recorded.
func lastStrokes(t *testing.T, s *Session, send func() error) []vt.Stroke {
	t.Helper()
	before := s.Timeline().Len()
	if err := send(); err != nil {
		t.Fatal(err)
	}
	inputs := slices.Collect(s.Timeline().Inputs())
	if s.Timeline().Len() != before+1 || len(inputs) == 0 {
		t.Fatalf("send recorded %d events, want 1 input", s.Timeline().Len()-before)
	}
	return vt.DecodeInput(inputs[len(inputs)-1].Data)
}

func TestStrokesKeys(t *testing.T) {
	t.Parallel()
	// Each key name decodes to the name a keys overlay shows, which is the
	// name itself unless noted.
	keys := map[string]string{
		"enter":       "",
		"tab":         "",
		"shift+tab":   "",
		"esc":         "",
		"escape":      "esc",
		"backspace":   "",
		"delete":      "",
		"insert":      "",
		"up":          "",
		"down":        "",
		"left":        "",
		"right":       "",
		"home":        "",
		"end":         "",
		"pgup":        "",
		"pagedown":    "pgdown",
		"f1":          "",
		"f5":          "",
		"f12":         "",
		"ctrl+a":      "",
		"ctrl+c":      "",
		"ctrl+z":      "",
		"alt+x":       "",
		"alt+enter":   "",
		"ctrl+up":     "",
		"shift+up":    "",
		"super+up":    "",
		"alt+shift+a": "",
		"alt+A":       "alt+shift+a",
		"alt+/":       "",
		"alt+é":       "",
		"ctrl+]":      "",
	}
	for _, mode := range keyboardModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 20, 4)
			write(t, s, mode.seq)
			for name, want := range keys {
				if want == "" {
					want = name
				}
				got := lastStrokes(t, s, func() error { return s.Key(name) })
				if len(got) != 1 || got[0].Kind != vt.StrokeKey || got[0].Key.String() != want {
					t.Errorf("Key(%q) decodes to %+v, want the key %q", name, got, want)
				}
			}
		})
	}
}

func TestStrokesText(t *testing.T) {
	t.Parallel()
	for _, mode := range keyboardModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 20, 4)
			write(t, s, mode.seq)
			text := func(s string) []vt.Stroke { return []vt.Stroke{{Kind: vt.StrokeText, Text: s}} }
			tests := []struct {
				send func() error
				want []vt.Stroke
			}{
				{func() error { return s.Type("Hello, world 42!") }, text("Hello, world 42!")},
				{func() error { return s.Type("é") }, text("é")},
				{func() error { return s.Key("a") }, text("a")},
				{func() error { return s.Key("shift+a") }, text("A")},
				{func() error { return s.Key("space") }, text(" ")},
				{func() error { return s.Key("?") }, text("?")},
			}
			for i, tt := range tests {
				if got := lastStrokes(t, s, tt.send); !slices.Equal(got, tt.want) {
					t.Errorf("case %d decodes to %+v, want %+v", i, got, tt.want)
				}
			}
		})
	}
}

func TestStrokesMixed(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4)
	got := lastStrokes(t, s, func() error { return s.Type("ab\tc") })
	want := []vt.Stroke{
		{Kind: vt.StrokeText, Text: "ab"},
		{Kind: vt.StrokeKey, Key: vt.Key{Code: "TAB"}},
		{Kind: vt.StrokeText, Text: "c"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("strokes = %+v, want %+v", got, want)
	}
}

func TestStrokesPaste(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4)
	// Without bracketed paste, a paste is typed text, with each newline
	// sent as enter.
	got := lastStrokes(t, s, func() error { return s.Paste("one\ntwo") })
	want := []vt.Stroke{
		{Kind: vt.StrokeText, Text: "one"},
		{Kind: vt.StrokeKey, Key: vt.Key{Code: "ENTER"}},
		{Kind: vt.StrokeText, Text: "two"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unbracketed paste = %+v, want %+v", got, want)
	}

	write(t, s, "\x1b[?2004h")
	got = lastStrokes(t, s, func() error { return s.Paste("one\ntwo\x1b[201~") })
	// The terminal strips the escape that would end the paste early.
	want = []vt.Stroke{{Kind: vt.StrokePaste, Text: "one\ntwo [201~"}}
	if !slices.Equal(got, want) {
		t.Fatalf("bracketed paste = %+v, want %+v", got, want)
	}

	// A paste cut off before its end marker keeps what arrived.
	in := Input{Data: []byte("\x1b[200~partial")}
	if got = vt.DecodeInput(in.Data); !slices.Equal(got, []vt.Stroke{{Kind: vt.StrokePaste, Text: "partial"}}) {
		t.Fatalf("unterminated paste = %+v", got)
	}
}

func TestStrokesMouse(t *testing.T) {
	t.Parallel()
	modes := []struct {
		name string
		seq  string
	}{
		{"x10", "\x1b[?1003h"},
		{"sgr", "\x1b[?1003h\x1b[?1006h"},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 20, 4)
			write(t, s, mode.seq)
			tests := []struct {
				action vt.MouseAction
				button vt.MouseButton
				want   vt.MouseEvent
			}{
				{vt.MousePress, vt.MouseLeft, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 3, Y: 2}},
				{vt.MousePress, vt.MouseRight, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseRight, X: 3, Y: 2}},
				{vt.MousePress, vt.MouseMiddle, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseMiddle, X: 3, Y: 2}},
				{vt.MouseMotion, vt.MouseLeft, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 3, Y: 2}},
				{vt.MousePress, vt.MouseWheelUp, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseWheelUp, X: 3, Y: 2}},
			}
			release := vt.MouseEvent{Action: vt.MouseRelease, Button: vt.MouseLeft, X: 3, Y: 2}
			if mode.name == "x10" {
				// X10 does not say which button was released.
				release.Button = vt.MouseUnknown
			}
			for _, tt := range tests {
				got := lastStrokes(t, s, func() error { return s.Mouse(tt.action, tt.button, 3, 2) })
				if want := []vt.Stroke{{Kind: vt.StrokeMouse, Mouse: tt.want}}; !slices.Equal(got, want) {
					t.Errorf("Mouse(%v, %v) decodes to %+v, want %+v", tt.action, tt.button, got, want)
				}
			}
			got := lastStrokes(t, s, func() error { return s.Mouse(vt.MouseRelease, vt.MouseLeft, 3, 2) })
			if want := []vt.Stroke{{Kind: vt.StrokeMouse, Mouse: release}}; !slices.Equal(got, want) {
				t.Errorf("release decodes to %+v, want %+v", got, want)
			}
		})
	}
}

func TestStrokesSkipsOtherInput(t *testing.T) {
	t.Parallel()
	in := Input{Data: []byte("\x1b[I" + "a" + "\x1b[O")}
	if got := vt.DecodeInput(in.Data); !slices.Equal(got, []vt.Stroke{{Kind: vt.StrokeText, Text: "a"}}) {
		t.Fatalf("strokes = %+v, want just the text", got)
	}
	if got := vt.DecodeInput(nil); got != nil {
		t.Fatalf("empty input strokes = %+v", got)
	}
}
