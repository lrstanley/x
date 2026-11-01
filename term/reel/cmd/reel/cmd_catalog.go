// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	tint "github.com/lrstanley/bubbletint/v2"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/themes"
	"github.com/lrstanley/x/term/reel/render/fonts"
)

// FontsCmd lists font families.
type FontsCmd struct{}

// Run prints the families, one per line.
func (FontsCmd) Run() error {
	for _, name := range fonts.Families() {
		if _, err := fmt.Fprintln(os.Stdout, name); err != nil {
			return err
		}
	}
	return nil
}

// ThemesCmd lists the named themes.
type ThemesCmd struct {
	Search  string `arg:"" optional:"" placeholder:"TEXT" help:"Only themes whose name holds this text, in any case."`
	Dark    bool   `name:"dark" xor:"tone" help:"Only dark themes."`
	Light   bool   `name:"light" xor:"tone" help:"Only light themes."`
	Hue     string `name:"hue" placeholder:"HUE" help:"Only themes of this dominant hue, such as blue or green."`
	Tone    string `name:"tone" placeholder:"TONE" help:"Only themes of this tone, such as muted or retro."`
	Preview bool   `name:"preview" short:"p" help:"Show each theme's colors, in true color."`
	JSON    bool   `name:"json" help:"Print JSON."`
}

// Run prints the matching themes.
func (c *ThemesCmd) Run() error {
	search := strings.ToLower(c.Search)
	matches := slices.DeleteFunc(slices.Clone(tint.DefaultTints()), func(t *tint.Tint) bool {
		named := search == "" || strings.Contains(strings.ToLower(t.DisplayName), search) || strings.Contains(t.ID, search)
		return !named || c.Dark && !t.Dark || c.Light && t.Dark ||
			c.Hue != "" && string(t.Hue) != c.Hue || c.Tone != "" && string(t.Tone) != c.Tone
	})
	slices.SortFunc(matches, func(a, b *tint.Tint) int { return strings.Compare(a.ID, b.ID) })
	if c.JSON {
		return json.MarshalWrite(os.Stdout, matches, jsontext.Multiline(true))
	}
	headers := []string{"NAME", "ID", "MODE", "HUE", "TONE"}
	if c.Preview {
		headers = append(headers, "PREVIEW")
	}
	rows := make([][]string, len(matches))
	for i, t := range matches {
		mode := "light"
		if t.Dark {
			mode = "dark"
		}
		rows[i] = []string{t.DisplayName, t.ID, mode, string(t.Hue), string(t.Tone)}
		if c.Preview {
			rows[i] = append(rows[i], swatch(t))
		}
	}
	return printTable(headers, rows, "No themes match.", "reel themes")
}

// swatch draws t's foreground and 16 colors on its background.
func swatch(t *tint.Tint) string {
	pal := themes.Palette(t)
	base := ansi.NewStyle()
	if pal.DefaultBackground != nil {
		base = base.BackgroundColor(pal.DefaultBackground)
	}
	var b strings.Builder
	for i := -1; i < 16; i++ {
		c := pal.DefaultForeground
		if i >= 0 {
			c = pal.Indexed[i]
		}
		if c == nil {
			b.WriteString(base.Styled("  "))
			continue
		}
		b.WriteString(base.ForegroundColor(c).Styled("● "))
	}
	return b.String()
}
