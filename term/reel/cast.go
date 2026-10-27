// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/internal/asciicast"
	"github.com/lrstanley/x/term/reel/vt"
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

// WriteTo writes the timeline to w as asciicast v3, the format asciinema
// records, and returns the number of bytes written. It is
// [Timeline.WriteCast] with [CastV3], where the format is described.
func (t *Timeline) WriteTo(w io.Writer) (int64, error) {
	return t.WriteCast(w, CastV3)
}

// WriteCast writes the timeline to w as asciicast version v, and returns the
// number of bytes written. [ReadTimeline] reads it back. The file holds the
// events appended before the call.
//
// A timeline returned by [Timeline.Between] is written as its window, so any
// player shows only the cut. The events before the window are replayed into
// a fresh terminal and replaced by one output event at the start, which
// redraws what they left on it (see [vt.Terminal.Redraw] for what that
// covers), followed by a theme and a focus event for the host colors and
// focus they left, when they changed them. The header has the size they
// left. Times count from the window's start, and the header's timestamp is
// when the window started.
//
// The redraw holds only the screen shown at the window's start. A cut that
// starts on the alternate screen, such as inside a full-screen program, and
// later leaves it, shows a blank primary screen from then on, where the
// source showed what was there before the program started. [Export] of the
// cut replays every event, so its frames are unaffected.
//
// Event times are whole milliseconds (see [Timeline]), written exactly. In
// v3, where each is written as the interval since the one before, the
// intervals add up to the times, so a long recording does not drift.
//
// [Output] and [Input] bytes are written as JSON strings, which hold UTF-8.
// A character split between two events of the same kind, as a program's
// writes often split them, is moved whole into the later event, so the bytes
// read back are the bytes written. An event left with no bytes is written
// empty, which replays as it was. Bytes that are not UTF-8, such as an X10
// mouse report past column 94, are written as U+FFFD, as asciinema writes
// them.
//
// The header has the size, the title, the time of the first event, and the
// header colors with asciicast's 16 color palette: [Header.Palette], with
// the colors reel draws with by default for its nil entries. The colors are
// left out unless Fg and Bg are set. A v3 header has reel's own
// "skip_pristine": true when [Header.SkipPristine] is set, which other
// readers ignore; [CastV2] leaves it out, so a v2 file read back replays
// the frames it skipped. Events are asciicast's output ("o"),
// input ("i"), resize ("r", as "COLSxROWS"), marker ("m"), and exit ("x"),
// and two of reel's own, which other readers ignore: focus ("f", as "1" or
// "0") and theme ("t", as "fg=#rrggbb,bg=#rrggbb,cursor=#rrggbb", leaving out
// the colors that are nil). [CastV2] has neither these nor exit.
//
// [Output] is the bytes the program wrote for a terminal of the size it had,
// and programs move the cursor by that size, as bash's readline does to
// redraw a line that wraps. A player that writes them to the terminal it runs
// in, such as asciinema's play command, shows them as recorded only when that
// terminal has the same size: in a wider one, such a redraw overwrites the
// lines above it. asciinema-player sizes its terminal from the header and the
// resize events, so it is unaffected.
func (t *Timeline) WriteCast(w io.Writer, v CastVersion) (int64, error) {
	if v != CastV3 && v != CastV2 {
		return 0, fmt.Errorf("reel: unknown asciicast version %d", v)
	}
	if t.from >= 0 {
		c, err := t.cut()
		if err != nil {
			return 0, err
		}
		return c.WriteCast(w, v)
	}
	events := t.snapshot()
	start := t.started()

	cw := &countWriter{w: w}
	bw := bufio.NewWriter(cw)
	aw, err := asciicast.NewWriter(bw, castHeaderOf(t.header, start, v))
	if err == nil {
		err = writeCastEvents(aw, events, v)
	}
	if err == nil {
		err = bw.Flush()
	}
	return cw.n, err
}

// cut returns the window of t as a timeline of its own, which starts at the
// window's start: the events before the window are replaced by an output
// event that redraws the terminal they leave, and the host colors and focus
// they leave, at offset zero.
func (t *Timeline) cut() (*Timeline, error) {
	events := t.snapshot()
	h, start := t.header, t.started()
	from := max(t.from, 0)
	before, window := events[:from], events[from:]
	at := window[0].At()
	out := make([]Event, 0, len(window)+3)
	if len(before) > 0 {
		r, err := newReplay(context.Background(), replayRuntime, h, from, 0)
		if err != nil {
			return nil, err
		}
		defer r.close()
		var theme *Theme
		for _, ev := range before {
			if aerr := r.apply(ev); aerr != nil {
				return nil, aerr
			}
			if th, ok := ev.(Theme); ok {
				theme = &th
			}
		}
		redraw, err := r.term.Redraw()
		if err != nil {
			return nil, err
		}
		h.Cols, h.Rows = r.term.Size()
		out = append(out, Output{Data: redraw})
		if theme != nil {
			out = append(out, Theme{Colors: theme.Colors})
		}
		if !r.focused {
			out = append(out, Focus{})
		}
	}
	for _, ev := range window {
		out = append(out, ev.stamp(ev.At()-at))
	}
	if !start.IsZero() {
		start = start.Add(at)
	}
	return &Timeline{header: h, start: start, events: out, from: -1}, nil
}

// castHeaderOf returns the header line for a timeline with header h whose
// first event was appended at start.
func castHeaderOf(h Header, start time.Time, v CastVersion) asciicast.Header {
	ch := asciicast.Header{
		Version: int(v), Cols: h.Cols, Rows: h.Rows, Term: castTermType,
		Timestamp: start, Title: h.Title, SkipPristine: h.SkipPristine,
	}
	if h.Theme.Fg != nil && h.Theme.Bg != nil {
		ch.Theme = &asciicast.Theme{Colors: asciicast.Colors(h.Theme), Palette: castPalette(h.Palette)}
	}
	return ch
}

// castPalette returns p with the colors indexed colors are drawn with by
// default for its nil entries.
func castPalette(p [16]color.Color) [16]color.Color {
	for i, c := range p {
		if c == nil {
			p[i] = vt.Indexed(i)
		}
	}
	return p
}

// writeCastEvents writes one line per event.
func writeCastEvents(aw *asciicast.Writer, events []Event, v CastVersion) error {
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

	for i, ev := range events {
		var code, data string
		// v3Only is set for the events v2 has no code for.
		v3Only := false
		switch ev := ev.(type) {
		case Output:
			code = "o"
			data, outputTail = asciicast.CarryRune(outputTail, ev.Data, i < lastOutput)
		case Input:
			code = "i"
			data, inputTail = asciicast.CarryRune(inputTail, ev.Data, i < lastInput)
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
			code, data, v3Only = "t", asciicast.FormatColors(asciicast.Colors(ev.Colors)), true
		case Exit:
			code, data, v3Only = "x", strconv.Itoa(ev.Status), true
		}
		if v == CastV2 && v3Only {
			continue
		}
		if err := aw.WriteEvent(asciicast.Event{Time: ev.At(), Code: code, Data: data}); err != nil {
			return err
		}
	}
	return nil
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
// skipped. Header fields and event codes reel does not use are ignored. The
// header's palette is read into [Header.Palette], and reel's "skip_pristine"
// into [Header.SkipPristine]. Times in a v2 file are from the start rather
// than from the event before; a time before the previous event's is raised
// to it. Each event's time from the start is rounded to the millisecond, as
// a [Timeline] keeps it, so intervals finer than that, as asciinema writes
// them, do not drift. A marker with no label reads as a [Mark] with an empty
// Label, which [Timeline.Between] cannot cut at.
//
// An asciicast v1 file, a header without a terminal size or with a malformed
// field reel reads, a line that is not a JSON array of time, code, and data,
// and an event reel reads whose data is malformed, such as a resize that is
// not "COLSxROWS", are errors, which name the line. Sizes are at most 65535,
// and times at most a year.
func ReadTimeline(r io.Reader) (*Timeline, error) {
	cr, err := asciicast.NewReader(r, castCodes...)
	if err != nil {
		return nil, fmt.Errorf("reel: %w", err)
	}
	ch := cr.Header
	h := Header{Cols: ch.Cols, Rows: ch.Rows, Title: ch.Title, SkipPristine: ch.SkipPristine}
	if ch.Theme != nil {
		h.Theme, h.Palette = Colors(ch.Theme.Colors), ch.Theme.Palette
	}
	t := newTimeline(h)
	t.start = ch.Timestamp
	for {
		ce, rerr := cr.Next()
		switch {
		case errors.Is(rerr, io.EOF):
			return t, nil
		case rerr != nil:
			return nil, fmt.Errorf("reel: %w", rerr)
		}
		ev, derr := castDecoders[ce.Code](ce.Data)
		if derr != nil {
			return nil, fmt.Errorf("reel: asciicast line %d: %q event: %w", cr.Line(), ce.Code, derr)
		}
		t.appendAt(ev, ce.Time)
	}
}

// castCodes are the event codes reel reads, which castDecoders parse.
var castCodes = slices.Collect(maps.Keys(castDecoders))

// castDecoders parse the data of the event codes reel reads.
var castDecoders = map[string]func(data string) (Event, error){
	"o": func(data string) (Event, error) { return Output{Data: []byte(data)}, nil },
	"i": func(data string) (Event, error) { return Input{Data: []byte(data)}, nil },
	"m": func(data string) (Event, error) { return Mark{Label: data}, nil },
	"r": func(data string) (Event, error) {
		cols, rows, _ := strings.Cut(data, "x")
		c, cok := asciicast.ParseSize(cols)
		r, rok := asciicast.ParseSize(rows)
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
		c, err := asciicast.ParseColors(data)
		return Theme{Colors: Colors(c)}, err
	},
}
