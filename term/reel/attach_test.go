// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

// classicTypescript and classicTiming are what "script -t" wrote for
// "printf hello; sleep 0.2; printf ' world\n'".
const (
	classicTypescript = "Script started on 2026-10-25 18:57:17-06:00 [COMMAND=\"printf ...\"]\n" +
		"hello world\r\n" +
		"\nScript done on 2026-10-25 18:57:17-06:00 [COMMAND_EXIT_CODE=\"0\"]\n"
	classicTiming = "0.000906 5\n0.200954 8\n"
)

// logIO and logIOTiming are what "script --log-io typescript -T timing"
// wrote on an 80x24 terminal for a program that printed "a", read the line
// "yo", printed "got yo", and then "b", while the terminal was resized to
// 100x30.
const (
	logIO = "Script started on 2026-10-25 18:57:46-06:00 [COMMAND=\"...\" TERM=\"xterm-256color\" COLUMNS=\"80\" LINES=\"24\"]\n" +
		"ayo\ryo\r\ngot yob\n" +
		"Script done on 2026-10-25 18:57:47-06:00 [COMMAND_EXIT_CODE=\"0\"]\n"
	logIOTiming = `H 0.000000 START_TIME 2026-10-25 18:57:46-06:00
H 0.000000 TERM xterm-256color
H 0.000000 COLUMNS 80
H 0.000000 LINES 24
H 0.000000 COMMAND printf a; sleep 0.3; read x; printf "got $x"; sleep 0.3; printf b
H 0.000000 TIMING_LOG timing
H 0.000000 OUTPUT_LOG typescript
H 0.000000 INPUT_LOG typescript
O 0.000624 1
I 0.148256 3
O 0.000143 4
O 0.152877 6
S 0.047112 SIGWINCH ROWS=30 COLS=100
O 0.253903 1
H 0.000000 DURATION 0.814101
H 0.000000 EXIT_CODE 0
`
)

func TestAttachClassicTiming(t *testing.T) {
	t.Parallel()
	s := openTest(t, 12, 2)
	if err := Attach(t.Context(), s, strings.NewReader(classicTypescript), WithTiming(strings.NewReader(classicTiming))); err != nil {
		t.Fatal(err)
	}
	// The times in the file, 0.906ms and 201.86ms, are rounded to the
	// millisecond.
	assertEvents(t, s.Timeline(), `1 output "hello"`, `202 output " world\r\n"`)
	assertFrames(t, exportFrames(t, s.Timeline(), Profile{FPS: 100, Hold: -1}),
		// Frames are at the times in the timeline, and delays on the 10ms
		// grid.
		wantFrame{"hello", 1 * ms, 200 * ms, true},
		wantFrame{"hello world", 202 * ms, 10 * ms, true},
	)
}

func TestAttachAdvancedTiming(t *testing.T) {
	t.Parallel()
	s := openTest(t, 80, 2)
	if err := Attach(t.Context(), s, strings.NewReader(logIO), WithTiming(strings.NewReader(logIOTiming))); err != nil {
		t.Fatal(err)
	}
	// Each time is the sum of the intervals before it, rounded to the
	// millisecond: 0.624ms, 148.88ms, 149.023ms, 301.9ms, 349.012ms,
	// 602.915ms.
	assertEvents(t, s.Timeline(),
		`1 output "a"`,
		`149 input "yo\r"`,
		`149 output "yo\r\n"`,
		`302 output "got yo"`,
		`349 resize 100x30`,
		`603 output "b"`,
		`603 exit 0`,
	)
	if text := s.Terminal().Text(); !strings.Contains(text, "got yob") {
		t.Fatalf("terminal:\n%s", text)
	}

	// With the input in a file of its own, the typescript has no input.
	s = openTest(t, 80, 2)
	timing := strings.Replace(logIOTiming, "INPUT_LOG typescript", "INPUT_LOG input", 1)
	if err := Attach(t.Context(), s, strings.NewReader(strings.Replace(logIO, "ayo\r", "a", 1)), WithTiming(strings.NewReader(timing))); err != nil {
		t.Fatal(err)
	}
	if n := len(slices.Collect(s.Timeline().Inputs())); n != 0 {
		t.Fatalf("%d input events with the input logged elsewhere", n)
	}
	if text := s.Terminal().Text(); !strings.Contains(text, "got yob") {
		t.Fatalf("terminal:\n%s", text)
	}
}

// TestAttachOwnsClock checks that the events appended while a timed Attach
// waits for its typescript are stamped at the timing file's time, and that
// the wall clock carries on from there.
func TestAttachOwnsClock(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 1)
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Attach(t.Context(), s, pr, WithTiming(strings.NewReader("5 20\n10 1\n"))) }()
	// Attach looks for the typescript header in the first 18 bytes, so the
	// first run is longer. The pipe hands it over only once Attach reads
	// it, and the second line is read straight after.
	if _, err := pw.Write([]byte(strings.Repeat("a", 20))); err != nil {
		t.Fatal(err)
	}
	// Attach is waiting for the second run now, or about to, at 15s.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := s.Mark("m"); err != nil {
			t.Fatal(err)
		}
		if lastAt(s.Timeline().snapshot()) == 15*time.Second {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("events never reached 15s: %q", describe(slices.Collect(s.Timeline().Events())))
		}
		time.Sleep(time.Millisecond)
	}
	if err := Attach(t.Context(), s, strings.NewReader("x"), WithTiming(strings.NewReader("0 1\n"))); err == nil {
		t.Fatal("a second timed Attach succeeded")
	}
	if _, err := pw.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	write(t, s, "c")
	events := slices.Collect(s.Timeline().Events())
	if c := events[len(events)-1].At(); c < 15*time.Second || c > 16*time.Second {
		t.Fatalf("output after the Attach at %v, want just after 15s", c)
	}
	if b := events[len(events)-2]; b.At() != 15*time.Second {
		t.Fatalf("second byte at %v, want 15s", b.At())
	}
}

// TestAttachEmptyTiming checks that a timed Attach that appends nothing
// leaves a new session's clock to start with its first event.
func TestAttachEmptyTiming(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 1)
	if err := Attach(t.Context(), s, strings.NewReader(""), WithTiming(strings.NewReader(""))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	write(t, s, "a")
	assertEvents(t, s.Timeline(), `0 output "a"`)
}

func TestAttachUntimed(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 1)
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("a"))
		time.Sleep(50 * time.Millisecond)
		_, _ = pw.Write([]byte("b"))
		_ = pw.Close()
	}()
	if err := Attach(t.Context(), s, pr); err != nil {
		t.Fatal(err)
	}
	events := slices.Collect(s.Timeline().Events())
	if len(events) != 2 || events[1].At()-events[0].At() < 50*ms {
		t.Fatalf("events = %+v, want two, 50ms apart", events)
	}
	if text := strings.TrimSpace(s.Terminal().Text()); text != "ab" {
		t.Fatalf("terminal = %q, want ab", text)
	}
}

func TestAttachTimingErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ typescript, timing, err string }{
		"short":         {"abc", "0.1 2\n0.1 2\n", "timing line 2: typescript: unexpected EOF"},
		"empty header":  {"Script started on now", "0 1\n", "typescript: unexpected EOF"},
		"delay":         {"a", "x 1\n", "timing line 1: delay"},
		"negative":      {"a", "-1 1\n", "timing line 1: delay"},
		"nan":           {"a", "NaN 1\n", "timing line 1: delay"},
		"count":         {"a", "0 -1\n", "timing line 1: count"},
		"fields":        {"a", "\n0 1 2\n", "timing line 2: malformed"},
		"kind":          {"a", "O 0\n", "timing line 1: malformed"},
		"winch":         {"a", "S 0 SIGWINCH ROWS=x\n", "timing line 1: SIGWINCH"},
		"winch range":   {"a", "S 0 SIGWINCH ROWS=70000 COLS=20\n", "timing line 1: SIGWINCH"},
		"winch sign":    {"a", "S 0 SIGWINCH ROWS=+5 COLS=20\n", "timing line 1: SIGWINCH"},
		"exit":          {"a", "H 0 EXIT_CODE x\n", "timing line 1: exit code"},
		"out of range":  {"a", "31536000 0\n1 0\n", "timing line 2: time is out of range"},
		"ignored lines": {"a", "S 0 SIGTERM\nH 0 SHELL /bin/sh\nH 0 EMPTY\nI 0 5\n", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 20, 1)
			err := Attach(t.Context(), s, strings.NewReader(tt.typescript), WithTiming(strings.NewReader(tt.timing)))
			switch {
			case tt.err == "" && err != nil:
				t.Fatal(err)
			case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
				t.Fatalf("Attach = %v, want an error containing %q", err, tt.err)
			}
		})
	}

	s := openTest(t, 20, 1)
	err := Attach(t.Context(), s, strings.NewReader("ab"), WithTiming(strings.NewReader("0 3\n")))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Attach of a short typescript = %v, want io.ErrUnexpectedEOF", err)
	}
	// What there was is written.
	if text := strings.TrimSpace(s.Terminal().Text()); text != "ab" {
		t.Fatalf("terminal = %q, want ab", text)
	}
}
