// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// keyNames maps the Ghostty keys that are named rather than shown by the
// character they type to their names.
var keyNames = func() map[string]string {
	names := map[string]string{
		"ENTER":                "enter",
		"ESCAPE":               "esc",
		"TAB":                  "tab",
		"BACKSPACE":            "backspace",
		"DELETE":               "delete",
		"INSERT":               "insert",
		"ARROW_UP":             "up",
		"ARROW_DOWN":           "down",
		"ARROW_LEFT":           "left",
		"ARROW_RIGHT":          "right",
		"HOME":                 "home",
		"END":                  "end",
		"PAGE_UP":              "pgup",
		"PAGE_DOWN":            "pgdown",
		"SPACE":                "space",
		"NUMPAD_BEGIN":         "begin",
		"CAPS_LOCK":            "capslock",
		"SCROLL_LOCK":          "scrolllock",
		"NUM_LOCK":             "numlock",
		"PRINT_SCREEN":         "printscreen",
		"PAUSE":                "pause",
		"CONTEXT_MENU":         "menu",
		"MEDIA_PLAY_PAUSE":     "mediaplaypause",
		"MEDIA_STOP":           "mediastop",
		"MEDIA_TRACK_NEXT":     "medianext",
		"MEDIA_TRACK_PREVIOUS": "mediaprev",
		"AUDIO_VOLUME_DOWN":    "lowervol",
		"AUDIO_VOLUME_UP":      "raisevol",
		"AUDIO_VOLUME_MUTE":    "mute",
		"SHIFT_LEFT":           "leftshift",
		"SHIFT_RIGHT":          "rightshift",
		"CONTROL_LEFT":         "leftctrl",
		"CONTROL_RIGHT":        "rightctrl",
		"ALT_LEFT":             "leftalt",
		"ALT_RIGHT":            "rightalt",
		"META_LEFT":            "leftsuper",
		"META_RIGHT":           "rightsuper",
	}
	for i := 1; i <= 25; i++ {
		names["F"+strconv.Itoa(i)] = "f" + strconv.Itoa(i)
	}
	return names
}()

// keyCodes maps the names [ParseKey] accepts for named keys to their Ghostty
// keys: the names in [keyNames], and a few aliases.
var keyCodes = func() map[string]string {
	codes := map[string]string{
		"escape":   "ESCAPE",
		"pageup":   "PAGE_UP",
		"pagedown": "PAGE_DOWN",
	}
	for code, name := range keyNames {
		codes[name] = code
	}
	return codes
}()

// modNames are the modifier names in the order they prefix a key name.
var modNames = []struct {
	name string
	mod  Mods
}{
	{"ctrl", ModCtrl},
	{"alt", ModAlt},
	{"shift", ModShift},
	{"super", ModSuper},
}

// usKeys maps the Ghostty keys that type a character on a US layout to that
// character without and with shift.
var usKeys = func() map[string][2]rune {
	keys := map[string][2]rune{
		"SPACE":         {' ', ' '},
		"MINUS":         {'-', '_'},
		"EQUAL":         {'=', '+'},
		"BRACKET_LEFT":  {'[', '{'},
		"BRACKET_RIGHT": {']', '}'},
		"BACKSLASH":     {'\\', '|'},
		"SEMICOLON":     {';', ':'},
		"QUOTE":         {'\'', '"'},
		"BACKQUOTE":     {'`', '~'},
		"COMMA":         {',', '<'},
		"PERIOD":        {'.', '>'},
		"SLASH":         {'/', '?'},
	}
	for r := 'a'; r <= 'z'; r++ {
		keys[string(unicode.ToUpper(r))] = [2]rune{r, unicode.ToUpper(r)}
	}
	for i, shifted := range ")!@#$%^&*(" {
		keys["DIGIT_"+string(rune('0'+i))] = [2]rune{rune('0' + i), shifted}
	}
	return keys
}()

// asciiKeys holds [RuneKey] for each ASCII character: the key in [usKeys]
// that types it, with shift held for a shifted character.
var asciiKeys = func() (keys [utf8.RuneSelf]Key) {
	const chars = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f" +
		"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f" +
		" !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~\x7f"
	for i := range keys {
		// Slicing a constant shares its bytes, so decoding text does not
		// allocate a string per character.
		keys[i].Text = chars[i : i+1]
	}
	for code, shifted := range usKeys {
		keys[shifted[1]].Code, keys[shifted[1]].Mods = code, ModShift
		keys[shifted[0]].Code, keys[shifted[0]].Mods = code, 0
	}
	return keys
}()

// RuneKey returns the key press that types r on a US layout: the key, with
// shift held for a shifted character, and r as its text. A character no key
// types, such as "é" or a control character, has no code, so the encoder
// sends its text as is.
func RuneKey(r rune) Key {
	if r >= 0 && r < utf8.RuneSelf {
		return asciiKeys[r]
	}
	return Key{Text: string(r)}
}

// runeKey is the press of the key that types r, with mods held. Shift turns
// r into the key's shifted character, or into upper case for a character no
// key on a US layout types.
func runeKey(r rune, mods Mods) Key {
	if mods&ModShift != 0 {
		if chars, ok := usKeys[RuneKey(r).Code]; ok {
			r = chars[1]
		} else {
			r = unicode.ToUpper(r)
		}
	}
	return RuneKey(r).with(mods)
}

// with adds mods to the key. A key with ctrl or super held types no text;
// the encoder works out what to send from the key. A key with no code keeps
// its text, which is all that names it.
func (k Key) with(mods Mods) Key {
	k.Mods |= mods
	if k.Code != "" && k.Mods&(ModCtrl|ModSuper) != 0 {
		k.Text = ""
	}
	return k
}

// ParseKey returns the key press a name such as "enter", "ctrl+c",
// "shift+tab", or "a" stands for. Modifiers come first, joined by "+":
// ctrl, alt, shift, and super. The key is a single character, which is
// pressed as the key that types it on a US layout, or one of these names:
//
//   - enter, esc (escape), tab, backspace, delete, insert, space
//   - up, down, left, right, home, end, pgup (pageup), pgdown (pagedown)
//   - f1 through f25, begin, capslock, scrolllock, numlock, printscreen,
//     pause, menu
//   - mediaplaypause, mediastop, medianext, mediaprev, lowervol, raisevol,
//     mute
//   - leftshift, rightshift, leftctrl, rightctrl, leftalt, rightalt,
//     leftsuper, rightsuper
//
// Shift with a letter types it upper case. A shifted symbol is named by
// itself, so "@" rather than "shift+2". [Key.String] returns the name back.
func ParseKey(name string) (Key, error) {
	var mods Mods
	key := name
	for {
		i := strings.IndexByte(key, '+')
		if i <= 0 || i == len(key)-1 {
			break
		}
		mod, ok := modBit(key[:i])
		if !ok {
			break
		}
		mods |= mod
		key = key[i+1:]
	}
	if code, ok := keyCodes[key]; ok {
		if code == "SPACE" {
			return runeKey(' ', mods), nil
		}
		return Key{Code: code}.with(mods), nil
	}
	r, size := utf8.DecodeRuneInString(key)
	if key == "" || size != len(key) || r == utf8.RuneError || !unicode.IsPrint(r) {
		return Key{}, fmt.Errorf("vt: unknown key %q", name)
	}
	if mods&ModShift != 0 && !unicode.IsLetter(r) {
		return Key{}, fmt.Errorf("vt: key %q: name the shifted character instead", name)
	}
	return runeKey(r, mods), nil
}

// modBit returns the modifier a name in [modNames] stands for.
func modBit(name string) (Mods, bool) {
	for _, m := range modNames {
		if m.name == name {
			return m.mod, true
		}
	}
	return 0, false
}

// IsModifier reports whether the key is a modifier on its own, such as left
// shift, which the kitty keyboard protocol can report.
func (k Key) IsModifier() bool {
	switch k.Code {
	case "SHIFT_LEFT", "SHIFT_RIGHT", "CONTROL_LEFT", "CONTROL_RIGHT",
		"ALT_LEFT", "ALT_RIGHT", "META_LEFT", "META_RIGHT":
		return true
	}
	return false
}

// String returns the key's name, as [ParseKey] accepts it: its modifiers,
// then the key. A letter is lower case, with shift for upper case; another
// character is named by itself, shifted or not; a key with no code is named
// by its text.
func (k Key) String() string {
	mods := k.Mods
	name, ok := keyNames[k.Code]
	if !ok {
		chars, us := usKeys[k.Code]
		switch {
		case k.Code == "":
			name = k.Text
			if mods&ModShift != 0 {
				name = strings.ToLower(name)
			}
		case !us:
			name = strings.ToLower(k.Code)
		case unicode.IsLetter(chars[0]):
			name = RuneKey(chars[0]).Text
		case mods&ModShift != 0:
			name = RuneKey(chars[1]).Text
			mods &^= ModShift
		default:
			name = RuneKey(chars[0]).Text
		}
	}
	if mods == 0 {
		return name
	}
	var sb strings.Builder
	sb.Grow(len("ctrl+alt+shift+super+") + len(name))
	for _, m := range modNames {
		if mods&m.mod != 0 {
			sb.WriteString(m.name)
			sb.WriteByte('+')
		}
	}
	sb.WriteString(name)
	return sb.String()
}
