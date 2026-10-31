// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// colorUse is one color drawn on the screen, and how many cells use it.
type colorUse struct {
	// Spec is the color as the program set it: "default-fg" or "default-bg", an index such as
	// "1" or "208", or "#rrggbb".
	Spec string `json:"spec"`
	Hex  string `json:"hex"`
	Fg   int    `json:"fg"`
	Bg   int    `json:"bg"`
	Ul   int    `json:"ul"`
}

// colors counts the colors the screen's cells use, most used first.
func (x *inspector) colors(scr *vt.Buffer, st vt.State) []colorUse {
	uses := map[string]*colorUse{}
	use := func(c color.Color, background bool) *colorUse {
		raw, _ := (reel.ScreenColor{Color: c}).MarshalText()
		spec := string(raw)
		if spec == "" {
			spec = "default-fg"
			if background {
				spec = "default-bg"
			}
		}
		resolved := render.ResolveColor(st, x.palette, c, background)
		u := uses[spec]
		if u == nil {
			u = &colorUse{Spec: spec, Hex: hex(resolved)}
			uses[spec] = u
		}
		return u
	}
	b := scr.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for xx := b.Min.X; xx < b.Max.X; xx++ {
			c := scr.CellAt(xx, y)
			if c == nil {
				continue
			}
			if c.Content != "" && c.Content != " " {
				use(c.Style.Fg, false).Fg++
			}
			use(c.Style.Bg, true).Bg++
			if c.Style.Underline != 0 && c.Style.UnderlineColor != nil {
				use(c.Style.UnderlineColor, false).Ul++
			}
		}
	}
	out := make([]colorUse, 0, len(uses))
	for _, u := range uses {
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b colorUse) int {
		return cmp.Or(cmp.Compare(b.Fg+b.Bg+b.Ul, a.Fg+a.Bg+a.Ul), strings.Compare(a.Spec, b.Spec))
	})
	return out
}

func hex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
