// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package asciicast

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"time"
)

var errV1 = errors.New("asciicast v1 is not supported")

// Reader reads the event lines of a file. Lines starting with "#" after the
// header are comments, and blank lines are skipped, as are header fields
// it does not read. Lines may end with "\r\n", and the file may start with a
// byte order mark.
//
// Errors in a line name it, as "asciicast line N: ...". Errors reading the
// underlying reader are "reading asciicast: ...".
type Reader struct {
	// Header is the file's header.
	Header Header

	br    *bufio.Reader
	codes []string
	// line is the number of the line read last.
	line int
	// at is the time of the event read last, which v3 intervals count
	// from.
	at time.Duration
	// err is the error reading the line read last, which is [io.EOF] when
	// it was the last line.
	err error
}

// NewReader reads the header of the file r reads, and returns a Reader of
// the events after it, which returns those with one of the codes.
//
// An asciicast v1 file, which is one JSON object rather than a line per
// event, is an error, as is a version other than 2 and 3, or a header
// without a terminal size or with a malformed field it reads.
func NewReader(r io.Reader, codes ...string) (*Reader, error) {
	cr := &Reader{br: bufio.NewReader(r), codes: codes, line: 1}
	line, err := readLine(cr.br)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading asciicast: %w", err)
	}
	cr.err = err
	line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, errors.New("asciicast line 1: no header")
	}

	var hl castHeader
	if jerr := json.Unmarshal(line, &hl); jerr != nil {
		if isV1(line, cr.br) {
			return nil, errV1
		}
		return nil, fmt.Errorf("asciicast line 1: header: %w", jerr)
	}
	if cr.Header, err = parseHeader(hl); err != nil {
		return nil, err
	}
	return cr, nil
}

// Line returns the number of the line of the event read last.
func (r *Reader) Line() int { return r.line }

// Next returns the next event with one of the Reader's codes, or [io.EOF]
// when the file has ended. Events with other codes are skipped, though
// their times count, and their data need not be a string.
//
// A line that is not a JSON array of time, code, and data is an error, as
// is a time that is not a number of seconds, or that puts the event more
// than [MaxTime] from the start. A time before the time of the event
// before is returned as it is.
func (r *Reader) Next() (Event, error) {
	for !errors.Is(r.err, io.EOF) {
		var line []byte
		line, r.err = readLine(r.br)
		r.line++
		if r.err != nil && !errors.Is(r.err, io.EOF) {
			return Event{}, fmt.Errorf("reading asciicast: %w", r.err)
		}
		if len(bytes.TrimSpace(line)) == 0 || line[0] == '#' {
			continue
		}
		ev, read, err := r.parseEvent(line)
		if err != nil {
			return Event{}, fmt.Errorf("asciicast line %d: %w", r.line, err)
		}
		if r.Header.Version == 3 {
			if ev.Time > MaxTime-r.at {
				return Event{}, fmt.Errorf("asciicast line %d: event time is out of range", r.line)
			}
			ev.Time += r.at
		}
		r.at = ev.Time
		if read {
			return ev, nil
		}
	}
	return Event{}, io.EOF
}

// parseEvent parses an event line, and reports whether its code is one of
// r's. Its time is the interval since the event before in v3, and the
// offset from the start in v2. Data is left empty for the other codes.
func (r *Reader) parseEvent(line []byte) (Event, bool, error) {
	var ev Event
	var fields []json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return ev, false, err
	}
	if len(fields) != 3 {
		return ev, false, fmt.Errorf("event has %d fields, want time, code, and data", len(fields))
	}
	var secs *float64
	if err := json.Unmarshal(fields[0], &secs); err != nil || secs == nil {
		return ev, false, fmt.Errorf("event time %s is not a number", fields[0])
	}
	if *secs < 0 || *secs > MaxTime.Seconds() {
		return ev, false, fmt.Errorf("event time %v is out of range", *secs)
	}
	ev.Time = time.Duration(math.Round(*secs * float64(time.Second)))
	if err := json.Unmarshal(fields[1], &ev.Code); err != nil {
		return ev, false, fmt.Errorf("event code: %w", err)
	}
	if ev.Code == "" {
		return ev, false, errors.New("event has no code")
	}
	if !slices.Contains(r.codes, ev.Code) {
		return ev, false, nil
	}
	var data *string
	if err := json.Unmarshal(fields[2], &data); err != nil || data == nil {
		return ev, false, fmt.Errorf("%q event: data %s is not a string", ev.Code, fields[2])
	}
	ev.Data = *data
	return ev, true, nil
}

// readLine returns the next line of br without its line ending.
func readLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadBytes('\n')
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	return line, err
}

// isV1 reports whether the file starting with the line first, which is not
// a header line, is asciicast v1: one JSON object over many lines. It looks
// for the version among the object's fields in the first MiB, where v1
// files have it first.
func isV1(first []byte, br *bufio.Reader) bool {
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

// parseHeader returns the header hl holds.
func parseHeader(hl castHeader) (Header, error) {
	h := Header{Version: hl.Version}
	var th *castTheme
	switch hl.Version {
	case 1:
		return h, errV1
	case 2:
		h.Cols, h.Rows, th = hl.Width, hl.Height, hl.Theme
	case 3:
		if hl.Term != nil {
			h.Cols, h.Rows, th = hl.Term.Cols, hl.Term.Rows, hl.Term.Theme
		}
	default:
		return h, fmt.Errorf("unsupported asciicast version %d", hl.Version)
	}
	if h.Cols <= 0 || h.Rows <= 0 || h.Cols > MaxSize || h.Rows > MaxSize {
		return h, fmt.Errorf("asciicast line 1: header: terminal size %dx%d", h.Cols, h.Rows)
	}
	h.Title, h.SkipPristine = hl.Title, hl.SkipPristine
	if hl.Timestamp > 0 {
		h.Timestamp = time.Unix(hl.Timestamp, 0)
	}
	if th != nil {
		h.Theme = &Theme{}
		var err error
		if h.Theme.Colors, err = parseTheme(th); err != nil {
			return h, fmt.Errorf("asciicast line 1: header: %w", err)
		}
		if h.Theme.Palette, err = parsePalette(th.Palette); err != nil {
			return h, fmt.Errorf("asciicast line 1: header: theme %w", err)
		}
	}
	return h, nil
}

// parseTheme returns the colors of th.
func parseTheme(th *castTheme) (Colors, error) {
	var c Colors
	var err error
	if c.Fg, err = parseColor(th.Fg); err != nil {
		return c, fmt.Errorf("theme fg: %w", err)
	}
	if c.Bg, err = parseColor(th.Bg); err != nil {
		return c, fmt.Errorf("theme bg: %w", err)
	}
	if th.Cursor != "" {
		if c.Cursor, err = parseColor(th.Cursor); err != nil {
			return c, fmt.Errorf("theme cursor: %w", err)
		}
	}
	return c, nil
}
