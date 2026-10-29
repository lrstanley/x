// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestConditions(t *testing.T) {
	t.Parallel()
	screen := ScreenText{Text: "héllo world\n$ ls ", CursorRow: 1}
	for name, tc := range map[string]struct {
		cond    Condition
		ok      bool
		matches []Match
	}{
		"contains": {
			Contains("world", "ls"), true,
			[]Match{{Text: "world", Row: 0, Col: 6}, {Text: "ls", Row: 1, Col: 2}},
		},
		"contains missing":  {Contains("world", "nope"), false, nil},
		"contains none":     {Contains(), true, nil},
		"not contains":      {NotContains("nope"), true, nil},
		"not contains some": {NotContains("nope", "ls"), false, nil},
		"matches":           {Matches(regexp.MustCompile(`w\w+`)), true, []Match{{Text: "world", Row: 0, Col: 6}}},
		"matches not":       {Matches(regexp.MustCompile(`\d`)), false, nil},
		"not matches":       {NotMatches(regexp.MustCompile(`\d`)), true, nil},
		"not matches some":  {NotMatches(regexp.MustCompile(`l+`)), false, nil},
		"func":              {ScreenFunc(func(s string) bool { return strings.HasPrefix(s, "hé") }), true, nil},
		"cursor line":       {OnCursorLine(Contains("$ ")), true, []Match{{Text: "$ ", Row: 1, Col: 0}}},
		"cursor line other": {OnCursorLine(Contains("world")), false, nil},
	} {
		matches, ok := tc.cond.Check(screen)
		if ok != tc.ok || !reflect.DeepEqual(matches, tc.matches) {
			t.Errorf("%s: Check = %+v, %v, want %+v, %v", name, matches, ok, tc.matches, tc.ok)
		}
	}
	if _, ok := OnCursorLine(Contains("$")).Check(ScreenText{Text: "$", CursorRow: -1}); ok {
		t.Error("OnCursorLine held with the viewport scrolled off the cursor")
	}
	if _, ok := OnCursorLine(NotContains("x")).Check(ScreenText{Text: "a\nb", CursorRow: 2}); ok {
		t.Error("OnCursorLine held with the cursor past the last line")
	}
	if got := OnCursorLine(Contains("$ ")).String(); got != `contains ["$ "] on the cursor line` {
		t.Errorf("name = %q", got)
	}
}

// writeLater writes out to s after d, in the background.
func writeLater(t *testing.T, s *Session, d time.Duration, out ...string) {
	t.Helper()
	go func() {
		for _, o := range out {
			time.Sleep(d)
			_, _ = s.Write([]byte(o))
		}
	}()
}

func TestSessionWait(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	write(t, s, "ready")
	// Already on the screen.
	got, err := s.Wait(t.Context(), Contains("ready"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []Match{{Text: "ready"}}; !reflect.DeepEqual(got.Matches, want) {
		t.Fatalf("Matches = %+v, want %+v", got.Matches, want)
	}

	screen, err := s.ScreenText()
	if err != nil || !strings.HasPrefix(screen.Text, "ready") || screen.CursorRow != 0 {
		t.Fatalf("ScreenText = %+v, %v", screen, err)
	}

	// Written after the wait starts.
	writeLater(t, s, 20*time.Millisecond, "\r\n$ ")
	got, err = s.Wait(t.Context(), OnCursorLine(Contains("$ ")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Text, "ready") || len(got.Matches) != 1 || got.Matches[0].Row != 1 {
		t.Fatalf("Wait = %+v", got)
	}
}

func TestSessionWaitTimeout(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	write(t, s, "nothing")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := s.Wait(ctx, Contains("never"))
	var werr *WaitError
	if !errors.As(err, &werr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v, want a WaitError for the deadline", err)
	}
	if werr.Wait != `contains ["never"]` || !strings.Contains(werr.Last, "nothing") {
		t.Fatalf("WaitError = %+v", werr)
	}
}

func TestSessionWaitCanceled(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	cause := errors.New("stop")
	ctx, cancel := context.WithCancelCause(t.Context())
	time.AfterFunc(10*time.Millisecond, func() { cancel(cause) })
	if _, err := s.Wait(ctx, Contains("never")); !errors.Is(err, cause) {
		t.Fatalf("Wait = %v, want the context's cause", err)
	}
}

func TestSessionWaitClosed(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	time.AfterFunc(10*time.Millisecond, func() { _ = s.Close() })
	if _, err := s.Wait(t.Context(), Contains("never")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait = %v, want ErrClosed", err)
	}
	if _, err := s.ScreenText(); !errors.Is(err, ErrClosed) {
		t.Fatalf("ScreenText = %v, want ErrClosed", err)
	}
	if _, err := s.WaitSettle(t.Context(), time.Hour); !errors.Is(err, ErrClosed) {
		t.Fatalf("WaitSettle = %v, want ErrClosed", err)
	}
	if err := s.WaitIdle(t.Context(), 0, time.Hour, time.Hour); !errors.Is(err, ErrClosed) {
		t.Fatalf("WaitIdle = %v, want ErrClosed", err)
	}
}

func TestSessionWaitSettle(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	const quiet = 40 * time.Millisecond
	start := time.Now()
	// Each change restarts the quiet time, and a repaint of the same text
	// does not.
	writeLater(t, s, 20*time.Millisecond, "a", "\rb", "\rc", "\rc")
	got, err := s.WaitSettle(t.Context(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "c") {
		t.Fatalf("settled on %q, want the last change", got)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond+quiet {
		t.Fatalf("settled after %s, before the last change had been quiet for %s", elapsed, quiet)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	var werr *WaitError
	if _, err = s.WaitSettle(ctx, time.Hour); !errors.As(err, &werr) || werr.Wait != "settle" {
		t.Fatalf("WaitSettle = %v, want a settle WaitError", err)
	}
}

func TestSessionWaitIdle(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)

	// No output: it gives up at the limit.
	start := time.Now()
	since := s.Terminal().OutputSeq()
	if err := s.WaitIdle(t.Context(), since, time.Hour, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WaitIdle with no output took %s", elapsed)
	}

	// Output already written counts, and further output restarts the quiet
	// time, past the limit.
	write(t, s, "a")
	writeLater(t, s, 15*time.Millisecond, "b", "c")
	start = time.Now()
	if err := s.WaitIdle(t.Context(), since, 30*time.Millisecond, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("WaitIdle returned after %s, before the output was quiet", elapsed)
	}
	if text := s.Terminal().Text(); !strings.HasPrefix(text, "abc") {
		t.Fatalf("screen = %q, want all the output", text)
	}

	// Output held by synchronized output that never ends is not shown, so
	// the limit still applies.
	held := s.Terminal().OutputSeq()
	write(t, s, "\x1b[?2026hhidden")
	start = time.Now()
	if err := s.WaitIdle(t.Context(), held, time.Hour, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WaitIdle on held output took %s, want the limit", elapsed)
	}
	write(t, s, "\x1b[?2026l")

	// Synchronized output holds the screen, so it is not idle.
	since = s.Terminal().OutputSeq()
	write(t, s, "\x1b[?2026h")
	writeLater(t, s, 50*time.Millisecond, "\x1b[?2026l")
	start = time.Now()
	if err := s.WaitIdle(t.Context(), since, 5*time.Millisecond, time.Hour); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("WaitIdle returned after %s, while output was held", elapsed)
	}
}
