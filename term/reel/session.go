// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lrstanley/x/term/reel/internal/pty"
	"github.com/lrstanley/x/term/reel/vt"
)

// ErrClosed is returned by [Session] methods called after [Session.Close].
var ErrClosed = errors.New("reel: session is closed")

// defaultRuntime is the emulator pool sessions open from unless
// [WithRuntime] names another. It has no limit: a session is held for as
// long as its caller chooses, so with one, callers that each hold a
// session while opening another could wait on each other forever.
var defaultRuntime = vt.NewRuntime(-1)

var _ io.ReadWriter = (*Session)(nil)

// Session owns a terminal emulator that a program runs in, and records what
// happens to it in a [Timeline]. The program writes output with
// [Session.Write] and reads input with [Session.Read]; the host sends keys,
// text, pastes, and mouse events, resizes, focuses, sets colors, and adds
// marks.
//
// Every change made through the session is appended to the timeline in the
// order the terminal saw it, so replaying the timeline into a fresh
// terminal reproduces it. Query replies are recorded as [Query]
// annotations unless opened [WithoutQueries], not as input. Changes made
// through [Session.Terminal] are not recorded. Methods are safe for
// concurrent use.
type Session struct {
	term     *vt.Terminal
	timeline *Timeline

	// mu makes each terminal change and its recorded event atomic, and
	// serializes Close with them.
	mu sync.Mutex
	// closed and focused are atomic so the blocking Read, Focused, and State
	// can read them without mu.
	closed  atomic.Bool
	focused atomic.Bool
	// done is closed by Close, to end waits.
	done chan struct{}
	// pty is the [Exec] process's pseudo-terminal, resized by Resize until
	// it hangs up. running is set from Exec until the exit is recorded.
	// Both are guarded by mu.
	pty     *pty.PTY
	running bool
	// scrollOnInput is [WithScrollOnInput].
	scrollOnInput bool
	// noQueries disables recording [QueryKind] annotations.
	noQueries bool
	// kittyFiles rewrites file transmissions; nil unless [WithKittyFiles].
	kittyFiles *kittyFiles
	// typingDelay is how long Type and Key pause after each key press.
	typingDelay time.Duration
}

// SessionOption configures [Open].
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	runtime *vt.Runtime
	// scrollback is [Header.Scrollback].
	scrollback    int
	title         string
	skipPristine  bool
	scrollOnInput bool
	noQueries     bool
	typingDelay   time.Duration
	// cellW and cellH are [Header.CellWidth] and [Header.CellHeight].
	cellW, cellH int
	// kittyStorage is [Header.KittyStorage].
	kittyStorage uint64
	// kittyFiles is set [WithKittyFiles].
	kittyFiles bool
	// unknown is [WithUnknownSequence].
	unknown func(vt.UnknownSequence)
}

// WithKittyStorage sets how many bytes of decoded Kitty graphics the
// terminal keeps (see [vt.WithKittyStorage]). It is written to the
// timeline's header, so replays match. Zero leaves vt's default.
func WithKittyStorage(n uint64) SessionOption {
	return func(cfg *sessionConfig) { cfg.kittyStorage = n }
}

// WithCellSize sets the terminal's pixel size of one cell, which programs
// read with a size report, and which sizes Kitty graphics in cells (see
// [vt.WithCellSize]). It is written to the timeline's header, so replays
// match. Sizes that are not positive leave vt's default.
func WithCellSize(width, height int) SessionOption {
	return func(cfg *sessionConfig) {
		if width > 0 && height > 0 {
			cfg.cellW, cfg.cellH = width, height
		}
	}
}

// WithRuntime opens the session's terminal from rt instead of the default
// pool, which has no limit on the terminals it lends, such as to cap them.
func WithRuntime(rt *vt.Runtime) SessionOption {
	return func(cfg *sessionConfig) {
		if rt != nil {
			cfg.runtime = rt
		}
	}
}

// WithScrollback sets how many lines of history the terminal keeps. Zero keeps
// none. A negative count keeps the default of 1000. It is recorded in the
// timeline's [Header]; [Session.SetScrollbackSize] changes it later.
func WithScrollback(lines int) SessionOption {
	return func(cfg *sessionConfig) {
		if lines >= 0 {
			cfg.scrollback = lines
		}
	}
}

// WithTitle names the recording in its timeline's [Header].
func WithTitle(title string) SessionOption {
	return func(cfg *sessionConfig) { cfg.title = title }
}

// WithSkipPristine sets [Header.SkipPristine].
func WithSkipPristine() SessionOption {
	return func(cfg *sessionConfig) { cfg.skipPristine = true }
}

// WithScrollOnInput sets whether [Session.Type], [Session.Key], and
// [Session.Paste] first scroll a scrolled-up viewport (see
// [Session.ScrollUp]) back to the bottom, as a real terminal does on a key
// press, so the program's response is in view. Enabled by default. The
// scroll is recorded as a [Scroll] event before the [Input]. Mouse events
// never scroll, since the program may be handling them.
func WithScrollOnInput(enabled bool) SessionOption {
	return func(cfg *sessionConfig) { cfg.scrollOnInput = enabled }
}

// WithoutQueries disables recording [QueryKind] annotations, such as to keep
// replies out of a recording (a clipboard read reply holds the clipboard's
// text).
func WithoutQueries() SessionOption {
	return func(cfg *sessionConfig) { cfg.noQueries = true }
}

// WithTypingDelay makes [Session.Type] and [Session.Key] pause for d after
// each key press, so a recording shows text being typed. Zero or less (the
// default) sends keys with no pause. [Delay] overrides it for one call.
// The pause is a [time.Sleep], so it costs no real time inside a
// [testing/synctest] bubble.
func WithTypingDelay(d time.Duration) SessionOption {
	return func(cfg *sessionConfig) { cfg.typingDelay = d }
}

// WithUnknownSequence calls fn with each APC or OSC sequence the terminal
// does not implement and drops, such as to log what a program sends that a
// recording will not show (see [vt.WithUnknownSequence]). fn runs while the
// session writes to its terminal, holding the session's lock, so it must not
// call the session's methods.
func WithUnknownSequence(fn func(vt.UnknownSequence)) SessionOption {
	return func(cfg *sessionConfig) { cfg.unknown = fn }
}

// Open starts a session on a cols by rows terminal. It blocks until the
// pool has room or ctx is canceled. The session starts focused. Close it to
// return the terminal to the pool.
func Open(ctx context.Context, cols, rows int, opts ...SessionOption) (*Session, error) {
	cfg := sessionConfig{runtime: defaultRuntime, scrollOnInput: true, scrollback: vt.DefaultScrollback}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	var timeline *Timeline
	term, err := cfg.runtime.Open(ctx, cols, rows,
		vt.WithScrollbackLines(cfg.scrollback),
		vt.WithClock(func() time.Duration { return timeline.now() }),
		vt.WithCellSize(cfg.cellW, cfg.cellH),
		kittyStorage(cfg.kittyStorage),
		vt.WithUnknownSequence(cfg.unknown),
	)
	if err != nil {
		return nil, err
	}
	theme := Colors{Fg: term.Foreground(), Bg: term.Background()}
	if c, ok := term.CursorColor(); ok {
		theme.Cursor = c
	}
	h := Header{
		Cols: cols, Rows: rows, Theme: theme, Title: cfg.title,
		SkipPristine: cfg.skipPristine, Scrollback: cfg.scrollback,
		CellWidth: cfg.cellW, CellHeight: cfg.cellH, KittyStorage: cfg.kittyStorage,
	}
	timeline = newTimeline(h)
	s := &Session{
		term: term, timeline: timeline, done: make(chan struct{}),
		scrollOnInput: cfg.scrollOnInput, noQueries: cfg.noQueries,
		typingDelay: cfg.typingDelay,
	}
	if cfg.kittyFiles {
		s.kittyFiles = newKittyFiles(cmp.Or(cfg.kittyStorage, vt.DefaultKittyStorage))
	}
	s.focused.Store(true)
	return s, nil
}

// Terminal returns the session's terminal, for reading its screen and state.
// Changes made through it directly are not recorded. It must not be used
// after [Session.Close], which returns it to its pool.
func (s *Session) Terminal() *vt.Terminal { return s.term }

// Timeline returns the session's event log, which is the same timeline for
// the session's life and grows as it runs.
func (s *Session) Timeline() *Timeline { return s.timeline }

// Focused reports whether the terminal window has focus.
func (s *Session) Focused() bool { return s.focused.Load() }

// State returns [vt.Terminal.State] with Focused set from the session.
func (s *Session) State() vt.State {
	st := s.term.State()
	st.Focused = s.Focused()
	return st
}

// Write appends p as an [Output] event, feeds it to the terminal, then
// appends a [QueryKind] annotation for each query it holds. It never
// returns a short write.
//
// [WithKittyFiles] rewrites the Kitty graphics p sends by file first, so
// the event and the terminal see the image itself.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return 0, ErrClosed
	}
	out := p
	if s.kittyFiles != nil {
		out = s.kittyFiles.rewrite(p)
	}
	if err := s.writeLocked(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// writeLocked is [Session.Write] of p, as the terminal should get it.
func (s *Session) writeLocked(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	s.timeline.append(Output{Data: bytes.Clone(p)})
	if s.noQueries {
		_, err := s.term.Write(p)
		return err
	}
	replies, err := s.term.WriteReplies(p)
	if err != nil {
		return err
	}
	for _, q := range queries(p, replies) {
		s.timeline.append(queryAnnotation(q))
	}
	return nil
}

// Read reads the program's input, including query replies, blocking until
// some is available. It returns [io.EOF] once [vt.Terminal.CloseReader] is
// called and queued input is drained, and immediately after
// [Session.Close].
func (s *Session) Read(p []byte) (int, error) {
	return s.readContext(context.Background(), p)
}

// readContext is [Session.Read] that also ends when ctx is done.
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
	if s.kittyFiles != nil && !s.closed.Load() {
		// The program ended inside a command: pass it on as sent.
		_ = s.writeLocked(s.kittyFiles.flush())
	}
	closed := s.closed.Swap(true)
	s.mu.Unlock()
	if closed {
		return nil
	}
	close(s.done)
	return s.term.Close()
}

// change runs fn, which changes the terminal, with the session locked, and
// appends the event it returns, if any. Holding the lock guarantees the
// event precedes any output the program writes in response.
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

// Focus sets whether the terminal window has focus and appends a [Focus]
// event. The program is told if it enabled focus reporting.
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

// ScrollUp scrolls the viewport n rows toward older scrollback and appends a
// [Scroll] event. The program is not told. The viewport stays put as the
// program writes more, until it is scrolled back to the bottom or input is
// sent (see [WithScrollOnInput]).
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

// scroll runs move and appends a [Scroll] event if the viewport moved.
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

// SetScrollbackSize sets how many lines of history the terminal keeps, as
// [WithScrollback] does when opening, and appends a [ScrollbackSize] event.
// Zero or less keeps none. Lowering it drops the oldest lines. The program is
// not told.
func (s *Session) SetScrollbackSize(lines int) error {
	lines = max(lines, 0)
	return s.change(func() (Event, error) {
		if err := s.term.SetScrollbackMaxLines(lines); err != nil {
			return nil, err
		}
		return ScrollbackSize{Lines: lines}, nil
	})
}

// clearScrollback is ED 3 (erase saved lines), which clears a terminal's
// history.
const clearScrollback = "\x1b[3J"

// ClearScrollback erases the terminal's history and appends it as the
// [Output] the terminal would get from a program doing the same, so players
// of the recording clear theirs too. The program is not told.
func (s *Session) ClearScrollback() error {
	return s.change(func() (Event, error) {
		if err := s.term.ClearScrollback(); err != nil {
			return nil, err
		}
		return Output{Data: []byte(clearScrollback)}, nil
	})
}

// Resize resizes the terminal to cols by rows and appends a [Resize] event.
// While an [Exec] process runs, its pseudo-terminal is resized too; an
// error doing so is returned after the terminal is resized and the event
// appended.
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

// Mark appends a [Mark] event, which [Timeline.Between] can cut at. The
// label must not be empty but need not be unique.
func (s *Session) Mark(label string) error {
	if label == "" {
		return errors.New("reel: empty mark label")
	}
	return s.change(func() (Event, error) { return Mark{Label: label}, nil })
}

// Annotate appends a. It returns an error wrapping [ErrInvalidAnnotation]
// when [AnnotationSchema] does not allow it.
func (s *Session) Annotate(a Annotation) error {
	if err := a.validate(); err != nil {
		return err
	}
	a.Data = bytes.Clone(a.Data)
	return s.change(func() (Event, error) { return a, nil })
}

// SetForeground sets the foreground color, or with nil reverts to
// [Session.SetDefaultForeground]'s. It appends a [Theme] event.
func (s *Session) SetForeground(c color.Color) error {
	return s.setColor(s.term.SetForeground, c)
}

// SetBackground sets the background color, or with nil reverts to
// [Session.SetDefaultBackground]'s. It appends a [Theme] event.
func (s *Session) SetBackground(c color.Color) error {
	return s.setColor(s.term.SetBackground, c)
}

// SetCursorColor sets the cursor color, or with nil reverts to
// [Session.SetDefaultCursorColor]'s. It appends a [Theme] event.
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

// setColor applies c with set and appends the resulting host colors.
func (s *Session) setColor(set func(color.Color) error, c color.Color) error {
	return s.change(func() (Event, error) {
		if err := set(c); err != nil {
			return nil, err
		}
		fg, bg, cursor := s.term.HostColors()
		return Theme{Colors: Colors{Fg: fg, Bg: bg, Cursor: cursor}}, nil
	})
}
