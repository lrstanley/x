// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// mapKeyToEvent converts a key string to a [uv.KeyPressEvent].
func mapKeyToEvent(key string) uv.KeyPressEvent {
	switch {
	case key == "enter":
		return uv.KeyPressEvent{Code: tea.KeyEnter}
	case key == "esc" || key == "escape":
		return uv.KeyPressEvent{Code: tea.KeyEscape}
	case key == "tab":
		return uv.KeyPressEvent{Code: tea.KeyTab}
	case key == "shift+tab":
		return uv.KeyPressEvent{Code: tea.KeyTab, Mod: tea.ModShift}
	case key == "backspace":
		return uv.KeyPressEvent{Code: tea.KeyBackspace}
	case key == "delete":
		return uv.KeyPressEvent{Code: tea.KeyDelete}
	case key == "up":
		return uv.KeyPressEvent{Code: tea.KeyUp}
	case key == "down":
		return uv.KeyPressEvent{Code: tea.KeyDown}
	case key == "left":
		return uv.KeyPressEvent{Code: tea.KeyLeft}
	case key == "right":
		return uv.KeyPressEvent{Code: tea.KeyRight}
	case key == "home":
		return uv.KeyPressEvent{Code: tea.KeyHome}
	case key == "end":
		return uv.KeyPressEvent{Code: tea.KeyEnd}
	case key == "pgup" || key == "pageup":
		return uv.KeyPressEvent{Code: tea.KeyPgUp}
	case key == "pgdown" || key == "pagedown":
		return uv.KeyPressEvent{Code: tea.KeyPgDown}
	case key == "space":
		return uv.KeyPressEvent{Code: ' ', Text: " "}
	case strings.HasPrefix(key, "ctrl+") && len(key) == 6:
		return uv.KeyPressEvent{Code: rune(key[5]), Mod: tea.ModCtrl}
	default:
		if len(key) == 1 {
			r := rune(key[0])
			return uv.KeyPressEvent{Code: r, Text: key}
		}
		return uv.KeyPressEvent{Text: key}
	}
}
