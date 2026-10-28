// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"fmt"
	"image/color"
	"iter"
	"sync"
	"time"
)

// ErrNoMark is returned by [Timeline.Between] when a label names no mark in
// the timeline's window.
var ErrNoMark = errors.New("reel: no such mark")

// Timeline is the event log of a [Session]: what the program wrote, what the
// session sent it, and what the host did to its terminal, each at its offset
// from the first event. Every recording is derived from a timeline by replaying it into
// a fresh terminal, so it holds no frames and does no rendering. Appending
// copies the bytes once.
//
// A session's timeline grows while the session runs. Its methods are safe for
// concurrent use, and each observes the events appended before it was called:
// an iterator does not see events appended after the call that returned it.
// Events are never changed once appended. Their byte slices must not be
// modified.
//
// Offsets are whole milliseconds, the precision of an asciicast file: an
// event's time is rounded to the millisecond as it is appended, whether it
// comes from the wall clock, a timing file, or a file being read, and raised
// to the offset of the event before, so offsets never decrease. [Export]
// therefore writes the same files from a timeline and from the asciicast file
// [Timeline.WriteTo] saved of it, except where the file holds something else:
// output bytes that are not UTF-8, which it saves as U+FFFD (see
// [Timeline.WriteCast]), and a cut that starts at a mark, which it saves as
// its window, redrawn.
//
// [Timeline.WriteTo] saves a timeline as an asciicast file, which asciinema
// and the tools around it play, and [ReadTimeline] reads one back.
type Timeline struct {
	// header is fixed when the timeline is created.
	header Header

	// mu guards start, events, and paced. Readers copy the slice header
	// under it and read the elements without it, which is safe because
	// appends never change an element already in the slice.
	mu sync.RWMutex
	// start is when the first event was appended with the wall clock, which
	// event offsets are measured from. It is zero until then. A paced
	// stretch moves it, so the wall clock carries on from where the
	// stretch ended.
	start time.Time
	// events are in the order they were appended, with offsets that never
	// decrease.
	events []Event
	// from is the index of the event a [Timeline.Between] window starts at,
	// or -1 when it starts before the first event. A timeline that is not a
	// window has from -1 and ends at its last event.
	from int
	// updated is closed, and cleared, by the next append. It is created by
	// [Timeline.Updated], and guarded by mu.
	updated chan struct{}
	// paced is set while a timed [Attach] owns the clock: append stamps
	// events at pacedAt, the offset it set last, rather than by the wall
	// clock.
	paced   bool
	pacedAt time.Duration
}

// newTimeline returns an empty timeline with header.
func newTimeline(header Header) *Timeline {
	return &Timeline{header: header, from: -1}
}

// Header describes the terminal a timeline starts with.
type Header struct {
	// Cols and Rows are the terminal size in cells when the session opened.
	// [Resize] events change it.
	Cols, Rows int
	// Theme is the colors the terminal showed when the session opened. Fg
	// and Bg are set, unless the timeline was read from a file without
	// them. Cursor is nil when no cursor color was set. A fresh terminal
	// shows the same colors, so a replay need not apply them; [Theme]
	// events then set the host's colors over them.
	Theme Colors
	// Palette is the 16 ANSI colors the terminal starts with, the 8 normal
	// colors and then their bright variants. A nil entry is the renderer's
	// default for its index. A session leaves every entry nil; a timeline
	// read from a file with a palette has its colors, and an 8 color palette
	// sets the first 8.
	Palette [16]color.Color
	// Title is a name for the recording, which is not the window title the
	// program sets. It is empty unless [WithTitle] set it, or the file the
	// timeline was read from had one.
	Title string
	// SkipPristine makes a replay draw no frame while the terminal still
	// looks as it did before anything was written to it: a blank screen
	// with the title, colors, cursor, progress, and status it opened with.
	// A Bubble Tea program starts by writing capability queries, which
	// change nothing, so without it a recording would open on a frame of
	// the bare terminal before the program's first paint. A program that
	// starts on a blank screen on purpose, such as a shell with an empty
	// prompt, wants its first frame shown, so it is unset unless
	// [WithSkipPristine] set it, or the file the timeline was read from
	// had it.
	SkipPristine bool
}

// Colors is a foreground, background, and cursor color. Non-nil colors are
// [color.NRGBA].
type Colors struct {
	Fg, Bg, Cursor color.Color
}

// Event is one entry in a [Timeline]: [Output], [Input], [Resize], [Mark],
// [Focus], [Scroll], [Theme], [Exit], or [Annotation]. The set is closed.
type Event interface {
	// At is the event's offset from the first event in its timeline.
	At() time.Duration

	// stamp returns a copy of the event at offset at.
	stamp(at time.Duration) Event
}

// event holds the offset every [Event] carries.
type event struct{ at time.Duration }

// At is the event's offset from the first event in its timeline.
func (e event) At() time.Duration { return e.at }

// Output is bytes the program wrote to its terminal.
type Output struct {
	event
	// Data is the bytes, as written. It must not be modified.
	Data []byte
}

// Input is bytes the program was sent as input: a key, typed text, a paste,
// or a mouse event, encoded as the terminal sends it in the modes the program
// had set. [vt.DecodeInput] decodes it.
type Input struct {
	event
	// Data is the bytes, as sent. It must not be modified.
	Data []byte
}

// Resize is the terminal changing size.
type Resize struct {
	event
	// Cols and Rows are the new size in cells.
	Cols, Rows int
}

// Mark is a named point in a timeline, which [Timeline.Between] cuts at.
type Mark struct {
	event
	// Label is the mark's name. It is never empty, unless the mark was read
	// from a file with an unlabeled marker.
	Label string
}

// Focus is the terminal window gaining or losing focus. A session starts
// focused.
type Focus struct {
	event
	// On is true when the window gained focus.
	On bool
}

// Scroll is the host scrolling the terminal's viewport through its scrollback.
// It is not sent to the program.
type Scroll struct {
	event
	// Offset is how many rows the viewport is scrolled up from the bottom,
	// after the change. Zero is the live screen.
	Offset int
}

// Theme is the host changing the terminal's colors.
type Theme struct {
	event
	// Colors is the colors the host set, after the change. A nil color is
	// one the host no longer sets, so the terminal's default, or the color
	// the program set, applies.
	Colors
}

// Exit is the program exiting.
type Exit struct {
	event
	// Status is the program's exit status.
	Status int
}

func (e Output) stamp(at time.Duration) Event { e.at = at; return e }
func (e Input) stamp(at time.Duration) Event  { e.at = at; return e }
func (e Resize) stamp(at time.Duration) Event { e.at = at; return e }
func (e Mark) stamp(at time.Duration) Event   { e.at = at; return e }
func (e Focus) stamp(at time.Duration) Event  { e.at = at; return e }
func (e Scroll) stamp(at time.Duration) Event { e.at = at; return e }
func (e Theme) stamp(at time.Duration) Event  { e.at = at; return e }
func (e Exit) stamp(at time.Duration) Event   { e.at = at; return e }

func (e Annotation) stamp(at time.Duration) Event { e.at = at; return e }

// append adds ev at the wall clock's offset from the first event appended
// this way, or while the timeline is paced, at the offset of the pace.
func (t *Timeline) append(ev Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.paced {
		t.appendLocked(ev, t.pacedAt)
		return
	}
	now := time.Now()
	if t.start.IsZero() {
		t.start = now
	}
	t.appendLocked(ev, now.Sub(t.start))
}

// follow appends ev, an event of src, at its offset, with src's start, so
// t holds a prefix of src.
func (t *Timeline) follow(src *Timeline, ev Event) {
	start := src.started()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.start = start
	t.appendLocked(ev, ev.At())
}

// pace makes append stamp events at the offsets passed to setPace rather
// than by the wall clock, until unpace, and returns the offset the clock
// has reached, which they count from. It reports false when the timeline
// is paced already.
func (t *Timeline) pace() (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.paced {
		return 0, false
	}
	t.paced, t.pacedAt = true, lastAt(t.events)
	if !t.start.IsZero() {
		t.pacedAt = max(t.pacedAt, time.Since(t.start))
	}
	return t.pacedAt, true
}

// setPace sets the offset append stamps events at while the timeline is
// paced.
func (t *Timeline) setPace(at time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pacedAt = at
}

// unpace returns append to the wall clock, which carries on from the last
// offset of the pace. A timeline still without events starts with the next
// one, as a new one does.
func (t *Timeline) unpace() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paced = false
	if len(t.events) > 0 {
		t.start = time.Now().Add(-t.pacedAt)
	}
}

// appendAt adds ev at offset at, for events whose times come from elsewhere
// than the wall clock, such as a timing file.
func (t *Timeline) appendAt(ev Event, at time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.appendLocked(ev, at)
}

// appendLocked adds ev at offset at rounded to the millisecond, and raised
// to zero and to the last event's offset, so offsets never decrease.
func (t *Timeline) appendLocked(ev Event, at time.Duration) {
	at = max(at.Round(time.Millisecond), 0)
	if n := len(t.events); n > 0 {
		at = max(at, t.events[n-1].At())
	}
	t.events = append(t.events, ev.stamp(at))
	if t.updated != nil {
		close(t.updated)
		t.updated = nil
	}
}

// Updated returns a channel that is closed once an event is appended after
// the call, so a reader of a growing timeline can wait for more events
// without polling. Taking the channel before reading the events makes sure
// no event is missed:
//
//	read := 0
//	for {
//		updated := tl.Updated()
//		for ev := range tl.EventsFrom(read) {
//			read++
//			// Use ev.
//		}
//		<-updated
//	}
//
// The channel of a timeline that does not grow is never closed.
func (t *Timeline) Updated() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.updated == nil {
		t.updated = make(chan struct{})
	}
	return t.updated
}

// fixed returns a timeline with header h that holds the first n events of t
// as of the call, or all of them when n is negative, and keeps t's window.
// It does not grow.
func (t *Timeline) fixed(h Header, n int) *Timeline {
	events := t.snapshot()
	if n >= 0 {
		events = events[:n:n]
	}
	return &Timeline{header: h, start: t.started(), events: events, from: t.from}
}

// snapshot returns the events appended so far. The caller must not modify
// the slice.
func (t *Timeline) snapshot() []Event {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.events[:len(t.events):len(t.events)]
}

// started returns when the first event was appended with the wall clock, or
// the zero time. Read after [Timeline.snapshot], it is set for any events
// that were.
func (t *Timeline) started() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.start
}

// Header returns the terminal the timeline starts with.
func (t *Timeline) Header() Header { return t.header }

// Len returns the number of events.
func (t *Timeline) Len() int { return len(t.snapshot()) }

// Duration returns the offset of the last event, or zero when there is none.
func (t *Timeline) Duration() time.Duration { return lastAt(t.snapshot()) }

// Window returns the span a recording of the timeline shows: from the start
// of the timeline to its last event, or for a timeline returned by
// [Timeline.Between], from its start mark to its end.
func (t *Timeline) Window() (from, to time.Duration) {
	events := t.snapshot()
	if t.from >= 0 {
		from = events[t.from].At()
	}
	return from, lastAt(events)
}

func lastAt(events []Event) time.Duration {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].At()
}

// Events returns an iterator over the events, oldest first.
func (t *Timeline) Events() iter.Seq[Event] {
	return eventsOf[Event](t)
}

// EventsFrom returns an iterator over the events from the one at index i
// on, oldest first, as of the call. A reader of a growing timeline can pass
// the number of events it has read, as [Timeline.Len] counts them, to read
// only those appended since. An i past the end is no events.
func (t *Timeline) EventsFrom(i int) iter.Seq[Event] {
	events := t.snapshot()
	events = events[min(max(i, 0), len(events)):]
	return func(yield func(Event) bool) {
		for _, ev := range events {
			if !yield(ev) {
				return
			}
		}
	}
}

// Inputs returns an iterator over the [Input] events, oldest first.
func (t *Timeline) Inputs() iter.Seq[Input] {
	return eventsOf[Input](t)
}

// Annotations returns an iterator over the [Annotation] events, oldest
// first.
func (t *Timeline) Annotations() iter.Seq[Annotation] {
	return eventsOf[Annotation](t)
}

// Marks returns an iterator over the [Mark] events, oldest first.
func (t *Timeline) Marks() iter.Seq[Mark] {
	return eventsOf[Mark](t)
}

// eventsOf returns an iterator over the events in t of type E, as of the
// call.
func eventsOf[E Event](t *Timeline) iter.Seq[E] {
	events := t.snapshot()
	return func(yield func(E) bool) {
		for _, ev := range events {
			if e, ok := ev.(E); ok && !yield(e) {
				return
			}
		}
	}
}

// Between returns the part of the timeline from the first mark labeled from
// to the first mark labeled to after it, for recording one stretch of a
// session. An empty from starts at the start of t's window, and an empty to
// ends at its end. Marks are only looked for inside t's window, its start mark
// included, so calling Between on the result narrows it further.
//
// The result keeps every event up to the end, including those before the
// start, because replaying them is what puts the terminal in the state the
// window starts in. Its Events, Inputs, Marks, Len, and Duration cover all of
// them; [Timeline.Window] reports the span to show. Its events are fixed as of
// the call, even when t is still growing. See [Timeline.WriteCast] for how
// it is written as a file.
//
// Between returns an error wrapping [ErrNoMark] when either label names no
// mark in the window. When from and to are the same label, the window runs
// from its first mark to its second.
func (t *Timeline) Between(from, to string) (*Timeline, error) {
	events := t.snapshot()
	start, end := t.from, len(events)-1
	if from != "" {
		i := findMark(events, from, start, end)
		if i < 0 {
			return nil, fmt.Errorf("%w: %q", ErrNoMark, from)
		}
		start = i
	}
	if to != "" {
		i := findMark(events, to, start+1, end)
		if i < 0 {
			return nil, fmt.Errorf("%w: %q after the start of the window", ErrNoMark, to)
		}
		end = i
	}
	return &Timeline{
		header: t.header,
		start:  t.started(),
		events: events[: end+1 : end+1],
		from:   start,
	}, nil
}

// findMark returns the index of the first mark labeled label in
// events[lo:hi+1], or -1.
func findMark(events []Event, label string, lo, hi int) int {
	for i := max(lo, 0); i <= hi; i++ {
		if m, ok := events[i].(Mark); ok && m.Label == label {
			return i
		}
	}
	return -1
}
