// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import "testing"

func TestParseKey(t *testing.T) {
	t.Parallel()
	tests := map[string]Key{
		"a":              {Code: "A", Text: "a"},
		"A":              {Code: "A", Text: "A", Mods: ModShift},
		"shift+a":        {Code: "A", Text: "A", Mods: ModShift},
		"ctrl+a":         {Code: "A", Mods: ModCtrl},
		"ctrl+alt+a":     {Code: "A", Mods: ModCtrl | ModAlt},
		"5":              {Code: "DIGIT_5", Text: "5"},
		"+":              {Code: "EQUAL", Text: "+", Mods: ModShift},
		"ctrl++":         {Code: "EQUAL", Mods: ModCtrl | ModShift},
		"alt+a":          {Code: "A", Text: "a", Mods: ModAlt},
		"é":              {Text: "é"},
		"shift+é":        {Text: "É", Mods: ModShift},
		"ctrl+é":         {Text: "é", Mods: ModCtrl},
		"space":          {Code: "SPACE", Text: " "},
		"ctrl+space":     {Code: "SPACE", Mods: ModCtrl},
		"super+shift+f1": {Code: "F1", Mods: ModSuper | ModShift},
		"escape":         {Code: "ESCAPE"},
		"f25":            {Code: "F25"},
		"leftshift":      {Code: "SHIFT_LEFT"},
	}
	for name, want := range tests {
		got, err := ParseKey(name)
		if err != nil || got != want {
			t.Errorf("ParseKey(%q) = %#v, %v, want %#v", name, got, err, want)
		}
	}
	for _, name := range []string{"", "ctrl+", "hyper+a", "f26", "ab", "\x01", "shift+1"} {
		if _, err := ParseKey(name); err == nil {
			t.Errorf("ParseKey(%q) succeeded", name)
		}
	}
}

func TestKeyString(t *testing.T) {
	t.Parallel()
	// Each name parses to a key named the same, or the name noted.
	names := map[string]string{
		"a":                       "",
		"A":                       "shift+a",
		"ctrl+shift+a":            "",
		"5":                       "",
		"@":                       "",
		"ctrl+@":                  "",
		"alt+!":                   "",
		"ctrl++":                  "",
		"é":                       "",
		"shift+é":                 "",
		"alt+é":                   "",
		"space":                   "",
		"shift+space":             "",
		"ctrl+space":              "",
		"enter":                   "",
		"escape":                  "esc",
		"pagedown":                "pgdown",
		"shift+tab":               "",
		"ctrl+alt+shift+super+up": "",
		"f25":                     "",
		"begin":                   "",
		"leftsuper":               "",
		"mediaplaypause":          "",
	}
	for name, want := range names {
		if want == "" {
			want = name
		}
		key, err := ParseKey(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := key.String(); got != want {
			t.Errorf("ParseKey(%q).String() = %q, want %q", name, got, want)
		}
	}
}
