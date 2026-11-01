// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package themes

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	mocha := Lookup("Catppuccin Mocha")
	if mocha == nil {
		t.Fatal("no Catppuccin Mocha")
	}
	file := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(file, []byte(`{"background": "#010203"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Catppuccin Mocha", "catppuccin_mocha", "CATPPUCCIN-MOCHA"} {
		pal, err := Resolve(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pal.DefaultBackground != color.Color(mocha.Bg) || len(pal.Indexed) != 16 {
			t.Errorf("%s: palette = %+v, want Mocha's", name, pal)
		}
	}
	for _, theme := range []string{file, `{"background": "#010203", "brightPurple": "#123", "name": "x"}`} {
		pal, err := Resolve(theme)
		if err != nil {
			t.Fatalf("%s: %v", theme, err)
		}
		if pal.DefaultBackground != (color.RGBA{1, 2, 3, 0xff}) {
			t.Errorf("%s: background = %v", theme, pal.DefaultBackground)
		}
	}
	pal, _ := Resolve(`{"brightPurple": "#123"}`)
	if pal.Indexed[13] != (color.RGBA{0x11, 0x22, 0x33, 0xff}) {
		t.Errorf("bright purple = %v, want index 13", pal.Indexed[13])
	}
	if _, err := Resolve("no such theme"); err == nil || !strings.Contains(err.Error(), "unknown theme") {
		t.Errorf("error = %v, want an unknown theme", err)
	}
}

// TestPaletteNil checks that a tint's missing colors are left unset, not
// set to a nil *tint.Color, which would not be a nil [color.Color].
func TestPaletteNil(t *testing.T) {
	for _, tn := range []string{"Catppuccin Mocha", "Dracula", "3024 Day"} {
		pal := Palette(Lookup(tn))
		for name, c := range map[string]color.Color{"cursor": pal.Cursor, "cursor text": pal.CursorText} {
			if c != nil {
				if r, g, b, a := c.RGBA(); r|g|b|a == 0 && a == 0 {
					t.Errorf("%s: %s is a transparent color, want unset", tn, name)
				}
			}
		}
	}
}
