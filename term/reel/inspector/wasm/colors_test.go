// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

func TestDefaultColorRoles(t *testing.T) {
	x := &inspector{}
	screen := vt.NewBuffer(1, 1)
	screen.SetCell(0, 0, &vt.Cell{Content: "x", Width: 1})
	uses := x.colors(screen, vt.State{})
	if len(uses) != 2 {
		t.Fatalf("colors = %+v, want two default roles", uses)
	}
	for _, use := range uses {
		switch use.Spec {
		case "default-fg":
			if use.Hex != "#ffffff" || use.Fg != 1 || use.Bg != 0 {
				t.Errorf("foreground = %+v", use)
			}
		case "default-bg":
			if use.Hex != "#000000" || use.Bg != 1 || use.Fg != 0 {
				t.Errorf("background = %+v", use)
			}
		default:
			t.Errorf("unexpected color = %+v", use)
		}
	}
}

func TestEffectivePalette(t *testing.T) {
	pal, err := render.PaletteOf(render.WithPalette(render.Palette{Indexed: map[int]color.Color{1: ansi.Blue}}))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 16 {
		got := hex(render.ResolveColor(vt.State{}, pal, vt.Indexed(i), false))
		if len(got) != 7 {
			t.Errorf("palette[%d] = %q", i, got)
		}
	}
	if got := hex(render.ResolveColor(vt.State{}, pal, vt.Indexed(1), false)); got != hex(ansi.Blue) {
		t.Errorf("palette override = %s, want %s", got, hex(ansi.Blue))
	}
}
