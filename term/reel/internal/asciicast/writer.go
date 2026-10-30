// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package asciicast

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// Writer writes the event lines of a file.
type Writer struct {
	enc  *jsontext.Encoder
	v3   bool
	prev time.Duration
}

// NewWriter writes h to w as a header line, and returns a Writer of the
// event lines after it. Version is 2 or 3. A theme's Fg, Bg, and palette
// colors must not be nil.
func NewWriter(w io.Writer, h Header) (*Writer, error) {
	if h.Version != 2 && h.Version != 3 {
		return nil, fmt.Errorf("unsupported asciicast version %d", h.Version)
	}
	// Bytes that are not UTF-8 are written as U+FFFD, as asciinema writes
	// them.
	enc := jsontext.NewEncoder(w, jsontext.AllowInvalidUTF8(true))
	if err := json.MarshalEncode(enc, headerOf(h)); err != nil {
		return nil, err
	}
	return &Writer{enc: enc, v3: h.Version == 3}, nil
}

// headerOf returns the JSON of h.
func headerOf(h Header) castHeader {
	hl := castHeader{
		Version: h.Version, Title: h.Title, SkipPristine: h.SkipPristine, Scrollback: h.Scrollback,
		CellWidth: h.CellWidth, CellHeight: h.CellHeight, KittyStorage: h.KittyStorage,
	}
	if !h.Timestamp.IsZero() {
		hl.Timestamp = h.Timestamp.Unix()
	}
	var th *castTheme
	if h.Theme != nil {
		th = &castTheme{
			Fg:      formatColor(h.Theme.Fg),
			Bg:      formatColor(h.Theme.Bg),
			Palette: formatPalette(h.Theme.Palette),
		}
		if h.Theme.Cursor != nil {
			th.Cursor = formatColor(h.Theme.Cursor)
		}
	}
	if h.Version == 2 {
		hl.Width, hl.Height = h.Cols, h.Rows
		if h.Term != "" {
			hl.Env = map[string]any{"TERM": h.Term}
		}
		hl.Theme = th
		return hl
	}
	hl.Term = &castTerm{Cols: h.Cols, Rows: h.Rows, Type: h.Term, Theme: th}
	return hl
}

// WriteEvent writes ev as an event line. Its time, which must not be before
// the time of the event written before it, is written in whole
// milliseconds. In v3, where it is the interval since that event, the
// intervals add up to the times, so a long recording does not drift.
func (w *Writer) WriteEvent(ev Event) error {
	from := time.Duration(0)
	if w.v3 {
		from = w.prev
	}
	w.prev = ev.Time
	return json.MarshalEncode(w.enc, [3]any{formatTime(ev.Time - from), ev.Code, ev.Data})
}

// formatTime formats an offset in whole milliseconds as seconds.
func formatTime(d time.Duration) jsontext.Value {
	ms := d.Milliseconds()
	return jsontext.Value(fmt.Sprintf("%d.%03d", ms/1000, ms%1000))
}

// CarryRune returns data after tail as a string, for the data of an event,
// without the start of a character split at its end, which it returns as
// the new tail to pass with the data of the next event of the same kind.
// When more is false, no such event comes after data to finish the
// character, so it is returned whole.
//
// Data is a JSON string, which holds UTF-8, so carrying a character split
// between two events into the later one keeps the bytes read back the
// bytes written. Bytes that are not UTF-8 are written as U+FFFD.
func CarryRune(tail, data []byte, more bool) (s string, newTail []byte) {
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
