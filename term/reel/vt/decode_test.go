// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

func TestDecodeEvents(t *testing.T) {
	t.Parallel()
	key := func(code string, mods Mods) Key { return Key{Code: code, Mods: mods} }
	text := runeKey
	mouse := func(action MouseAction, button MouseButton) MouseEvent {
		return MouseEvent{Action: action, Button: button, X: 3, Y: 2}
	}
	tests := []struct {
		name string
		in   string
		want []inputEvent
	}{
		{"text", "aA é", []inputEvent{text('a', 0), text('A', 0), text(' ', 0), Key{Text: "é"}}},
		{"grapheme", "👍🏽", []inputEvent{Key{Text: "👍🏽"}}},
		{"invalid utf-8", "\xff\x80a", []inputEvent{text('a', 0)}},
		{"controls", "\x00\x01\x08\t\n\r\x1d\x1f\x7f", []inputEvent{
			key("SPACE", ModCtrl), key("A", ModCtrl), key("H", ModCtrl), key("TAB", 0),
			key("J", ModCtrl), key("ENTER", 0), text(']', ModCtrl), text('_', ModCtrl), key("BACKSPACE", 0),
		}},
		{"escape", "\x1b", []inputEvent{key("ESCAPE", 0)}},
		{"alt escape", "\x1b\x1b", []inputEvent{key("ESCAPE", ModAlt)}},
		{"alt text", "\x1ba\x1bA\x1b\r\x1b\x7f", []inputEvent{
			text('a', ModAlt), text('A', ModAlt), key("ENTER", ModAlt), key("BACKSPACE", ModAlt),
		}},
		{"alt introducer", "\x1b[", []inputEvent{text('[', ModAlt)}},
		{"alt ss3 introducer", "\x1bO", []inputEvent{text('O', ModAlt)}},
		{"alt csi key", "\x1b\x1b[A", []inputEvent{key("ARROW_UP", ModAlt)}},
		{"csi keys", "\x1b[A\x1b[1;5A\x1b[1;3D\x1b[1;9B\x1b[Z\x1b[H\x1b[F\x1b[E", []inputEvent{
			key("ARROW_UP", 0), key("ARROW_UP", ModCtrl), key("ARROW_LEFT", ModAlt), key("ARROW_DOWN", ModSuper),
			key("TAB", ModShift), key("HOME", 0), key("END", 0), key("NUMPAD_BEGIN", 0),
		}},
		{"tilde keys", "\x1b[3~\x1b[5;2~\x1b[15~\x1b[24~\x1b[34~\x1b[1~\x1b[4~", []inputEvent{
			key("DELETE", 0), key("PAGE_UP", ModShift), key("F5", 0), key("F12", 0), key("F20", 0),
			key("HOME", 0), key("END", 0),
		}},
		{"f3 and cursor reports", "\x1b[R\x1b[12;40R\x1b[1;5R", []inputEvent{key("F3", 0)}},
		{"ss3 keys", "\x1bOA\x1bOP\x1bO5P\x1bOM\x1bOp\x1bOk", []inputEvent{
			key("ARROW_UP", 0), key("F1", 0), key("F1", ModCtrl), key("ENTER", 0), text('0', 0), text('+', 0),
		}},
		{"8-bit introducers", "\x9bB\x8fA", []inputEvent{key("ARROW_DOWN", 0), key("ARROW_UP", 0)}},
		{"urxvt keys", "\x1b[a\x1bOa\x1b[2$\x1b[3^\x1b[5@", []inputEvent{
			key("ARROW_UP", ModShift), key("ARROW_UP", ModCtrl), key("INSERT", ModShift),
			key("DELETE", ModCtrl), key("PAGE_UP", ModCtrl|ModShift),
		}},
		{"modify other keys", "\x1b[27;5;105~\x1b[27;2;65~\x1b[27;5;13~", []inputEvent{
			key("I", ModCtrl), text('A', 0), key("ENTER", ModCtrl),
		}},
		{"kitty keys", "\x1b[99;5u\x1b[97;;97u\x1b[97:65;2;65u\x1b[97;6u\x1b[13u\x1b[127;3u\x1b[233u", []inputEvent{
			key("C", ModCtrl), text('a', 0), text('A', 0), key("A", ModCtrl|ModShift), key("ENTER", 0),
			key("BACKSPACE", ModAlt),
			Key{Text: "é"},
		}},
		{"kitty functional keys", "\x1b[57441;2u\x1b[57399u\x1b[57414u\x1b[57376u\x1b[57427~", []inputEvent{
			key("SHIFT_LEFT", ModShift), text('0', 0), key("ENTER", 0), key("F13", 0), key("NUMPAD_BEGIN", 0),
		}},
		{"kitty events", "\x1b[97;1:3u\x1b[97;1:2u\x1b[1;5:3A\x1b[97;129u", []inputEvent{text('a', 0), text('a', 0)}},
		{"kitty unknown keys", "\x1b[57390u\x1b[57445u", nil},
		{"kitty shifted non-us letter", "\x1b[233;2u", []inputEvent{Key{Text: "É", Mods: ModShift}}},
		{"kitty no-break space", "\x1b[160u", []inputEvent{Key{Text: "\u00a0"}}},
		{"high function keys", "\x1b[42~\x1b[46;5~", []inputEvent{key("F21", 0), key("F25", ModCtrl)}},
		{"urxvt shift then text", "\x1b[2$xy", []inputEvent{key("INSERT", ModShift), text('x', 0), text('y', 0)}},
		{"replacement character", "\ufffd", []inputEvent{Key{Text: "\ufffd"}}},
		{"sgr mouse", "\x1b[<0;4;3M\x1b[<0;4;3m\x1b[<1;4;3M\x1b[<2;4;3M\x1b[<32;4;3M\x1b[<35;4;3M\x1b[<64;4;3M\x1b[<65;4;3m\x1b[<128;4;3M", []inputEvent{
			mouse(MousePress, MouseLeft), mouse(MouseRelease, MouseLeft), mouse(MousePress, MouseMiddle),
			mouse(MousePress, MouseRight), mouse(MouseMotion, MouseLeft), mouse(MouseMotion, MouseUnknown),
			mouse(MousePress, MouseWheelUp), mouse(MousePress, MouseWheelDown), mouse(MousePress, MouseBackward),
		}},
		{"x10 mouse", "\x1b[M $#\x1b[M#$#", []inputEvent{mouse(MousePress, MouseLeft), mouse(MouseRelease, MouseUnknown)}},
		{"x10 mouse cut off", "\x1b[M ", nil},
		{"urxvt mouse", "\x1b[32;4;3M", []inputEvent{mouse(MousePress, MouseLeft)}},
		{"paste", "\x1b[200~hi\nthere\x1b[201~x", []inputEvent{paste("hi\nthere"), text('x', 0)}},
		{"paste cut off", "\x1b[200~part", []inputEvent{paste("part")}},
		{"skipped", "\x1b[I\x1b[O\x1b]11;rgb:0/0/0\a\x1bP>|ghostty\x1b\\\x1b[?62;22c\x1b_Gi=1;OK\x1b\\\x1b[?1u", nil},
		{"many params", "\x1b[" + strings.Repeat(";", 100) + "u\x1b[" + strings.Repeat("9", 100) + "u", nil},
		{"kitty release then text", "\x1b[97;1:3ub", []inputEvent{text('b', 0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := slices.Collect(decodeEvents([]byte(tt.in)))
			if !slices.Equal(got, tt.want) {
				t.Errorf("decodeEvents(%q) =\n\t%#v\nwant\n\t%#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecodeInputInvertsEncodeKey(t *testing.T) {
	t.Parallel()
	modes := map[string]string{
		"legacy":             "",
		"kitty disambiguate": "\x1b[>1u",
		"kitty all keys":     "\x1b[>31u",
		"modify other keys":  "\x1b[>4;2m",
	}
	names := []string{
		"a", "shift+a", "z", "5", "?", "é", "space", "enter", "tab", "shift+tab", "esc", "backspace",
		"delete", "insert", "up", "ctrl+up", "shift+up", "super+up", "home", "end", "pgup", "pgdown",
		"f1", "f5", "f12", "f13", "f21", "ctrl+f25", "ctrl+a", "ctrl+c", "alt+x", "alt+enter", "alt+shift+a", "alt+/", "alt+é",
	}
	for mode, seq := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			term := openTest(t, 8, 2)
			if _, err := term.Write([]byte(seq)); err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				key, err := ParseKey(name)
				if err != nil {
					t.Fatal(err)
				}
				data, err := term.EncodeKey(key)
				if err != nil {
					t.Fatal(err)
				}
				got := slices.Collect(decodeEvents(data))
				if len(got) != 1 || got[0] != key {
					t.Errorf("%q encodes to %q, which decodes to %#v, want %#v", name, data, got, key)
				}
			}
		})
	}
}

func TestDecodeInputInvertsKittyKeys(t *testing.T) {
	t.Parallel()
	// The kitty protocol reporting all keys encodes keys and modifiers
	// unambiguously, so every name the encoder encodes decodes back to its
	// key.
	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("\x1b[>31u")); err != nil {
		t.Fatal(err)
	}
	names := slices.Collect(maps.Values(keyNames))
	for r := '!'; r <= '~'; r++ {
		names = append(names, string(r))
	}
	for _, mods := range []string{"", "ctrl+", "alt+", "shift+", "super+", "ctrl+alt+", "ctrl+shift+"} {
		for _, name := range names {
			key, err := ParseKey(mods + name)
			if err != nil {
				continue // shift with a symbol, which is named by itself.
			}
			data, err := term.EncodeKey(key)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) == 0 {
				continue // A key the encoder has no encoding for, such as mute.
			}
			if got := slices.Collect(decodeEvents(data)); len(got) != 1 || got[0] != key {
				t.Errorf("%q encodes to %q, which decodes to %#v, want %#v", mods+name, data, got, key)
			}
		}
	}
}

func TestDecodeTablesUseGhosttyKeys(t *testing.T) {
	t.Parallel()
	// wasmvt.KeyByName panics on a name Ghostty does not have.
	codes := slices.Collect(maps.Keys(keyNames))
	codes = slices.AppendSeq(codes, maps.Values(tildeKeys))
	codes = slices.AppendSeq(codes, maps.Keys(usKeys))
	for _, table := range []map[byte]Key{csiKeys, ss3Keys} {
		for _, key := range table {
			codes = append(codes, key.Code)
		}
	}
	for _, key := range kittyCodes {
		codes = append(codes, key.Code)
	}
	for _, code := range codes {
		wasmvt.KeyByName(code)
	}
}

func FuzzDecodeInput(f *testing.F) {
	for _, seed := range []string{
		"a\x1b[1;5A\x1bOP\x1b[97:65;2;65u\x1b[<0;4;3M\x1b[M $#\x1b[200~x\x1b[201~",
		"\x1b\x1b[A\x1b[2$\x1b]11;rgb:0/0/0\a\x9bB\x8fA\xff",
		"\x1b[" + strings.Repeat("1;", 40) + "u",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, s := range DecodeInput(data) {
			if s.Kind == 0 {
				t.Fatalf("stroke with no kind: %#v", s)
			}
		}
	})
}
