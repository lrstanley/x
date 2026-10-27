// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"image/color"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
)

func openTest(t *testing.T, cols, rows int, opts ...SessionOption) *Session {
	t.Helper()
	s, err := Open(t.Context(), cols, rows, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// write writes program output to s, failing the test on error.
func write(t *testing.T, s *Session, out string) {
	t.Helper()
	if _, err := s.Write([]byte(out)); err != nil {
		t.Fatal(err)
	}
}

// readInput reads n bytes the program was sent.
func readInput(t *testing.T, s *Session, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func TestSessionOpen(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4, WithTitle("demo"), WithScrollback(0), WithRuntime(vt.NewRuntime(1)))
	h := s.Timeline().Header()
	white, black := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.NRGBA{A: 0xff}
	if h.Cols != 20 || h.Rows != 4 || h.Title != "demo" || h.Theme.Fg != white || h.Theme.Bg != black || h.Theme.Cursor != nil {
		t.Fatalf("Header = %+v", h)
	}
	if !s.Focused() || !s.State().Focused {
		t.Fatal("new session is not focused")
	}
	if s.Timeline().Len() != 0 {
		t.Fatalf("new session timeline has %d events", s.Timeline().Len())
	}
	if _, err := Open(t.Context(), 0, 4); err == nil {
		t.Fatal("Open with zero columns succeeded")
	}
}

func TestSessionRecordsWithTimestamps(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := openTest(t, 20, 4)
		out := []byte("hello")
		if _, err := s.Write(out); err != nil {
			t.Fatal(err)
		}
		// The timeline keeps its own copy.
		out[0] = 'j'
		time.Sleep(time.Second)
		if err := s.Key("enter"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if err := s.Type("hi"); err != nil {
			t.Fatal(err)
		}
		if err := s.Paste("p"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		if err := s.Mark("m"); err != nil {
			t.Fatal(err)
		}
		if err := s.Resize(10, 3); err != nil {
			t.Fatal(err)
		}
		if err := s.Focus(false); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		write(t, s, "bye")

		assertEvents(t, s.Timeline(),
			`0 output "hello"`,
			`1000 input "\r"`,
			`2000 input "hi"`,
			`2000 input "p"`,
			`3000 mark m`,
			`3000 resize 10x3`,
			`3000 focus false`,
			`4000 output "bye"`,
		)
		if got := readInput(t, s, 4); got != "\rhip" {
			t.Fatalf("program read %q, want %q", got, "\rhip")
		}
		if cols, rows := s.Terminal().Size(); cols != 10 || rows != 3 {
			t.Fatalf("terminal size = %dx%d, want 10x3", cols, rows)
		}
		if s.Focused() || s.State().Focused {
			t.Fatal("session still focused after Focus(false)")
		}
	})
}

func TestSessionInputMatchesRead(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4)
	// The program asks for the kitty keyboard protocol, SGR mouse reports,
	// and bracketed paste, which change what each input sends.
	write(t, s, "\x1b[>1u\x1b[?1000h\x1b[?1006h\x1b[?2004h")
	steps := []func() error{
		func() error { return s.Key("ctrl+c") },
		func() error { return s.Type("ok") },
		func() error { return s.Paste("x") },
		func() error { return s.Mouse(vt.MousePress, vt.MouseLeft, 2, 1) },
	}
	var want strings.Builder
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	for in := range s.Timeline().Inputs() {
		want.Write(in.Data)
	}
	if got := readInput(t, s, want.Len()); got != want.String() {
		t.Fatalf("program read %q, timeline inputs %q", got, want.String())
	}
	if want.String() != "\x1b[99;5uok\x1b[200~x\x1b[201~\x1b[<0;3;2M" {
		t.Fatalf("inputs = %q", want.String())
	}
}

func TestSessionInputNotSent(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4)
	// No mouse mode is on, so the click is not sent or recorded.
	if err := s.Mouse(vt.MousePress, vt.MouseLeft, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Type(""); err != nil {
		t.Fatal(err)
	}
	if n := s.Timeline().Len(); n != 0 {
		t.Fatalf("timeline has %d events, want 0", n)
	}
	if err := s.Key("hyper+x"); err == nil {
		t.Fatal("Key with an unknown modifier succeeded")
	}
	// Without the kitty protocol, ctrl+shift+a has no encoding.
	if err := s.Key("ctrl+shift+a"); err == nil {
		t.Fatal("Key with no encoding succeeded")
	}
	if err := s.Mark(""); err == nil {
		t.Fatal("Mark with an empty label succeeded")
	}
}

func TestSessionFocusReport(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4)
	write(t, s, "\x1b[?1004h")
	if err := s.Focus(false); err != nil {
		t.Fatal(err)
	}
	if err := s.Focus(true); err != nil {
		t.Fatal(err)
	}
	if got := readInput(t, s, 6); got != "\x1b[O\x1b[I" {
		t.Fatalf("program read %q, want focus out then in", got)
	}
	// Focus is its own event, not an Input.
	if n := len(slices.Collect(s.Timeline().Inputs())); n != 0 {
		t.Fatalf("focus changes recorded %d inputs", n)
	}
}

func TestSessionTheme(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4)
	red := color.NRGBA{R: 0xff, A: 0xff}
	blue := color.RGBA{B: 0xff, A: 0xff}
	steps := []func() error{
		func() error { return s.SetDefaultForeground(red) },
		func() error { return s.SetForeground(blue) },
		func() error { return s.SetBackground(red) },
		func() error { return s.SetCursorColor(red) },
		func() error { return s.SetForeground(nil) },
		func() error { return s.SetDefaultForeground(nil) },
		func() error { return s.SetDefaultBackground(blue) },
		func() error { return s.SetDefaultCursorColor(nil) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	nblue := color.NRGBA{B: 0xff, A: 0xff}
	want := []Colors{
		{Fg: red},
		{Fg: nblue},
		{Fg: nblue, Bg: red},
		{Fg: nblue, Bg: red, Cursor: red},
		{Fg: red, Bg: red, Cursor: red},
		{Bg: red, Cursor: red},
		{Bg: red, Cursor: red},
		{Bg: red, Cursor: red},
	}
	var got []Colors
	for ev := range s.Timeline().Events() {
		theme, ok := ev.(Theme)
		if !ok {
			t.Fatalf("event %T, want Theme", ev)
		}
		got = append(got, theme.Colors)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("themes:\n got %v\nwant %v", got, want)
	}
	if fg := s.Terminal().Foreground(); fg != (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Fatalf("foreground after clearing = %v, want the terminal default", fg)
	}
}

func TestSessionClose(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	write(t, s, "x")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if _, err = s.Write([]byte("y")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after Close = %v, want ErrClosed", err)
	}
	if err = s.Key("a"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Key after Close = %v, want ErrClosed", err)
	}
	if err = s.Mark("m"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Mark after Close = %v, want ErrClosed", err)
	}
	if _, err = s.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("Read after Close = %v, want EOF", err)
	}
	assertEvents(t, s.Timeline(), `0 output "x"`)
}

func TestSessionCloseUnblocksRead(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() {
		_, rerr := s.Read(make([]byte, 1))
		done <- rerr
	}()
	_ = s.Close()
	select {
	case got := <-done:
		if !errors.Is(got, io.EOF) {
			t.Fatalf("Read = %v, want EOF", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read still blocked after Close")
	}
}

// TestSessionConcurrent drives a session from several goroutines, as a
// program writing output while a test sends input does, and checks the
// timeline holds every event in order.
func TestSessionConcurrent(t *testing.T) {
	t.Parallel()
	s := openTest(t, 40, 10)
	const n = 200
	var wg sync.WaitGroup
	wg.Go(func() {
		for range n {
			write(t, s, "line\r\n")
		}
	})
	wg.Go(func() {
		for range n {
			if err := s.Type("x"); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Go(func() {
		buf := make([]byte, 64)
		for read := 0; read < n; {
			m, err := s.Read(buf)
			if err != nil {
				t.Error(err)
				return
			}
			read += m
		}
	})
	wg.Go(func() {
		for range n {
			_ = s.Timeline().Len()
			_ = s.State()
		}
	})
	wg.Go(func() {
		for i := range 20 {
			if err := s.Resize(40-i%2, 10); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Wait()
	var last time.Duration
	counts := map[string]int{}
	for ev := range s.Timeline().Events() {
		if ev.At() < last {
			t.Fatalf("offset %v after %v", ev.At(), last)
		}
		last = ev.At()
		counts[strings.Fields(describe([]Event{ev})[0])[1]]++
	}
	if counts["output"] != n || counts["input"] != n || counts["resize"] != 20 {
		t.Fatalf("event counts = %v", counts)
	}
}
