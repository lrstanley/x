// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package asciicast reads and writes asciicast v2 and v3, the
// newline-delimited JSON that asciinema records and plays, with the header
// fields reel adds to it. It knows the format but not reel's events: an
// event is a time, a code, and a string of data, and only the theme event's
// data has a format here.
package asciicast

import (
	"encoding/hex"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxTime bounds event times, well inside a [time.Duration].
	MaxTime = 365 * 24 * time.Hour
	// MaxSize bounds the terminal size, as asciinema does.
	MaxSize = math.MaxUint16
)

// Header is the first line of a file.
type Header struct {
	// Version is 2 or 3.
	Version int
	// Cols and Rows are the terminal size, each from 1 to [MaxSize].
	Cols, Rows int
	// Term is the terminal type, written as the type of a v3 terminal and
	// the TERM variable of a v2 environment, unless it is empty. A [Reader]
	// leaves it empty.
	Term string
	// Timestamp is when the recording started, to the second. It is zero
	// when the file does not say.
	Timestamp time.Time
	// Title is the recording's title.
	Title string
	// Theme is the colors the terminal starts with, or nil for none.
	Theme *Theme
	// SkipPristine is reel's own "skip_pristine", which other readers
	// ignore.
	SkipPristine bool
	// Scrollback is reel's own "scrollback", which other readers ignore,
	// or nil when the file does not say. A [Reader] rejects a negative one.
	Scrollback *int
}

// Theme is the colors a terminal starts with. Fg and Bg are set. Cursor is
// reel's own, which other readers ignore, and may be nil.
type Theme struct {
	Colors
	// Palette is the 16 ANSI colors, the 8 normal colors and then their
	// bright variants. A [Writer] writes each of them, so none may be nil. A
	// [Reader] leaves nil the colors a file leaves out: all 16, or the
	// bright ones of an 8 color palette.
	Palette [16]color.Color
}

// Colors are a terminal's default foreground and background, and its cursor
// color. A nil color is one left out.
type Colors struct {
	Fg, Bg, Cursor color.Color
}

// Event is an event line: its time, its code, and its data.
type Event struct {
	// Time is the event's offset from the start of the recording. In a
	// file, v3 holds the interval since the event before, and v2 the
	// offset.
	Time time.Duration
	Code string
	Data string
}

// castHeader is the JSON of a header line. Term is set in v3, and Width,
// Height, Env, and Theme in v2.
type castHeader struct {
	Version      int            `json:"version"`
	Term         *castTerm      `json:"term,omitzero"`
	Width        int            `json:"width,omitzero"`
	Height       int            `json:"height,omitzero"`
	Timestamp    int64          `json:"timestamp,omitzero"`
	Title        string         `json:"title,omitzero"`
	Env          map[string]any `json:"env,omitzero"`
	Theme        *castTheme     `json:"theme,omitzero"`
	SkipPristine bool           `json:"skip_pristine,omitzero"`
	Scrollback   *int           `json:"scrollback,omitzero"`
}

type castTerm struct {
	Cols  int        `json:"cols"`
	Rows  int        `json:"rows"`
	Type  string     `json:"type,omitzero"`
	Theme *castTheme `json:"theme,omitzero"`
}

// castTheme is the JSON of a header's colors. Fg, Bg, and Palette are
// asciicast's, which readers need all of. Cursor is reel's.
type castTheme struct {
	Fg      string `json:"fg"`
	Bg      string `json:"bg"`
	Palette string `json:"palette"`
	Cursor  string `json:"cursor,omitzero"`
}

// ParseSize parses a terminal width or height: digits, from 1 to
// [MaxSize].
func ParseSize(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && s[0] != '+' && n > 0 && n <= MaxSize
}

// FormatColors formats c as a theme event's data,
// "fg=#rrggbb,bg=#rrggbb,cursor=#rrggbb", leaving out the colors that are
// nil.
func FormatColors(c Colors) string {
	var parts []string
	for _, kv := range []struct {
		key string
		c   color.Color
	}{{"fg", c.Fg}, {"bg", c.Bg}, {"cursor", c.Cursor}} {
		if kv.c != nil {
			parts = append(parts, kv.key+"="+formatColor(kv.c))
		}
	}
	return strings.Join(parts, ",")
}

// ParseColors parses a theme event's data. Keys other than fg, bg, and
// cursor are ignored, and a key left out or with no value is a nil color.
func ParseColors(data string) (Colors, error) {
	var c Colors
	if data == "" {
		return c, nil
	}
	for part := range strings.SplitSeq(data, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return c, fmt.Errorf("%q is not key=#rrggbb", part)
		}
		var dst *color.Color
		switch key {
		case "fg":
			dst = &c.Fg
		case "bg":
			dst = &c.Bg
		case "cursor":
			dst = &c.Cursor
		default:
			continue
		}
		if value == "" {
			continue
		}
		col, err := parseColor(value)
		if err != nil {
			return c, fmt.Errorf("%s: %w", key, err)
		}
		*dst = col
	}
	return c, nil
}

// formatPalette formats p as a header's palette.
func formatPalette(p [16]color.Color) string {
	colors := make([]string, len(p))
	for i, c := range p {
		colors[i] = formatColor(c)
	}
	return strings.Join(colors, ":")
}

// parsePalette parses a header's palette of 8 or 16 colors. An empty
// palette is none.
func parsePalette(s string) ([16]color.Color, error) {
	var p [16]color.Color
	if s == "" {
		return p, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 8 && len(parts) != 16 {
		return p, fmt.Errorf("palette has %d colors, want 8 or 16", len(parts))
	}
	for i, part := range parts {
		c, err := parseColor(part)
		if err != nil {
			return p, err
		}
		p[i] = c
	}
	return p, nil
}

// formatColor formats c as "#rrggbb", dropping its alpha.
func formatColor(c color.Color) string {
	n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
	return fmt.Sprintf("#%02x%02x%02x", n.R, n.G, n.B)
}

// parseColor parses "#rrggbb" as an opaque color.
func parseColor(s string) (color.Color, error) {
	if len(s) != 7 || s[0] != '#' {
		return nil, fmt.Errorf("color %q is not #rrggbb", s)
	}
	rgb, err := hex.DecodeString(s[1:])
	if err != nil {
		return nil, fmt.Errorf("color %q is not #rrggbb", s)
	}
	return color.NRGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 0xff}, nil
}
