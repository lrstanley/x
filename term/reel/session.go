// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"sync"
	"sync/atomic"

	"github.com/lrstanley/x/term/reel/internal/pty"
	"github.com/lrstanley/x/term/reel/vt"
)

// ErrClosed is returned by [Session] methods called after [Session.Close].
var ErrClosed = errors.New("reel: session is closed")

// defaultRuntime is the emulator pool sessions open from unless [WithRuntime]
// names another. Its size follows GOMAXPROCS, so parallel sessions stay
// within one Wasm instance per processor.
var defaultRuntime = vt.NewRuntime(0)

var _ io.ReadWriter = (*Session)(nil)

// Session owns a terminal emulator that a program runs in, and records what
// happens to it in a [Timeline]. The program writes its output with
// [Session.Write] and reads its input with [Session.Read]. The host sends it
// keys, text, pastes, and mouse events, resizes and focuses its window, sets
// its colors, and marks points to cut a recording at.
//
// Every change made through the session is appended to the timeline in the
// order the terminal saw it, so replaying the timeline into a fresh terminal
// reproduces it. The terminal's replies to the program's queries are not
// recorded as input, because replaying its output produces them again, but
// each query and its reply are recorded as a [QueryKind] annotation after
// the output that held the query, unless the session was opened
// [WithoutQueries]. Changes made through [Session.Terminal] are not
// recorded. Methods are safe for concurrent use.
type Session struct {
	term     *vt.Terminal
	timeline *Timeline

	// mu serializes each change to the terminal with the event that records
	// it, and Close with those changes.
	mu sync.Mutex
	// closed and focused are atomic so that Read, which blocks, and Focused
	// and State can read them without mu.
	closed  atomic.Bool
	focused atomic.Bool
	// pty is the pseudo-terminal of the process [Exec] runs on the session,
	// which Resize resizes too, until it hangs up. running is set from when
	// Exec starts the process until its exit is recorded. Both are guarded
	// by mu.
	pty     *pty.PTY
	running bool
	// scrollOnInput is whether Type, Key, and Paste scroll the viewport to the
	// bottom first, see [WithScrollOnInput].
	scrollOnInput bool
	// noQueries stops Write recording the program's queries.
	noQueries bool
}

// SessionOption configures [Open].
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	runtime       *vt.Runtime
	vtOpts        []vt.Option
	title         string
	skipPristine  bool
	scrollOnInput bool
	noQueries     bool
}

// WithRuntime opens the session's terminal from rt instead of the package's
// pool, which lends at most GOMAXPROCS terminals at once.
func WithRuntime(rt *vt.Runtime) SessionOption {
	return func(cfg *sessionConfig) {
		if rt != nil {
			cfg.runtime = rt
		}
	}
}

// WithScrollback sets how many lines of history the terminal keeps. Zero keeps
// none. A negative count keeps the default of 1000.
func WithScrollback(lines int) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.vtOpts = append(cfg.vtOpts, vt.WithScrollbackLines(lines))
	}
}

// WithTitle names the recording in its timeline's [Header].
func WithTitle(title string) SessionOption {
	return func(cfg *sessionConfig) { cfg.title = title }
}

// WithSkipPristine sets [Header.SkipPristine], so a recording of the
// session starts on the program's first paint rather than on the blank
// terminal it opened on. It is for programs that write capability queries
// before they draw, as Bubble Tea programs do.
func WithSkipPristine() SessionOption {
	return func(cfg *sessionConfig) { cfg.skipPristine = true }
}

// WithScrollOnInput sets whether [Session.Type], [Session.Key], and
// [Session.Paste] scroll the viewport back to the bottom first when it is
// scrolled up (see [Session.ScrollUp]), as a real terminal does when a key is
// pressed, so the program's response is in view. Enabled by default. The scroll
// is recorded as a [Scroll] event, before the [Input], and only when the
// viewport was scrolled, so typing at the bottom adds nothing. Mouse events
// never scroll, since the program may be handling them.
func WithScrollOnInput(enabled bool) SessionOption {
	return func(cfg *sessionConfig) { cfg.scrollOnInput = enabled }
}

// WithoutQueries stops the session recording a [QueryKind] annotation for
// each query the program writes, such as to keep the replies out of a
// recording: a reply to a clipboard read holds the clipboard's text. A
// query costs one small annotation, and programs write a few, mostly as they
// start, so they are recorded by default.
func WithoutQueries() SessionOption {
	return func(cfg *sessionConfig) { cfg.noQueries = true }
}

// Open starts a session on a terminal of cols by rows cells. It blocks until
// the terminal's pool has room or ctx is canceled. The session starts
// focused. Close it to return the terminal to the pool.
func Open(ctx context.Context, cols, rows int, opts ...SessionOption) (*Session, error) {
	cfg := sessionConfig{runtime: defaultRuntime, scrollOnInput: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	term, err := cfg.runtime.Open(ctx, cols, rows, cfg.vtOpts...)
	if err != nil {
		return nil, err
	}
	theme := Colors{Fg: term.Foreground(), Bg: term.Background()}
	if c, ok := term.CursorColor(); ok {
		theme.Cursor = c
	}
	h := Header{Cols: cols, Rows: rows, Theme: theme, Title: cfg.title, SkipPristine: cfg.skipPristine}
	s := &Session{
		term: term, timeline: newTimeline(h),
		scrollOnInput: cfg.scrollOnInput, noQueries: cfg.noQueries,
	}
	s.focused.Store(true)
	return s, nil
}

// Terminal returns the session's terminal, for reading its screen and state.
// Changes made through it directly are not recorded. It must not be used
// after [Session.Close], which returns it to its pool.
func (s *Session) Terminal() *vt.Terminal { return s.term }

// Timeline returns the session's event log. It is the same timeline for the
// life of the session, and grows as the session runs.
func (s *Session) Timeline() *Timeline { return s.timeline }

// Focused reports whether the terminal window has focus.
func (s *Session) Focused() bool { return s.focused.Load() }

// State returns the terminal's state, as [vt.Terminal.State] does, with
// Focused set from the session.
func (s *Session) State() vt.State {
	st := s.term.State()
	st.Focused = s.Focused()
	return st
}

// Write feeds program output to the terminal, after appending it to the
// timeline as an [Output] event, followed by a [QueryKind] annotation for
// each query it holds. It never returns a short write.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return 0, ErrClosed
	}
	if len(p) > 0 {
		s.timeline.append(Output{Data: bytes.Clone(p)})
	}
	if s.noQueries {
		return s.term.Write(p)
	}
	replies, err := s.term.WriteReplies(p)
	if err != nil {
		return 0, err
	}
	for _, q := range queries(p, replies) {
		s.timeline.append(queryAnnotation(q))
	}
	return len(p), nil
}

// Read reads the program's input: what the session sent it, and the
// terminal's replies to its queries. It blocks until there is some. It
// returns [io.EOF] once [vt.Terminal.CloseReader] is called and what was
// queued has been read, and at once after [Session.Close].
func (s *Session) Read(p []byte) (int, error) {
	return s.readContext(context.Background(), p)
}

// readContext is [Session.Read], which also ends when ctx is done.
func (s *Session) readContext(ctx context.Context, p []byte) (int, error) {
	if s.closed.Load() {
		return 0, io.EOF
	}
	return s.term.ReadContext(ctx, p)
}

// Close returns the terminal to its pool, unblocking [Session.Read] with
// [io.EOF]. The timeline stays readable. Close is safe to call more than
// once.
func (s *Session) Close() error {
	s.mu.Lock()
	closed := s.closed.Swap(true)
	s.mu.Unlock()
	if closed {
		return nil
	}
	return s.term.Close()
}

// change runs fn, which changes the terminal, with the session locked, and
// appends the event it returns, if any. Program output waits for the lock to
// be appended, so the event precedes any output the program writes in
// response to the change.
func (s *Session) change(fn func() (Event, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return ErrClosed
	}
	ev, err := fn()
	if err != nil {
		return err
	}
	if ev != nil {
		s.timeline.append(ev)
	}
	return nil
}

// send sends the program the input encode returns, and appends it as an
// [Input] event. Nothing is sent or appended when encode returns no bytes.
// With snap, and unless [WithScrollOnInput] turned it off, a scrolled viewport
// first goes back to the bottom, which appends a [Scroll] event before the
// input.
func (s *Session) send(encode func() ([]byte, error), snap bool) error {
	return s.change(func() (Event, error) {
		data, err := encode()
		if err != nil || len(data) == 0 {
			return nil, err
		}
		if snap && s.scrollOnInput && s.term.ScrollbackOffset() != 0 {
			if err = s.term.ScrollToBottom(); err != nil {
				return nil, err
			}
			s.timeline.append(Scroll{})
		}
		return Input{Data: data}, s.term.SendInput(data)
	})
}

// Type sends text as key presses, one per character, as one [Input] event.
// Each character is pressed as the key that types it on a US layout, with
// shift held for upper case letters and shifted symbols. Other characters,
// including control characters such as "\n", are sent as they are. Type is
// for text, not for keys with modifiers; see [Session.Key]. A scrolled
// viewport goes back to the bottom first (see [WithScrollOnInput]).
func (s *Session) Type(text string) error {
	return s.send(func() ([]byte, error) {
		var data []byte
		for _, r := range text {
			b, err := s.term.EncodeKey(vt.RuneKey(r))
			if err != nil {
				return nil, err
			}
			data = append(data, b...)
		}
		return data, nil
	}, true)
}

// Key sends one key press, such as "enter", "ctrl+c", "shift+tab", "f5", or
// "a". Modifiers come first, joined by "+": ctrl, alt, shift, and super. The
// key is a single character or one of: enter, esc (escape), tab, backspace,
// delete, insert, up, down, left, right, home, end, pgup (pageup), pgdown
// (pagedown), space, and f1 through f12. An unknown name is an error.
//
// The key is encoded the way the program's keyboard mode asks, such as the
// kitty keyboard protocol. A combination the mode cannot encode, such as
// ctrl+shift+a without the kitty protocol, is an error, and sends nothing.
// A scrolled viewport goes back to the bottom first (see [WithScrollOnInput]).
func (s *Session) Key(name string) error {
	key, perr := parseKey(name)
	if perr != nil {
		return perr
	}
	return s.send(func() ([]byte, error) {
		data, err := s.term.EncodeKey(key)
		if err == nil && len(data) == 0 {
			err = fmt.Errorf("reel: key %q has no encoding in the program's keyboard mode", name)
		}
		return data, err
	}, true)
}

// Paste sends text as a paste, bracketed when the program has enabled
// bracketed paste mode. A scrolled viewport goes back to the bottom first (see
// [WithScrollOnInput]).
func (s *Session) Paste(text string) error {
	return s.send(func() ([]byte, error) { return s.term.EncodePaste(text) }, true)
}

// Mouse sends a mouse event at cell (x, y), counted from 0. The program
// must have enabled a mouse tracking mode that reports the event, as in a real
// terminal; otherwise nothing is sent. Unlike keys, it leaves a scrolled
// viewport where it is, since the program may be handling the event.
func (s *Session) Mouse(action vt.MouseAction, button vt.MouseButton, x, y int) error {
	return s.send(func() ([]byte, error) { return s.term.EncodeMouse(action, button, x, y) }, false)
}

// Focus gives the terminal window focus, or takes it away, and appends a
// [Focus] event. The program is told when it has enabled focus reporting.
func (s *Session) Focus(on bool) error {
	return s.change(func() (Event, error) {
		data, err := s.term.EncodeFocus(on)
		if err == nil {
			err = s.term.SendInput(data)
		}
		if err != nil {
			return nil, err
		}
		s.focused.Store(on)
		return Focus{On: on}, nil
	})
}

// ScrollUp scrolls the terminal's viewport n rows toward older scrollback, as
// scrolling a real terminal's window does, and appends a [Scroll] event. The
// program is not told. The viewport stays put as the program writes more, until
// it is scrolled back to the bottom, or until keys, text, or a paste are sent to
// it (see [WithScrollOnInput]).
func (s *Session) ScrollUp(n int) error {
	return s.scroll(func() error { return s.term.ScrollUp(n) })
}

// ScrollDown scrolls the viewport n rows toward the live screen. See
// [Session.ScrollUp].
func (s *Session) ScrollDown(n int) error {
	return s.scroll(func() error { return s.term.ScrollDown(n) })
}

// ScrollToTop scrolls the viewport to the oldest scrollback row. See
// [Session.ScrollUp].
func (s *Session) ScrollToTop() error { return s.scroll(s.term.ScrollToTop) }

// ScrollToBottom scrolls the viewport back to the live screen. See
// [Session.ScrollUp].
func (s *Session) ScrollToBottom() error { return s.scroll(s.term.ScrollToBottom) }

// scroll runs move, which scrolls the terminal's viewport, and appends a
// [Scroll] event when it moved.
func (s *Session) scroll(move func() error) error {
	return s.change(func() (Event, error) {
		before := s.term.ScrollbackOffset()
		if err := move(); err != nil {
			return nil, err
		}
		after := s.term.ScrollbackOffset()
		if after == before {
			return nil, nil
		}
		return Scroll{Offset: after}, nil
	})
}

// Resize changes the terminal to cols by rows cells and appends a [Resize]
// event. While a process [Exec] started runs, its pseudo-terminal is resized
// too; an error doing so is returned after the terminal has been resized and
// the event appended.
func (s *Session) Resize(cols, rows int) error {
	var ptyErr error
	err := s.change(func() (Event, error) {
		if err := s.term.Resize(cols, rows); err != nil {
			return nil, err
		}
		if s.pty != nil {
			ptyErr = s.pty.Resize(cols, rows)
		}
		return Resize{Cols: cols, Rows: rows}, nil
	})
	if err == nil && ptyErr != nil {
		err = fmt.Errorf("reel: resizing the pseudo-terminal: %w", ptyErr)
	}
	return err
}

// Mark appends a [Mark] event labeled label, which [Timeline.Between] can cut
// a recording at. The label must not be empty. Labels need not be unique.
func (s *Session) Mark(label string) error {
	if label == "" {
		return errors.New("reel: empty mark label")
	}
	return s.change(func() (Event, error) { return Mark{Label: label}, nil })
}

// Annotate appends a, an [Annotation] of something the terminal does not
// show, such as what a test did. It returns an error wrapping
// [ErrInvalidAnnotation] when [AnnotationSchema] does not allow it.
func (s *Session) Annotate(a Annotation) error {
	if err := a.validate(); err != nil {
		return err
	}
	a.Data = bytes.Clone(a.Data)
	return s.change(func() (Event, error) { return a, nil })
}

// SetForeground sets the terminal's foreground color. Nil clears it back to
// the default set with [Session.SetDefaultForeground]. It appends a [Theme]
// event.
func (s *Session) SetForeground(c color.Color) error {
	return s.setColor(s.term.SetForeground, c)
}

// SetBackground sets the terminal's background color. Nil clears it back to
// the default set with [Session.SetDefaultBackground]. It appends a [Theme]
// event.
func (s *Session) SetBackground(c color.Color) error {
	return s.setColor(s.term.SetBackground, c)
}

// SetCursorColor sets the terminal's cursor color. Nil clears it back to the
// default set with [Session.SetDefaultCursorColor]. It appends a [Theme]
// event.
func (s *Session) SetCursorColor(c color.Color) error {
	return s.setColor(s.term.SetCursorColor, c)
}

// SetDefaultForeground sets the foreground color used while none is set with
// [Session.SetForeground]. Nil clears it. It appends a [Theme] event.
func (s *Session) SetDefaultForeground(c color.Color) error {
	return s.setColor(s.term.SetDefaultForeground, c)
}

// SetDefaultBackground sets the background color used while none is set with
// [Session.SetBackground]. Nil clears it. It appends a [Theme] event.
func (s *Session) SetDefaultBackground(c color.Color) error {
	return s.setColor(s.term.SetDefaultBackground, c)
}

// SetDefaultCursorColor sets the cursor color used while none is set with
// [Session.SetCursorColor]. Nil clears it. It appends a [Theme] event.
func (s *Session) SetDefaultCursorColor(c color.Color) error {
	return s.setColor(s.term.SetDefaultCursorColor, c)
}

// setColor applies c with set and appends the host colors that result.
func (s *Session) setColor(set func(color.Color) error, c color.Color) error {
	return s.change(func() (Event, error) {
		if err := set(c); err != nil {
			return nil, err
		}
		fg, bg, cursor := s.term.HostColors()
		return Theme{Colors: Colors{Fg: fg, Bg: bg, Cursor: cursor}}, nil
	})
}
