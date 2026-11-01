// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package themes resolves the themes the CLI and tapes name: a bubbletint
// tint, a file of xterm.js theme JSON, or that JSON inline, as VHS takes it.
package themes

import (
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"

	tint "github.com/lrstanley/bubbletint/v2"
	"github.com/lrstanley/x/term/reel/render"
)

// Resolve returns the palette of a theme: a tint's ID or display name, in
// any case, such as "Catppuccin Mocha" or "catppuccin_mocha", xterm.js
// theme JSON, or a file holding it.
func Resolve(theme string) (render.Palette, error) {
	theme = strings.TrimSpace(theme)
	if strings.HasPrefix(theme, "{") {
		return ParseJSON([]byte(theme))
	}
	if t := Lookup(theme); t != nil {
		return Palette(t), nil
	}
	data, err := os.ReadFile(theme)
	if err == nil {
		return ParseJSON(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return render.Palette{}, fmt.Errorf("theme %q: %w", theme, err)
	}
	return render.Palette{}, fmt.Errorf("unknown theme %q: not a theme name (see \"reel themes\"), a file, or JSON", theme)
}

// Lookup returns the tint whose ID or display name is name, in any case,
// or nil.
func Lookup(name string) *tint.Tint {
	key := normalize(name)
	i := slices.IndexFunc(tint.DefaultTints(), func(t *tint.Tint) bool {
		return normalize(t.ID) == key || normalize(t.DisplayName) == key
	})
	if i < 0 {
		return nil
	}
	return tint.DefaultTints()[i]
}

// normalize folds case and the separators that tint IDs and display names
// differ by, so "Catppuccin Mocha" and "catppuccin_mocha" are one name.
func normalize(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '_', '-':
			return -1
		}
		return r
	}, strings.ToLower(name))
}

// Palette returns t's colors as a palette.
func Palette(t *tint.Tint) render.Palette {
	pal := render.Palette{Indexed: map[int]color.Color{}}
	ansi := []*tint.Color{
		t.Black, t.Red, t.Green, t.Yellow, t.Blue, t.Purple, t.Cyan, t.White,
		t.BrightBlack, t.BrightRed, t.BrightGreen, t.BrightYellow,
		t.BrightBlue, t.BrightPurple, t.BrightCyan, t.BrightWhite,
	}
	for i, c := range ansi {
		if c != nil {
			pal.Indexed[i] = c
		}
	}
	// A nil *tint.Color in a color.Color field would not be a nil interface.
	set := func(dst *color.Color, c *tint.Color) {
		if c != nil {
			*dst = c
		}
	}
	set(&pal.DefaultForeground, t.Fg)
	set(&pal.DefaultBackground, t.Bg)
	set(&pal.Cursor, t.Cursor)
	set(&pal.CursorText, t.CursorText)
	return pal
}

// ansiNames are the 8 ANSI color names, in index order; "bright" ones are
// 8 higher.
var ansiNames = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// ParseJSON parses xterm.js theme JSON, as VHS's Set Theme takes it. It
// also takes the Windows Terminal names some VHS tapes use ("purple",
// "cursorColor"), and ignores keys that are not colors reel draws.
func ParseJSON(data []byte) (render.Palette, error) {
	var colors map[string]any
	if err := json.Unmarshal(data, &colors); err != nil {
		return render.Palette{}, fmt.Errorf("invalid theme JSON: %w", err)
	}
	pal := render.Palette{Indexed: map[int]color.Color{}}
	for key, v := range colors {
		set := colorSetter(&pal, key)
		if set == nil {
			continue
		}
		str, ok := v.(string)
		if !ok {
			return render.Palette{}, fmt.Errorf("invalid theme color %s: %v is not a string", key, v)
		}
		c, err := ParseColor(str)
		if err != nil {
			return render.Palette{}, fmt.Errorf("invalid theme color %s: %w", key, err)
		}
		set(c)
	}
	return pal, nil
}

// colorSetter returns what sets the color the theme key names in pal, or
// nil for a key that is not a color reel draws.
func colorSetter(pal *render.Palette, key string) func(color.Color) {
	switch key {
	case "background":
		return func(c color.Color) { pal.DefaultBackground = c }
	case "foreground":
		return func(c color.Color) { pal.DefaultForeground = c }
	case "cursor", "cursorColor":
		return func(c color.Color) { pal.Cursor = c }
	case "cursorAccent":
		return func(c color.Color) { pal.CursorText = c }
	}
	name, bright := strings.CutPrefix(key, "bright")
	name = strings.ToLower(name)
	if name == "purple" {
		name = "magenta"
	}
	i := slices.Index(ansiNames, name)
	if i < 0 {
		return nil
	}
	if bright {
		i += 8
	}
	return func(c color.Color) { pal.Indexed[i] = c }
}

// ParseColor parses a "#rgb" or "#rrggbb" color.
func ParseColor(s string) (color.Color, error) {
	h, ok := strings.CutPrefix(s, "#")
	if ok && len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	b, err := hex.DecodeString(h)
	if !ok || err != nil || len(b) != 3 {
		return nil, fmt.Errorf("%q is not a #rrggbb color", s)
	}
	return color.RGBA{R: b[0], G: b[1], B: b[2], A: 0xff}, nil
}
