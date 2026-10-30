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
)

// Replays use their own terminal pools so they never wait on running
// sessions, and exports and cuts never wait on running recordings.
// replayRuntime lends terminals for one export, which holds no other while
// it waits, so its limit cannot deadlock. recordRuntime lends them for the
// life of a [Record], which its caller holds for as long as it chooses, so
// it has no limit, as sessions do.
var (
	replayRuntime = vt.NewRuntime(0)
	recordRuntime = vt.NewRuntime(-1)
)

// replay feeds a timeline's events into a fresh terminal one at a time and
// passes the frames of the timeline's window to frame. Events before the
// window are applied too, to reach the state the window starts in.
type replay struct {
	term *vt.Terminal
	// base is the header colors applied as terminal defaults: nil where the
	// header has none or matches the terminal's own.
	base    Colors
	focused bool

	// skipPristine drops frames that look like a fresh terminal (pristine)
	// until the first real frame, per [Header.SkipPristine].
	skipPristine bool
	pristine     vt.State

	// sent is the last frame's state; an event must change its look (see
	// [vt.State.SameLook]), including focus or colors, to make a new frame.
	sent     vt.State
	haveSent bool

	// now is the offset of the event being applied: the terminal's clock,
	// so program events are stamped on the timeline.
	now time.Duration

	// start is the index of the window's first event, from its offset.
	start int
	from  time.Duration
	// n is the number of events applied.
	n int
	// early is set when a frame occurred before the window; the window then
	// opens on the terminal at its start, held in opening until the
	// terminal changes again.
	early   bool
	opening *CellFrame
	emitted bool

	// frame receives the window's frames in order; if nil, the replay only
	// applies events.
	frame func(CellFrame) error
	// mark, if set, is called for each [Mark] in the window, after the
	// events before it are applied.
	mark func(Mark) error
}

// newReplay opens a terminal from rt for a timeline with header h whose
// window starts at event index start, offset from.
func newReplay(ctx context.Context, rt *vt.Runtime, h Header, start int, from time.Duration) (*replay, error) {
	r := &replay{focused: true, skipPristine: h.SkipPristine, start: start, from: from}
	term, err := rt.Open(ctx, h.Cols, h.Rows,
		vt.WithScrollbackLines(h.Scrollback),
		vt.WithClock(func() time.Duration { return r.now }),
		vt.WithCellSize(h.CellWidth, h.CellHeight),
		kittyStorage(h.KittyStorage),
	)
	if err != nil {
		return nil, err
	}
	// Nothing reads query replies.
	term.CloseReader()
	r.term = term
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

// kittyStorage is the vt option for [Header.KittyStorage] n, whose zero is
// vt's default.
func kittyStorage(n uint64) vt.Option {
	if n == 0 {
		return nil
	}
	return vt.WithKittyStorage(n)
}

func nrgba(c color.Color) color.NRGBA {
	n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
	return n
}

func (r *replay) close() error { return r.term.Close() }

// setColors applies a [Theme] event's colors, falling back to the header's.
func (r *replay) setColors(c Colors) error {
	if err := r.term.SetDefaultForeground(cmp.Or(c.Fg, r.base.Fg)); err != nil {
		return err
	}
	if err := r.term.SetDefaultBackground(cmp.Or(c.Bg, r.base.Bg)); err != nil {
		return err
	}
	return r.term.SetDefaultCursorColor(cmp.Or(c.Cursor, r.base.Cursor))
}

// apply applies the next event and passes on the resulting frame, if any.
func (r *replay) apply(ev Event) error {
	inWindow := r.n >= r.start
	if r.n == r.start {
		if err := r.animate(min(r.from, ev.At()), false); err != nil {
			return err
		}
		if r.early && r.frame != nil {
			// Capture what the pre-window events left as the opening frame.
			cf := r.current(r.from)
			r.opening = &cf
		}
	}
	if err := r.animate(ev.At(), inWindow); err != nil {
		return err
	}
	r.n++
	r.now = ev.At()
	var err error
	switch ev := ev.(type) {
	case Output:
		_, err = r.term.Write(ev.Data)
	case Resize:
		err = r.term.Resize(ev.Cols, ev.Rows)
	case Focus:
		r.focused = ev.On
	case Scroll:
		// The scrollback of a replay that started mid-timeline may be shorter
		// than the one the event was recorded against, so the offset clamps.
		err = r.term.ScrollTo(ev.Offset)
	case ScrollbackSize:
		err = r.term.SetScrollbackMaxLines(ev.Lines)
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

// animate passes on the Kitty animation frames due before until, each at
// the time it is due.
func (r *replay) animate(until time.Duration, inWindow bool) error {
	if r.frame == nil {
		return nil
	}
	for {
		due, ok := r.term.AnimationDue()
		// The terminal has already ticked at now, so a frame due then or
		// before is one it showed.
		if !ok || due >= until || due <= r.now {
			return nil
		}
		r.now = due
		r.term.Animate()
		if err := r.commit(due, inWindow); err != nil {
			return err
		}
	}
}

// state returns the terminal's state with the timeline's focus.
func (r *replay) state() vt.State {
	st := r.term.State()
	st.Focused = r.focused
	return st
}

// current returns the terminal as shown now, stamped at.
func (r *replay) current(at time.Duration) CellFrame {
	return CellFrame{Screen: r.term.Screen(), State: r.state(), At: at}
}

// commit passes on the terminal as a frame at at, unless it is in
// synchronized output, looks unchanged, or is still pristine.
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

// isPristine reports whether st and the screen text look like the fresh
// terminal.
func (r *replay) isPristine(st vt.State, text string) bool {
	p := r.pristine
	return strings.TrimSpace(text) == "" && len(r.term.Images()) == 0 &&
		st.Title == p.Title &&
		st.ShownProgress() == p.ShownProgress() &&
		st.ProgramStatusGeneration == p.ProgramStatusGeneration &&
		st.AltScreen == p.AltScreen &&
		st.Foreground == p.Foreground &&
		st.Background == p.Background &&
		st.Cursor == p.Cursor
}

// send passes on cf, preceded by the opening frame if cf comes after the
// window's start.
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

// finish ensures the recording has at least one frame: the pending opening
// frame, or else the current terminal if nothing was emitted.
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
