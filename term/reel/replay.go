// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"context"
	"image/color"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// Replays open their terminals from pools of their own, so a replay never
// waits on the sessions still running, and an export or a cut written as a
// file never waits on the recordings still running: replayRuntime lends
// terminals for the length of one export, and recordRuntime for the life of
// a [Record].
var (
	replayRuntime = vt.NewRuntime(0)
	recordRuntime = vt.NewRuntime(0)
)

// replay feeds the events of a timeline, one at a time, into a fresh
// terminal, and passes the frames a recording of the timeline's window shows
// to frame. Every event is applied, those before the window too, since they
// put the terminal in the state the window starts in.
type replay struct {
	term *vt.Terminal
	// base is the header colors set as the terminal's defaults: nil where
	// the header has none, or where it matches the terminal's own.
	base    Colors
	focused bool

	// skipPristine drops the frames that look like a terminal nothing has
	// drawn on yet, until the first frame. A Bubble Tea program starts by
	// writing capability queries, which commit without changing anything, so
	// recording them would show a frame of the default colors and the cursor
	// at 0,0 before the program's first paint. A program that legitimately
	// starts on a blank screen, such as a shell, wants it off.
	skipPristine bool
	pristine     screen.State

	// sent is the state of the last frame, which an event must change the
	// look of (see [screen.State.SameLook]) to make a frame of its own: a
	// change of focus or colors is one too, so blur dimming and new colors
	// show when they happen.
	sent     screen.State
	haveSent bool

	// start is the index of the window's first event, and from its offset.
	start int
	from  time.Duration
	// n is the number of events applied.
	n int
	// early is set when a frame changed the terminal before the window,
	// which then starts on the terminal as it is at its start, and opening
	// is that frame until the terminal changes again.
	early   bool
	opening *CellFrame
	// emitted is set once frame has been called.
	emitted bool

	// frame receives the frames of the window, in order. When it is nil,
	// the replay only applies events.
	frame func(CellFrame) error
	// mark, when set, is called for each [Mark] in the window, after the
	// events before it are applied.
	mark func(Mark) error
}

// newReplay opens a terminal from rt for a timeline that starts with h,
// whose window starts at the event with index start, at offset from.
func newReplay(ctx context.Context, rt *vt.Runtime, h Header, start int, from time.Duration) (*replay, error) {
	term, err := rt.Open(ctx, h.Cols, h.Rows)
	if err != nil {
		return nil, err
	}
	// Nothing reads the replies to the program's queries.
	term.CloseReader()
	r := &replay{term: term, focused: true, skipPristine: true, start: start, from: from}
	if c := h.Theme.Fg; c != nil && nrgba(c) != term.Foreground() {
		r.base.Fg = c
	}
	if c := h.Theme.Bg; c != nil && nrgba(c) != term.Background() {
		r.base.Bg = c
	}
	if c, ok := term.CursorColor(); h.Theme.Cursor != nil && (!ok || nrgba(h.Theme.Cursor) != c) {
		r.base.Cursor = h.Theme.Cursor
	}
	if r.base != (Colors{}) {
		if err = r.setColors(Colors{}); err != nil {
			_ = term.Close()
			return nil, err
		}
	}
	r.pristine = term.State()
	return r, nil
}

func nrgba(c color.Color) color.NRGBA {
	n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
	return n
}

func (r *replay) close() error { return r.term.Close() }

// setColors sets the colors a [Theme] event reports the host set, falling
// back to the header's.
func (r *replay) setColors(c Colors) error {
	if err := r.term.SetDefaultForeground(cmp.Or(c.Fg, r.base.Fg)); err != nil {
		return err
	}
	if err := r.term.SetDefaultBackground(cmp.Or(c.Bg, r.base.Bg)); err != nil {
		return err
	}
	return r.term.SetDefaultCursorColor(cmp.Or(c.Cursor, r.base.Cursor))
}

// apply applies the next event, and passes on the frame it shows, if any.
func (r *replay) apply(ev Event) error {
	inWindow := r.n >= r.start
	if r.n == r.start && r.early && r.frame != nil {
		// What the events before the window left, focus and colors
		// included, whose frames fall before it.
		cf := r.current(r.from)
		r.opening = &cf
	}
	r.n++
	var err error
	switch ev := ev.(type) {
	case Output:
		_, err = r.term.Write(ev.Data)
	case Resize:
		err = r.term.Resize(ev.Cols, ev.Rows)
	case Focus:
		r.focused = ev.On
	case Theme:
		err = r.setColors(ev.Colors)
	case Mark:
		if inWindow && r.mark != nil {
			return r.mark(ev)
		}
		return nil
	default:
		return nil
	}
	if err != nil {
		return err
	}
	return r.commit(ev.At(), inWindow)
}

// state returns the terminal's state, with the timeline's focus.
func (r *replay) state() screen.State {
	st := r.term.State()
	st.Focused = r.focused
	return st
}

// current returns the terminal as it is shown now, as of at.
func (r *replay) current(at time.Duration) CellFrame {
	return CellFrame{Screen: r.term.Screen(), State: r.state(), At: at}
}

// commit passes on the terminal as a frame at at, unless it is held in
// synchronized output, looks the same as the last frame, or is still
// pristine.
func (r *replay) commit(at time.Duration, inWindow bool) error {
	if r.frame == nil || r.term.Held() {
		return nil
	}
	st := r.state()
	if r.haveSent && r.sent.SameLook(st) {
		return nil
	}
	if r.skipPristine && !r.haveSent && r.isPristine(st, r.term.Text()) {
		return nil
	}
	r.haveSent, r.sent = true, st
	if !inWindow {
		r.early = true
		return nil
	}
	return r.send(CellFrame{Screen: r.term.Screen(), State: st, At: at})
}

// isPristine reports whether st and the screen text are indistinguishable
// from the terminal before anything was written to it.
func (r *replay) isPristine(st screen.State, text string) bool {
	p := r.pristine
	return strings.TrimSpace(text) == "" &&
		st.Title == p.Title &&
		st.ShownProgress() == p.ShownProgress() &&
		st.ProgramStatusGeneration == p.ProgramStatusGeneration &&
		st.AltScreen == p.AltScreen &&
		st.Foreground == p.Foreground &&
		st.Background == p.Background &&
		st.Cursor == p.Cursor
}

// send passes on cf, after the frame the window opens on when cf comes after
// the window's start.
func (r *replay) send(cf CellFrame) error {
	if o := r.opening; o != nil {
		r.opening = nil
		if cf.At > r.from {
			if err := r.frame(*o); err != nil {
				return err
			}
		}
	}
	r.emitted = true
	return r.frame(cf)
}

// finish passes on the frame the window opens on if nothing replaced it, or
// the terminal as it is now when the window has no frame at all, so a
// recording always has one.
func (r *replay) finish() error {
	switch {
	case r.frame == nil:
		return nil
	case r.opening != nil:
		cf := *r.opening
		r.opening = nil
		r.emitted = true
		return r.frame(cf)
	case !r.emitted:
		r.emitted = true
		return r.frame(r.current(r.from))
	}
	return nil
}
