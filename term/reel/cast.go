// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel/vt/screen"
)

// CastMediaType is the media type of the files [Timeline.WriteTo] writes,
// which take the extension ".cast".
const CastMediaType = "application/x-asciicast"

// CastVersion is a version of the asciicast format, the newline-delimited
// JSON that asciinema records and plays, which [Timeline.WriteCast] writes.
type CastVersion int

// Asciicast versions.
const (
	// CastV2 is asciicast v2, for players that do not read v3. It has no
	// event for focus, theme, or exit, so those are left out.
	CastV2 CastVersion = 2
	// CastV3 is asciicast v3, which [Timeline.WriteTo] writes. It keeps
	// every event.
	CastV3 CastVersion = 3
)

// castTermType is the terminal type files name, since the terminal is
// libghostty-vt.
const castTermType = "xterm-ghostty"

var _ io.WriterTo = (*Timeline)(nil)

// castHeader is the first line of an asciicast file. Term is set in v3, and
// Width, Height, Env, and Theme in v2.
type castHeader struct {
	Version   int            `json:"version"`
	Term      *castTerm      `json:"term,omitempty"`
	Width     int            `json:"width,omitempty"`
	Height    int            `json:"height,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Title     string         `json:"title,omitempty"`
	Env       map[string]any `json:"env,omitempty"`
	Theme     *castTheme     `json:"theme,omitempty"`
}

type castTerm struct {
	Cols  int        `json:"cols"`
	Rows  int        `json:"rows"`
	Type  string     `json:"type,omitempty"`
	Theme *castTheme `json:"theme,omitempty"`
}

// castTheme is the colors a file starts with. Fg, Bg, and Palette are
// asciicast's, which readers need all of. Cursor is reel's, which v3 readers
// ignore.
type castTheme struct {
	Fg      string `json:"fg"`
	Bg      string `json:"bg"`
	Palette string `json:"palette"`
	Cursor  string `json:"cursor,omitempty"`
}

// WriteTo writes the timeline to w as asciicast v3, the format asciinema
// records, and returns the number of bytes written. It is
// [Timeline.WriteCast] with [CastV3], where the format is described.
func (t *Timeline) WriteTo(w io.Writer) (int64, error) {
	return t.WriteCast(w, CastV3)
}

// WriteCast writes the timeline to w as asciicast version v, and returns the
// number of bytes written. [ReadTimeline] reads it back. The file holds the
// events appended before the call. A timeline returned by [Timeline.Between]
// is written with every event it keeps, from the first; its window is not.
//
// Event times are rounded to milliseconds. In v3, where each is written as
// the interval since the one before, the intervals add up to the rounded
// times, so a long recording does not drift.
//
// [Output] and [Input] bytes are written as JSON strings, which hold UTF-8.
// A character split between two events of the same kind, as a program's
// writes often split them, is moved whole into the later event, so the bytes
// read back are the bytes written. Bytes that are not UTF-8, such as an X10
// mouse report past column 94, are written as U+FFFD, as asciinema writes
// them.
//
// The header has the size, the title, the time of the first event, and the
// header colors with asciicast's 16 color palette, which is the one reel
// draws with by default: the timeline has none of its own. The colors are
// left out unless Fg and Bg are set. Events are asciicast's output ("o"),
// input ("i"), resize ("r", as "COLSxROWS"), marker ("m"), and exit ("x"),
// and two of reel's own, which other readers ignore: focus ("f", as "1" or
// "0") and theme ("t", as "fg=#rrggbb,bg=#rrggbb,cursor=#rrggbb", leaving out
// the colors that are nil). [CastV2] has neither these nor exit.
func (t *Timeline) WriteCast(w io.Writer, v CastVersion) (int64, error) {
	if v != CastV3 && v != CastV2 {
		return 0, fmt.Errorf("reel: unknown asciicast version %d", v)
	}
	events := t.snapshot()
	start := t.started()

	cw := &countWriter{w: w}
	bw := bufio.NewWriter(cw)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)

	err := enc.Encode(castHeaderOf(t.header, start, v))
	if err == nil {
		err = writeCastEvents(enc, events, v)
	}
	if err == nil {
		err = bw.Flush()
	}
	return cw.n, err
}

// castHeaderOf returns the header line for a timeline with header h whose
// first event was appended at start.
func castHeaderOf(h Header, start time.Time, v CastVersion) castHeader {
	ch := castHeader{Version: int(v), Title: h.Title}
	if !start.IsZero() {
		ch.Timestamp = start.Unix()
	}
	var theme *castTheme
	if h.Theme.Fg != nil && h.Theme.Bg != nil {
		theme = &castTheme{
			Fg:      hexColor(h.Theme.Fg),
			Bg:      hexColor(h.Theme.Bg),
			Palette: defaultCastPalette(),
		}
	}
	if v == CastV2 {
		ch.Width, ch.Height = h.Cols, h.Rows
		ch.Env = map[string]any{"TERM": castTermType}
		ch.Theme = theme
		return ch
	}
	if theme != nil && h.Theme.Cursor != nil {
		theme.Cursor = hexColor(h.Theme.Cursor)
	}
	ch.Term = &castTerm{Cols: h.Cols, Rows: h.Rows, Type: castTermType, Theme: theme}
	return ch
}

// defaultCastPalette returns the 16 colors indexed colors are drawn with
// when no palette is set, in asciicast's form.
func defaultCastPalette() string {
	colors := make([]string, 16)
	for i := range colors {
		colors[i] = hexColor(screen.Indexed(i))
	}
	return strings.Join(colors, ":")
}

// writeCastEvents writes one line per event.
func writeCastEvents(enc *json.Encoder, events []Event, v CastVersion) error {
	// No event comes after the last Output and Input to finish a character
	// split at their end.
	lastOutput, lastInput := -1, -1
	for i, ev := range events {
		switch ev.(type) {
		case Output:
			lastOutput = i
		case Input:
			lastInput = i
		}
	}
	// Split characters carried into the next Output and Input.
	var outputTail, inputTail []byte
	var prev time.Duration
	line := make([]any, 3)

	for i, ev := range events {
		var code, data string
		// v3Only is set for the events v2 has no code for.
		v3Only := false
		switch ev := ev.(type) {
		case Output:
			code = "o"
			data, outputTail = carryRune(outputTail, ev.Data, i < lastOutput)
		case Input:
			code = "i"
			data, inputTail = carryRune(inputTail, ev.Data, i < lastInput)
		case Resize:
			code, data = "r", strconv.Itoa(ev.Cols)+"x"+strconv.Itoa(ev.Rows)
		case Mark:
			code, data = "m", ev.Label
		case Focus:
			code, data, v3Only = "f", "0", true
			if ev.On {
				data = "1"
			}
		case Theme:
			code, data, v3Only = "t", formatCastColors(ev.Colors), true
		case Exit:
			code, data, v3Only = "x", strconv.Itoa(ev.Status), true
		}
		switch {
		case v == CastV2 && v3Only:
			continue
		case (code == "o" || code == "i") && data == "":
			// No bytes are left, as when the event held only the start of a
			// split character.
			continue
		}

		// v3 times are intervals, v2 times are offsets.
		at, from := ev.At().Round(time.Millisecond), time.Duration(0)
		if v == CastV3 {
			from = prev
		}
		prev = at
		line[0], line[1], line[2] = castTime(at-from), code, data
		if err := enc.Encode(line); err != nil {
			return err
		}
	}
	return nil
}

// carryRune returns data after tail as a string, without the start of a
// character split at its end, which it returns as the new tail. When more
// is false, nothing comes after data to finish the character, so it is
// returned whole.
func carryRune(tail, data []byte, more bool) (s string, newTail []byte) {
	if len(tail) > 0 {
		data = append(tail, data...)
	}
	if !more {
		return string(data), nil
	}
	// A split character starts at most utf8.UTFMax-1 bytes from the end,
	// and utf8.FullRune is false only for the start of a valid one.
	for i := len(data) - 1; i >= 0 && i >= len(data)-(utf8.UTFMax-1); i-- {
		if utf8.RuneStart(data[i]) {
			if !utf8.FullRune(data[i:]) {
				return string(data[:i]), bytes.Clone(data[i:])
			}
			break
		}
	}
	return string(data), nil
}

// castTime formats an offset as seconds with millisecond precision.
func castTime(d time.Duration) json.Number {
	ms := d.Milliseconds()
	return json.Number(fmt.Sprintf("%d.%03d", ms/1000, ms%1000))
}

// formatCastColors formats colors as a theme event's data.
func formatCastColors(c Colors) string {
	var parts []string
	for _, kv := range []struct {
		key string
		c   color.Color
	}{{"fg", c.Fg}, {"bg", c.Bg}, {"cursor", c.Cursor}} {
		if kv.c != nil {
			parts = append(parts, kv.key+"="+hexColor(kv.c))
		}
	}
	return strings.Join(parts, ",")
}

// hexColor formats c as "#rrggbb", dropping its alpha.
func hexColor(c color.Color) string {
	n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
	return fmt.Sprintf("#%02x%02x%02x", n.R, n.G, n.B)
}

// countWriter counts the bytes written through it.
type countWriter struct {
	w io.Writer
	n int64
}

func (cw *countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

// ReadTimeline reads a timeline written by [Timeline.WriteTo], or any
// asciicast v3 or v2 file, such as one asciinema recorded. The timeline does
// not grow.
//
// Lines starting with "#" after the header are comments, and blank lines are
// skipped. Header fields and event codes reel does not use are ignored, and
// so is the header's palette: [Header] has none. Times in a v2 file are from
// the start rather than from the event before; a time before the previous
// event's is raised to it. A marker with no label reads as a [Mark] with an
// empty Label, which [Timeline.Between] cannot cut at.
//
// An asciicast v1 file, a header without a terminal size or with a malformed
// field reel reads, a line that is not a JSON array of time, code, and data,
// and an event reel reads whose data is malformed, such as a resize that is
// not "COLSxROWS", are errors, which name the line. Sizes are at most 65535,
// and times at most a year.
func ReadTimeline(r io.Reader) (*Timeline, error) {
	br := bufio.NewReader(r)
	line, err := readCastLine(br)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reel: reading asciicast: %w", err)
	}
	line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, errors.New("reel: asciicast line 1: no header")
	}

	var ch castHeader
	if jerr := json.Unmarshal(line, &ch); jerr != nil {
		if isCastV1(line, br) {
			return nil, errCastV1
		}
		return nil, fmt.Errorf("reel: asciicast line 1: header: %w", jerr)
	}
	t, terr := timelineOf(ch)
	if terr != nil {
		return nil, terr
	}

	var at time.Duration
	for n := 2; !errors.Is(err, io.EOF); n++ {
		line, err = readCastLine(br)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("reel: reading asciicast: %w", err)
		}
		if len(bytes.TrimSpace(line)) == 0 || line[0] == '#' {
			continue
		}
		ev, d, perr := parseCastEvent(line)
		if perr != nil {
			return nil, fmt.Errorf("reel: asciicast line %d: %w", n, perr)
		}
		if ch.Version == 3 {
			if d > maxCastTime-at {
				return nil, fmt.Errorf("reel: asciicast line %d: event time is out of range", n)
			}
			d += at
		}
		at = d
		if ev != nil {
			t.appendAt(ev, at)
		}
	}
	return t, nil
}

var errCastV1 = errors.New("reel: asciicast v1 is not supported")

const (
	// maxCastTime bounds event times, well inside a [time.Duration].
	maxCastTime = 365 * 24 * time.Hour
	// maxCastSize bounds the terminal size, as asciinema does.
	maxCastSize = math.MaxUint16
)

// readCastLine returns the next line of br without its line ending.
func readCastLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadBytes('\n')
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	return line, err
}

// isCastV1 reports whether the file starting with the line first, which is
// not a header line, is asciicast v1: one JSON object over many lines. It
// looks for the version among the object's fields in the first MiB, where v1
// files have it first.
func isCastV1(first []byte, br *bufio.Reader) bool {
	dec := json.NewDecoder(io.MultiReader(bytes.NewReader(first), io.LimitReader(br, 1<<20)))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return false
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return false
		}
		if key == "version" {
			var v int
			return dec.Decode(&v) == nil && v == 1
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return false
}

// timelineOf returns an empty timeline with the header ch describes.
func timelineOf(ch castHeader) (*Timeline, error) {
	var h Header
	var theme *castTheme
	switch ch.Version {
	case 1:
		return nil, errCastV1
	case 2:
		h.Cols, h.Rows, theme = ch.Width, ch.Height, ch.Theme
	case 3:
		if ch.Term != nil {
			h.Cols, h.Rows, theme = ch.Term.Cols, ch.Term.Rows, ch.Term.Theme
		}
	default:
		return nil, fmt.Errorf("reel: unsupported asciicast version %d", ch.Version)
	}
	if h.Cols <= 0 || h.Rows <= 0 || h.Cols > maxCastSize || h.Rows > maxCastSize {
		return nil, fmt.Errorf("reel: asciicast line 1: header: terminal size %dx%d", h.Cols, h.Rows)
	}
	h.Title = ch.Title
	if theme != nil {
		var err error
		if h.Theme, err = parseCastTheme(theme); err != nil {
			return nil, fmt.Errorf("reel: asciicast line 1: header: %w", err)
		}
	}
	t := newTimeline(h)
	if ch.Timestamp > 0 {
		t.start = time.Unix(ch.Timestamp, 0)
	}
	return t, nil
}

// parseCastTheme returns the header colors of theme.
func parseCastTheme(theme *castTheme) (Colors, error) {
	var c Colors
	var err error
	if c.Fg, err = parseHexColor(theme.Fg); err != nil {
		return c, fmt.Errorf("theme fg: %w", err)
	}
	if c.Bg, err = parseHexColor(theme.Bg); err != nil {
		return c, fmt.Errorf("theme bg: %w", err)
	}
	if theme.Cursor != "" {
		if c.Cursor, err = parseHexColor(theme.Cursor); err != nil {
			return c, fmt.Errorf("theme cursor: %w", err)
		}
	}
	return c, nil
}

// parseCastEvent parses an event line. Its time is the interval since the
// event before in v3, and the offset from the start in v2. The event is nil
// when reel does not read its code.
func parseCastEvent(line []byte) (Event, time.Duration, error) {
	var fields []json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return nil, 0, err
	}
	if len(fields) != 3 {
		return nil, 0, fmt.Errorf("event has %d fields, want time, code, and data", len(fields))
	}
	var secs *float64
	if err := json.Unmarshal(fields[0], &secs); err != nil || secs == nil {
		return nil, 0, fmt.Errorf("event time %s is not a number", fields[0])
	}
	if *secs < 0 || *secs > maxCastTime.Seconds() {
		return nil, 0, fmt.Errorf("event time %v is out of range", *secs)
	}
	d := time.Duration(math.Round(*secs * float64(time.Second)))
	var code string
	if err := json.Unmarshal(fields[1], &code); err != nil {
		return nil, 0, fmt.Errorf("event code: %w", err)
	}
	if code == "" {
		return nil, 0, errors.New("event has no code")
	}
	decode, ok := castDecoders[code]
	if !ok {
		return nil, d, nil
	}
	var data *string
	if err := json.Unmarshal(fields[2], &data); err != nil || data == nil {
		return nil, 0, fmt.Errorf("%q event: data %s is not a string", code, fields[2])
	}
	ev, err := decode(*data)
	if err != nil {
		return nil, 0, fmt.Errorf("%q event: %w", code, err)
	}
	return ev, d, nil
}

// castDecoders parse the data of the event codes reel reads.
var castDecoders = map[string]func(data string) (Event, error){
	"o": func(data string) (Event, error) { return Output{Data: []byte(data)}, nil },
	"i": func(data string) (Event, error) { return Input{Data: []byte(data)}, nil },
	"m": func(data string) (Event, error) { return Mark{Label: data}, nil },
	"r": func(data string) (Event, error) {
		cols, rows, _ := strings.Cut(data, "x")
		c, cok := parseCastSize(cols)
		r, rok := parseCastSize(rows)
		if !cok || !rok {
			return nil, fmt.Errorf("size %q is not COLSxROWS", data)
		}
		return Resize{Cols: c, Rows: r}, nil
	},
	"x": func(data string) (Event, error) {
		status, err := strconv.Atoi(data)
		if err != nil {
			return nil, fmt.Errorf("status %q is not a number", data)
		}
		return Exit{Status: status}, nil
	},
	"f": func(data string) (Event, error) {
		if data != "0" && data != "1" {
			return nil, fmt.Errorf("focus %q is not 0 or 1", data)
		}
		return Focus{On: data == "1"}, nil
	},
	"t": func(data string) (Event, error) {
		c, err := parseCastColors(data)
		return Theme{Colors: c}, err
	},
}

// parseCastSize parses a terminal width or height: digits, from 1 to
// maxCastSize.
func parseCastSize(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && s[0] != '+' && n > 0 && n <= maxCastSize
}

// parseCastColors parses a theme event's data. Keys other than fg, bg, and
// cursor are ignored, and a key left out or with no value is a nil color.
func parseCastColors(data string) (Colors, error) {
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
		col, err := parseHexColor(value)
		if err != nil {
			return c, fmt.Errorf("%s: %w", key, err)
		}
		*dst = col
	}
	return c, nil
}

// parseHexColor parses "#rrggbb" as an opaque color.
func parseHexColor(s string) (color.Color, error) {
	if len(s) != 7 || s[0] != '#' {
		return nil, fmt.Errorf("color %q is not #rrggbb", s)
	}
	rgb, err := hex.DecodeString(s[1:])
	if err != nil {
		return nil, fmt.Errorf("color %q is not #rrggbb", s)
	}
	return color.NRGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 0xff}, nil
}
