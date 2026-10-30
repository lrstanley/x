// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"fmt"
	"image/color"
	"iter"
	"slices"
	"sync"
	"time"
)

// ErrNoMark is wrapped by every [NoMarkError], such as from
// [Timeline.Between].
var ErrNoMark = errors.New("reel: no such mark")

// NoMarkError is the error for a mark a timeline lacks, such as one a
// recording never reached; it wraps [ErrNoMark]. Every missing-mark error
// from reel and its effects wraps one, so [errors.As] gives the label. An
// [Effect] of a caller's own that looks for marks should return one too, for
// [MissingMarks] to find.
type NoMarkError struct {
	// Label is the label of the missing mark.
	Label string
}

func (e *NoMarkError) Error() string { return fmt.Sprintf("%v: %q", ErrNoMark, e.Label) }
func (e *NoMarkError) Unwrap() error { return ErrNoMark }

// MissingMarks splits err, such as from [Export] or [Recording.Stop], into
// the labels of the marks it reports missing, in order and each once, and
// the rest of it (nil if nothing else). It looks through joined errors at
// any depth.
func MissingMarks(err error) (labels []string, rest error) {
	var errs []error
	var split func(error)
	split = func(err error) {
		if j, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range j.Unwrap() {
				split(e)
			}
			return
		}
		var nm *NoMarkError
		switch {
		case err == nil:
		case errors.As(err, &nm):
			if !slices.Contains(labels, nm.Label) {
				labels = append(labels, nm.Label)
			}
		default:
			errs = append(errs, err)
		}
	}
	split(err)
	return labels, errors.Join(errs...)
}

// Timeline is the event log of a [Session]: what the program wrote, what it
// was sent, and what the host did to its terminal, each at its offset from
// the first event. Recordings are derived by replaying it, so it holds no
// frames.
//
// A session's timeline grows while the session runs. Methods are safe for
// concurrent use and see only events appended before the call; iterators do
// not see later appends. Events and their byte slices are never modified
// once appended, and callers must not modify them either.
//
// Offsets are whole milliseconds (asciicast's precision): each is rounded
// when appended, and raised to the previous event's, so offsets never
// decrease. [Export] therefore writes the same files from a timeline as from
// its saved asciicast file, except for invalid UTF-8 output (saved as
// U+FFFD) and cuts, which are saved as their redrawn window (see
// [Timeline.WriteCast]).
type Timeline struct {
	header Header

	// mu guards start, events, updated, and paced. Readers copy the slice
	// header under it and read elements without it, which is safe because
	// appends never change existing elements.
	mu sync.RWMutex
	// start is the wall time offsets are measured from; zero until the
	// first wall-clock append. Unpacing moves it so the wall clock resumes
	// from where the paced stretch ended.
	start  time.Time
	events []Event
	// from is the index of a [Timeline.Between] window's first event, or -1
	// when the window starts at the beginning.
	from int
	// updated is created by [Timeline.Updated] and closed and cleared by the
	// next append.
	updated chan struct{}
	// paced is set while a timed [Attach] owns the clock: appends are
	// stamped at pacedAt rather than by the wall clock.
	paced   bool
	pacedAt time.Duration
}

// newTimeline returns an empty timeline with header.
func newTimeline(header Header) *Timeline {
	return &Timeline{header: header, from: -1}
}

// Header describes the terminal a timeline starts with.
type Header struct {
	// Cols and Rows are the initial terminal size in cells.
	Cols, Rows int
	// Theme is the terminal's initial colors. Fg and Bg are set unless the
	// timeline was read from a file without them; Cursor is nil when no
	// cursor color was set.
	Theme Colors
	// Palette is the 16 initial ANSI colors (8 normal, then bright). A nil
	// entry means the renderer's default. Sessions leave every entry nil;
	// a file's palette fills it (an 8-color palette sets the first 8).
	Palette [16]color.Color
	// Title names the recording; it is not the window title the program
	// sets.
	Title string
	// SkipPristine makes a replay draw no frame while the terminal still
	// looks as it did when opened (blank screen, same title, colors,
	// cursor, progress, and status), so a recording starts on the
	// program's first paint rather than the bare terminal. It is off by
	// default because programs that start on a blank screen on purpose,
	// such as a shell with an empty prompt, want that frame shown. Set it
	// for programs that write capability queries before their first paint,
	// as Bubble Tea programs do.
	SkipPristine bool
	// Scrollback is how many lines of history the terminal keeps. Zero
	// keeps none. A session's is [vt.DefaultScrollback] unless opened
	// [WithScrollback], as is that of a file that does not say.
	Scrollback int
	// CellWidth and CellHeight are the terminal's pixel size of one cell
	// (see [vt.WithCellSize]), which sizes Kitty graphics in cells, so a
	// replay places images over the cells they were recorded on. Zero is
	// vt's default. A session's are set [WithCellSize].
	CellWidth, CellHeight int
	// KittyStorage is how many bytes of decoded Kitty graphics the terminal
	// keeps (see [vt.WithKittyStorage]), so a replay drops the same images
	// when it runs out. Zero is vt's default. A session's is set
	// [WithKittyStorage].
	KittyStorage uint64
}

// Colors is a foreground, background, and cursor color. Non-nil colors are
// [color.NRGBA].
type Colors struct {
	Fg, Bg, Cursor color.Color
}

// Event is one entry in a [Timeline]: [Output], [Input], [Resize], [Mark],
// [Focus], [Scroll], [ScrollbackSize], [Theme], [Exit], or [Annotation]. The
// set is closed.
type Event interface {
	// At is the event's offset from the first event in its timeline.
	At() time.Duration

	// stamp returns a copy of the event at offset at.
	stamp(at time.Duration) Event
}

// event holds every [Event]'s offset.
type event struct{ at time.Duration }

// At is the event's offset from the first event in its timeline.
func (e event) At() time.Duration { return e.at }

// Output is bytes the program wrote to its terminal.
type Output struct {
	event
	// Data is the bytes, as written. It must not be modified.
	Data []byte
}

// Input is bytes the program was sent as input (keys, text, pastes, or mouse
// events), encoded per the program's modes at the time. [vt.DecodeInput]
// decodes it.
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
	// Label is never empty, unless read from a file with an unlabeled
	// marker.
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

// ScrollbackSize is the host changing how many lines of history the terminal
// keeps. It is not sent to the program.
type ScrollbackSize struct {
	event
	// Lines is the most lines of history kept after the change. Zero keeps
	// none.
	Lines int
}

// Theme is the host changing the terminal's colors.
type Theme struct {
	event
	// Colors is the full set of host colors after the change. A nil color
	// is one the host no longer sets, so the terminal's default or the
	// program's color applies.
	Colors
}

// Exit is the program exiting.
type Exit struct {
	event
	Status int
}

func (e Output) stamp(at time.Duration) Event         { e.at = at; return e }
func (e Input) stamp(at time.Duration) Event          { e.at = at; return e }
func (e Resize) stamp(at time.Duration) Event         { e.at = at; return e }
func (e Mark) stamp(at time.Duration) Event           { e.at = at; return e }
func (e Focus) stamp(at time.Duration) Event          { e.at = at; return e }
func (e Scroll) stamp(at time.Duration) Event         { e.at = at; return e }
func (e ScrollbackSize) stamp(at time.Duration) Event { e.at = at; return e }
func (e Theme) stamp(at time.Duration) Event          { e.at = at; return e }
func (e Exit) stamp(at time.Duration) Event           { e.at = at; return e }

func (e Annotation) stamp(at time.Duration) Event { e.at = at; return e }

// append adds ev at the wall clock's offset from start, or at pacedAt while
// paced.
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

// now returns the offset append would stamp an event with now, without
// starting the clock.
func (t *Timeline) now() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t.paced:
		return t.pacedAt
	case t.start.IsZero():
		return 0
	}
	return time.Since(t.start)
}

// follow appends src's event ev at its own offset, adopting src's start, so
// t holds a prefix of src.
func (t *Timeline) follow(src *Timeline, ev Event) {
	start := src.started()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.start = start
	t.appendLocked(ev, ev.At())
}

// pace makes append stamp events at the offset given to setPace until
// unpace, and returns the clock's current offset to count from. It reports
// false if already paced.
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

// setPace sets the offset append stamps events at while paced.
func (t *Timeline) setPace(at time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pacedAt = at
}

// unpace returns append to the wall clock, continuing from the last paced
// offset. A timeline with no events yet starts fresh on the next append.
func (t *Timeline) unpace() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paced = false
	if len(t.events) > 0 {
		t.start = time.Now().Add(-t.pacedAt)
	}
}

// appendAt adds ev at offset at, for times not from the wall clock.
func (t *Timeline) appendAt(ev Event, at time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.appendLocked(ev, at)
}

// appendLocked adds ev at at, rounded to the millisecond and clamped to at
// least zero and the last event's offset.
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

// Updated returns a channel that is closed when the next event is
// appended. Take the channel before reading events so none are missed:
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
// For a timeline that does not grow, the channel is never closed.
func (t *Timeline) Updated() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.updated == nil {
		t.updated = make(chan struct{})
	}
	return t.updated
}

// fixed returns a non-growing copy of t with header h, holding its first n
// events (all if n is negative) and keeping its window.
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

// started returns the wall time offsets count from, or zero. When called
// after [Timeline.snapshot], it is set if any snapshotted event was
// appended by the wall clock.
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

// Window returns the span a recording shows: the whole timeline, or for one
// from [Timeline.Between], its start mark to its end.
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

// EventsFrom returns an iterator over the events from index i on, oldest
// first, as of the call. Passing the number already read reads only newer
// events; an i past the end yields nothing.
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

// eventsOf iterates t's events of type E as of the call.
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
// to the first mark labeled to after it. An empty from or to means the
// start or end of t's window. Marks are only searched within t's window
// (its start mark included), so Between on the result narrows it further.
// When from and to are the same label, the window runs from its first mark
// to its second.
//
// The result keeps every event up to the end, including those before the
// start, which are replayed to reach the window's starting state. Its
// Events, Inputs, Marks, Len, and Duration cover all of them;
// [Timeline.Window] reports the span to show. The result does not grow,
// even if t does. See [Timeline.WriteCast] for how it is saved.
//
// It returns a [NoMarkError] if either label is not found.
func (t *Timeline) Between(from, to string) (*Timeline, error) {
	events := t.snapshot()
	start, end := t.from, len(events)-1
	if from != "" {
		i := findMark(events, from, start, end)
		if i < 0 {
			return nil, &NoMarkError{Label: from}
		}
		start = i
	}
	if to != "" {
		i := findMark(events, to, start+1, end)
		if i < 0 {
			return nil, fmt.Errorf("%w after the start of the window", &NoMarkError{Label: to})
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
