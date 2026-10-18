// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"strings"

	"github.com/lrstanley/x/term/vt"
)

func mapKey(key string) vt.Key {
	switch key {
	case "enter":
		return vt.Key{Code: "ENTER"}
	case "esc", "escape":
		return vt.Key{Code: "ESCAPE"}
	case "tab":
		return vt.Key{Code: "TAB"}
	case "shift+tab":
		return vt.Key{Code: "TAB", Mods: vt.ModShift}
	case "backspace":
		return vt.Key{Code: "BACKSPACE"}
	case "delete":
		return vt.Key{Code: "DELETE"}
	case "up":
		return vt.Key{Code: "ARROW_UP"}
	case "down":
		return vt.Key{Code: "ARROW_DOWN"}
	case "left":
		return vt.Key{Code: "ARROW_LEFT"}
	case "right":
		return vt.Key{Code: "ARROW_RIGHT"}
	case "home":
		return vt.Key{Code: "HOME"}
	case "end":
		return vt.Key{Code: "END"}
	case "pgup", "pageup":
		return vt.Key{Code: "PAGE_UP"}
	case "pgdown", "pagedown":
		return vt.Key{Code: "PAGE_DOWN"}
	case "space":
		return vt.Key{Code: "SPACE", Text: " "}
	default:
		if strings.HasPrefix(key, "ctrl+") && len(key) == 6 {
			return vt.Key{Code: letterCode(rune(key[5])), Mods: vt.ModCtrl}
		}
		if len(key) == 1 {
			r := rune(key[0])
			return vt.Key{Code: letterCode(r), Text: key}
		}
		return vt.Key{Text: key}
	}
}

func letterCode(r rune) string {
	switch {
	case r >= 'a' && r <= 'z':
		return string(r - 'a' + 'A')
	case r >= 'A' && r <= 'Z':
		return string(r)
	case r >= '0' && r <= '9':
		return "DIGIT_" + string(r)
	default:
		return ""
	}
}
