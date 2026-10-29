// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ScreenText is the screen a [Condition] checks.
type ScreenText struct {
	// Text is the viewport's text, as [vt.Terminal.Text] returns it: one
	// line per row.
	Text string
	// CursorRow is the cursor's row in Text, or -1 when there is none, such
	// as when the viewport is scrolled off it or the text is not a
	// terminal's.
	CursorRow int
}

// Match is where a [Condition] found what it looked for on the screen.
type Match struct {
	// Text is the text found.
	Text string `json:"text"`
	// Row and Col are where it starts, from 0, with Col in cells.
	Row int `json:"row"`
	Col int `json:"col"`
}

// Condition is what [Session.Wait] waits for on the screen. The conditions
// reel provides are [Contains], [NotContains], [Matches], [NotMatches],
// [ScreenFunc], and [OnCursorLine].
type Condition interface {
	// Check reports whether screen satisfies the condition, and where on
	// it the condition found what it looked for, if it locates anything.
	Check(screen ScreenText) (matches []Match, ok bool)
	// String describes the condition, such as `contains ["$ "]`, for a
	// [WaitError].
	String() string
}

// Contains is a [Condition] that holds when every one of texts is on the
// screen. Its matches are where each first appears.
func Contains(texts ...string) Condition { return contains{texts: texts, present: true} }

// NotContains is a [Condition] that holds when none of texts is on the
// screen.
func NotContains(texts ...string) Condition { return contains{texts: texts} }

type contains struct {
	texts   []string
	present bool
}

func (c contains) Check(screen ScreenText) ([]Match, bool) {
	var matches []Match
	for _, text := range c.texts {
		i := strings.Index(screen.Text, text)
		if (i >= 0) != c.present {
			return nil, false
		}
		if i >= 0 && text != "" {
			matches = append(matches, matchAt(screen.Text, i, i+len(text)))
		}
	}
	return matches, true
}

func (c contains) String() string {
	name := "contains"
	if !c.present {
		name = "not contains"
	}
	return fmt.Sprintf("%s %q", name, c.texts)
}

// Matches is a [Condition] that holds when re matches the screen. Its match
// is where re first matches.
func Matches(re *regexp.Regexp) Condition { return pattern{re: re, match: true} }

// NotMatches is a [Condition] that holds when re does not match the screen.
func NotMatches(re *regexp.Regexp) Condition { return pattern{re: re} }

type pattern struct {
	re    *regexp.Regexp
	match bool
}

func (p pattern) Check(screen ScreenText) ([]Match, bool) {
	loc := p.re.FindStringIndex(screen.Text)
	if (loc != nil) != p.match {
		return nil, false
	}
	if loc == nil {
		return nil, true
	}
	return []Match{matchAt(screen.Text, loc[0], loc[1])}, true
}

func (p pattern) String() string {
	if p.match {
		return fmt.Sprintf("matches %q", p.re)
	}
	return fmt.Sprintf("not matches %q", p.re)
}

// ScreenFunc is a [Condition] that holds when fn returns true for the
// screen's text. It locates nothing. Like every condition, it is checked
// only when the screen changes, so fn should depend on the text alone.
func ScreenFunc(fn func(text string) bool) Condition { return screenFunc(fn) }

type screenFunc func(text string) bool

func (f screenFunc) Check(screen ScreenText) ([]Match, bool) { return nil, f(screen.Text) }

func (screenFunc) String() string { return "screen func" }

// OnCursorLine is a [Condition] that holds when c holds for the cursor's
// line alone, such as to wait for a prompt. It does not hold while the
// viewport is scrolled off the cursor.
func OnCursorLine(c Condition) Condition { return cursorLine{c: c} }

type cursorLine struct{ c Condition }

func (l cursorLine) Check(screen ScreenText) ([]Match, bool) {
	if screen.CursorRow < 0 {
		return nil, false
	}
	line := screen.Text
	for range screen.CursorRow {
		var ok bool
		if _, line, ok = strings.Cut(line, "\n"); !ok {
			return nil, false
		}
	}
	line, _, _ = strings.Cut(line, "\n")
	matches, ok := l.c.Check(ScreenText{Text: line})
	for i := range matches {
		matches[i].Row += screen.CursorRow
	}
	return matches, ok
}

func (l cursorLine) String() string { return l.c.String() + " on the cursor line" }

// matchAt returns the match of text[i:j].
func matchAt(text string, i, j int) Match {
	line := strings.LastIndexByte(text[:i], '\n') + 1
	return Match{
		Text: text[i:j],
		Row:  strings.Count(text[:line], "\n"),
		Col:  ansi.StringWidth(text[line:i]),
	}
}

// WaitResult is the screen a [Session.Wait] condition held on.
type WaitResult struct {
	// Text is the screen's text.
	Text string
	// Matches is where the condition found what it looked for.
	Matches []Match
}

// WaitError is the error of a wait that ended before what it waited for,
// because its context was done or the session closed.
type WaitError struct {
	// Wait names what was waited for, such as `contains ["$ "]`, "settle",
	// or "idle".
	Wait string
	// Last is the screen text the wait saw last, empty for "idle".
	Last string
	// Err is why the wait ended: the cause of its context, or [ErrClosed].
	Err error
}

func (e *WaitError) Error() string { return "reel: wait for " + e.Wait + ": " + e.Err.Error() }
func (e *WaitError) Unwrap() error { return e.Err }

// screenRead is the screen as a wait last read it.
type screenRead struct {
	ScreenText
	read bool
	gen  uint64
}

// readScreen updates r from the terminal, reading the text only when the
// grid or cursor row changed since r was read, and reports whether it did.
// It holds s.mu, so the terminal has every recorded event's bytes.
func (s *Session) readScreen(r *screenRead) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return false, ErrClosed
	}
	gen, row := s.term.GridGeneration(), s.term.Cursor().Y
	if r.read && gen == r.gen && row == r.CursorRow {
		return false, nil
	}
	*r = screenRead{ScreenText: ScreenText{Text: s.term.Text(), CursorRow: row}, read: true, gen: gen}
	return true, nil
}

// ScreenText returns the screen as [Session.Wait] checks it, such as to
// check a [Condition] once. It returns [ErrClosed] after [Session.Close].
func (s *Session) ScreenText() (ScreenText, error) {
	var r screenRead
	_, err := s.readScreen(&r)
	return r.ScreenText, err
}

// await runs step now, and again after each event appended to the
// timeline and each time timer (if any) fires, until step reports done or
// fails, the session closes (which step sees as [ErrClosed]), or ctx is
// done, returning its cause. step learns whether the timer woke it.
func (s *Session) await(ctx context.Context, timer *time.Timer, step func(fired bool) (bool, error)) error {
	var tick <-chan time.Time
	if timer != nil {
		tick = timer.C
	}
	fired := false
	for {
		updated := s.timeline.Updated()
		if done, err := step(fired); done || err != nil {
			return err
		}
		fired = false
		select {
		case <-updated:
		case <-s.done:
		case <-tick:
			fired = true
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// Wait waits until cond holds for the screen, and returns the screen it
// held on. It checks the screen when called and again whenever an event
// recorded through the session changes it; changes made through
// [Session.Terminal] are not seen until the next event. It returns a
// [*WaitError] when ctx is done or the session closes first; pass a context
// with a deadline to time out.
func (s *Session) Wait(ctx context.Context, cond Condition) (WaitResult, error) {
	var r screenRead
	var res WaitResult
	err := s.await(ctx, nil, func(bool) (bool, error) {
		changed, err := s.readScreen(&r)
		if err != nil || !changed {
			return false, err
		}
		matches, ok := cond.Check(r.ScreenText)
		res = WaitResult{Text: r.Text, Matches: matches}
		return ok, nil
	})
	if err != nil {
		return WaitResult{}, &WaitError{Wait: cond.String(), Last: r.Text, Err: err}
	}
	return res, nil
}

// WaitSettle waits until the screen's text has not changed for quiet,
// counting from the call, and returns it. Changes that leave the text as it
// was, such as a repaint, do not count. It sees changes as [Session.Wait]
// does, and returns a [*WaitError] when ctx is done or the session closes
// first.
func (s *Session) WaitSettle(ctx context.Context, quiet time.Duration) (string, error) {
	var r screenRead
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	err := s.await(ctx, timer, func(fired bool) (bool, error) {
		prev, started := r.Text, r.read
		if _, err := s.readScreen(&r); err != nil {
			return false, err
		}
		if started && r.Text != prev {
			timer.Reset(quiet)
			return false, nil
		}
		return fired, nil
	})
	if err != nil {
		return "", &WaitError{Wait: "settle", Last: r.Text, Err: err}
	}
	return r.Text, nil
}

// outputState returns the terminal's output count and whether
// synchronized output holds its screen, under s.mu.
func (s *Session) outputState() (seq uint64, held bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return 0, false, ErrClosed
	}
	return s.term.OutputSeq(), s.term.Held(), nil
}

// WaitIdle waits for the program to finish responding to something, such
// as input: until the terminal has shown output newer than since (a
// [vt.Terminal.OutputSeq] read before sending it), and then no more output
// for quiet. Output written while synchronized output holds the screen
// counts once the hold ends. With no output shown newer than since after
// limit, it returns nil without waiting longer; once there is, only quiet
// ends it. It returns a [*WaitError] when ctx is done or the session closes
// first.
func (s *Session) WaitIdle(ctx context.Context, since uint64, quiet, limit time.Duration) error {
	// The timer runs to limit until there is output, then to quiet after
	// each output.
	timer := time.NewTimer(limit)
	defer timer.Stop()
	output := false
	err := s.await(ctx, timer, func(fired bool) (bool, error) {
		seq, held, err := s.outputState()
		if err != nil {
			return false, err
		}
		if seq != since && !held {
			since, output = seq, true
			timer.Reset(quiet)
			return false, nil
		}
		return fired && (!output || !held), nil
	})
	if err != nil {
		return &WaitError{Wait: "idle", Err: err}
	}
	return nil
}
