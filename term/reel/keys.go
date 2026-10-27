// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel/vt"
)

// keyCodes maps the named keys [Session.Key] accepts to Ghostty key codes.
var keyCodes = map[string]string{
	"enter":     "ENTER",
	"esc":       "ESCAPE",
	"escape":    "ESCAPE",
	"tab":       "TAB",
	"backspace": "BACKSPACE",
	"delete":    "DELETE",
	"insert":    "INSERT",
	"up":        "ARROW_UP",
	"down":      "ARROW_DOWN",
	"left":      "ARROW_LEFT",
	"right":     "ARROW_RIGHT",
	"home":      "HOME",
	"end":       "END",
	"pgup":      "PAGE_UP",
	"pageup":    "PAGE_UP",
	"pgdown":    "PAGE_DOWN",
	"pagedown":  "PAGE_DOWN",
	"space":     "SPACE",
	"f1":        "F1",
	"f2":        "F2",
	"f3":        "F3",
	"f4":        "F4",
	"f5":        "F5",
	"f6":        "F6",
	"f7":        "F7",
	"f8":        "F8",
	"f9":        "F9",
	"f10":       "F10",
	"f11":       "F11",
	"f12":       "F12",
}

// keyMods maps modifier names to their bits.
var keyMods = map[string]vt.Mods{
	"ctrl":  vt.ModCtrl,
	"alt":   vt.ModAlt,
	"shift": vt.ModShift,
	"super": vt.ModSuper,
}

// parseKey turns a key name such as "enter", "ctrl+c", "shift+tab", or "a"
// into a key event. Modifiers come first, joined by "+": ctrl, alt, shift, and
// super. The key is a name in [keyCodes] or a single character.
func parseKey(name string) (vt.Key, error) {
	var mods vt.Mods
	key := name
	for {
		i := strings.IndexByte(key, '+')
		if i <= 0 || i == len(key)-1 {
			break
		}
		mod, ok := keyMods[key[:i]]
		if !ok {
			break
		}
		mods |= mod
		key = key[i+1:]
	}
	if code, ok := keyCodes[key]; ok {
		ev := vt.Key{Code: code, Mods: mods}
		if code == "SPACE" && mods&^vt.ModShift == 0 {
			ev.Text = " "
		}
		return ev, nil
	}
	r, size := utf8.DecodeRuneInString(key)
	if key == "" || size != len(key) || r == utf8.RuneError || !unicode.IsPrint(r) {
		return vt.Key{}, fmt.Errorf("reel: unknown key %q", name)
	}
	if mods&vt.ModShift != 0 {
		if !unicode.IsLetter(r) {
			return vt.Key{}, fmt.Errorf("reel: key %q: name the shifted character instead", name)
		}
		r = unicode.ToUpper(r)
	}
	ev := vt.RuneKey(r)
	ev.Mods |= mods
	if mods&(vt.ModCtrl|vt.ModSuper) != 0 {
		// ctrl and super combinations type no text; the encoder works out
		// what to send from the key.
		ev.Text = ""
	}
	return ev, nil
}
