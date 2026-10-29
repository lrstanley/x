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

// CastMediaType is the media type of asciicast files (extension ".cast").
const CastMediaType = "application/x-asciicast"

// CastVersion is an asciicast format version for [Timeline.WriteCast].
type CastVersion int

// Asciicast versions. Both round-trip every event and header field of a
// timeline.
const (
	// CastV2 is asciicast v2, the default, which every asciinema player
	// plays.
	CastV2 CastVersion = 2
	// CastV3 is asciicast v3. Players older than asciinema 3 and
	// asciinema-player 3.10 cannot play it.
	CastV3 CastVersion = 3
)

// castTermType is the terminal type written to headers (the emulator is
// libghostty-vt).
const castTermType = "xterm-ghostty"

var _ io.WriterTo = (*Timeline)(nil)

// WriteTo is [Timeline.WriteCast] with [CastV2].
func (t *Timeline) WriteTo(w io.Writer) (int64, error) {
	return t.WriteCast(w, CastV2)
}

// WriteCast writes the events appended so far to w as asciicast version v,
// and returns the number of bytes written. [ReadTimeline] reads it back.
//
// A timeline from [Timeline.Between] is written as just its window: the
// events before it are replaced by one output event that redraws the screen
// at the window's start (see [vt.Terminal.Redraw]), plus theme and focus
// events if they changed the host colors or focus. The header has the size
// at the window's start, and times and the header timestamp count from it.
// Only the visible screen is redrawn, so a cut that starts on the alternate
// screen and later leaves it shows a blank primary screen; [Export] of the
// cut is unaffected.
//
// Times are written exactly in whole milliseconds; v3 intervals sum to the
// absolute times, so long recordings do not drift.
//
// [Output] and [Input] bytes are written as JSON strings. A UTF-8 character
// split across two events of the same kind is moved whole into the later
// one, so bytes read back match bytes written. Invalid UTF-8 (such as X10
// mouse reports past column 94) is written as U+FFFD, as asciinema does.
//
// The header holds the size, title, and first event's time. Its colors,
// including the 16-color palette (nil entries filled with reel's defaults)
// and cursor color, are written only when [Header.Theme] has Fg and Bg set.
// [Header.SkipPristine] and [Header.Scrollback] are written as reel's own
// "skip_pristine" and "scrollback" fields, the latter unless it is
// [vt.DefaultScrollback].
//
// Events use asciicast's codes: output "o", input "i", resize "r"
// ("COLSxROWS"), marker "m", and exit "x" (standard in v3, reel's own in v2).
// Reel adds focus "f" ("1" or "0"), scroll "s" (the rows the viewport is
// scrolled up from the bottom), scrollback size "h" (the lines of history
// kept), theme "t"
// ("fg=#rrggbb,bg=#rrggbb,cursor=#rrggbb", omitting nil colors), and
// [Annotation] "R". Other readers ignore reel's own fields and events.
//
// Output is laid out for the terminal size at the time it was written. A
// player that writes it to its own terminal, such as "asciinema play", only
// shows it correctly at the same size; asciinema-player sizes its terminal
// from the file, so it is unaffected.
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
		err = writeCastEvents(aw, events)
	}
	if err == nil {
		err = bw.Flush()
	}
	return cw.n, err
}

// cut returns t's window as a standalone timeline, with the events before
// it collapsed into a redraw (and theme/focus events) at offset zero, as
// [Timeline.WriteCast] describes.
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
			switch ev := ev.(type) {
			case Theme:
				theme = &ev
			case ScrollbackSize:
				h.Scrollback = ev.Lines
			}
		}
		redraw, err := r.term.Redraw()
		if err != nil {
			return nil, err
		}
		h.Cols, h.Rows = r.term.Size()
		out = append(out, Output{Data: redraw})
		if offset := r.term.ScrollbackOffset(); offset > 0 {
			out = append(out, Scroll{Offset: offset})
		}
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

// castHeaderOf returns the asciicast header for h, whose first event was at
// start.
func castHeaderOf(h Header, start time.Time, v CastVersion) asciicast.Header {
	ch := asciicast.Header{
		Version: int(v), Cols: h.Cols, Rows: h.Rows, Term: castTermType,
		Timestamp: start, Title: h.Title, SkipPristine: h.SkipPristine,
	}
	if h.Scrollback != vt.DefaultScrollback {
		ch.Scrollback = &h.Scrollback
	}
	if h.CellWidth > 0 && h.CellHeight > 0 {
		ch.CellWidth, ch.CellHeight = h.CellWidth, h.CellHeight
	}
	if h.Theme.Fg != nil && h.Theme.Bg != nil {
		ch.Theme = &asciicast.Theme{Colors: asciicast.Colors(h.Theme), Palette: castPalette(h.Palette)}
	}
	return ch
}

// castPalette returns p with nil entries replaced by the default indexed
// colors.
func castPalette(p [16]color.Color) [16]color.Color {
	for i, c := range p {
		if c == nil {
			p[i] = vt.Indexed(i)
		}
	}
	return p
}

// writeCastEvents writes one line per event.
func writeCastEvents(aw *asciicast.Writer, events []Event) error {
	// The last Output and Input keep any trailing partial character, since
	// no later event can complete it.
	lastOutput, lastInput := -1, -1
	for i, ev := range events {
		switch ev.(type) {
		case Output:
			lastOutput = i
		case Input:
			lastInput = i
		}
	}
	var outputTail, inputTail []byte

	for i, ev := range events {
		var code, data string
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
			code, data = "f", "0"
			if ev.On {
				data = "1"
			}
		case Scroll:
			code, data = "s", strconv.Itoa(ev.Offset)
		case ScrollbackSize:
			code, data = "h", strconv.Itoa(ev.Lines)
		case Theme:
			code, data = "t", asciicast.FormatColors(asciicast.Colors(ev.Colors))
		case Exit:
			code, data = "x", strconv.Itoa(ev.Status)
		case Annotation:
			// Already validated when it entered the timeline.
			b, err := ev.encode()
			if err != nil {
				return err
			}
			code, data = annotationCode, string(b)
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

// ReadTimeline reads an asciicast v2 or v3 file, such as one written by
// [Timeline.WriteCast] or recorded by asciinema. The returned timeline does
// not grow.
//
// Comment ("#") and blank lines, and unknown header fields and event codes,
// are skipped. Event times are rounded to the millisecond from the start, so
// finer intervals do not drift, and a time earlier than the previous event's
// is raised to it. A marker with no label reads as a [Mark] with an empty
// Label, which [Timeline.Between] cannot cut at.
//
// Errors name the offending line. It is an error for the file to be
// asciicast v1, to have a header without a size or with a malformed known
// field, to have a line that is not a [time, code, data] array, or to have
// a known event with malformed data (including annotations
// [AnnotationSchema] rejects). Sizes are limited to 65535 and times to one
// year.
func ReadTimeline(r io.Reader) (*Timeline, error) {
	cr, err := asciicast.NewReader(r, castCodes...)
	if err != nil {
		return nil, fmt.Errorf("reel: %w", err)
	}
	ch := cr.Header
	h := Header{
		Cols: ch.Cols, Rows: ch.Rows, Title: ch.Title,
		SkipPristine: ch.SkipPristine, Scrollback: vt.DefaultScrollback,
		CellWidth: ch.CellWidth, CellHeight: ch.CellHeight,
	}
	if ch.Scrollback != nil {
		h.Scrollback = *ch.Scrollback
	}
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

// castCodes are the event codes reel reads.
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
	"s": func(data string) (Event, error) {
		offset, err := strconv.Atoi(data)
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("scroll offset %q is not a row count", data)
		}
		return Scroll{Offset: offset}, nil
	},
	"h": func(data string) (Event, error) {
		lines, err := strconv.Atoi(data)
		if err != nil || lines < 0 {
			return nil, fmt.Errorf("scrollback size %q is not a line count", data)
		}
		return ScrollbackSize{Lines: lines}, nil
	},
	"t": func(data string) (Event, error) {
		c, err := asciicast.ParseColors(data)
		return Theme{Colors: Colors(c)}, err
	},
	annotationCode: func(data string) (Event, error) { return parseAnnotation(data) },
}
