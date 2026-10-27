// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package reel

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// bash returns a command running script with bash, skipping the test when
// there is no bash.
func bash(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	return exec.Command(path, "--norc", "--noprofile", "-c", script)
}

// execTest runs cmd on s, skipping the test when the host has no
// pseudo-terminals for it.
func execTest(ctx context.Context, t *testing.T, s *Session, cmd *exec.Cmd) *Process {
	t.Helper()
	p, err := Exec(ctx, s, cmd)
	if err != nil {
		if strings.Contains(err.Error(), "opening a pseudo-terminal") {
			t.Skipf("no pseudo-terminal: %v", err)
		}
		t.Fatal(err)
	}
	return p
}

// wait waits for p, failing the test unless it exits with status want.
func wait(t *testing.T, p *Process, want int) {
	t.Helper()
	status, err := p.Wait()
	if err != nil || status != want {
		t.Fatalf("Wait = %d, %v, want %d", status, err, want)
	}
}

// waitText waits for the terminal of s to show text.
func waitText(t *testing.T, s *Session, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(s.Terminal().Text(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("terminal never showed %q:\n%s", text, s.Terminal().Text())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestExecRecordsFrames(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 2)
	p := execTest(t.Context(), t, s, bash(t, "printf one; sleep 0.2; printf ' two'"))
	wait(t, p, 0)

	tl := s.Timeline()
	events := slices.Collect(tl.Events())
	if exit, ok := events[len(events)-1].(Exit); !ok || exit.Status != 0 {
		t.Fatalf("last event = %#v, want Exit{0}", events[len(events)-1])
	}
	frames := exportFrames(t, tl, Profile{FPS: 100, Hold: -1})
	var texts []string
	for _, f := range frames {
		texts = append(texts, frameText(f))
	}
	if !slices.Equal(texts, []string{"one", "one two"}) {
		t.Fatalf("frames = %q, want one, one two", texts)
	}
	// Output is stamped when it is read, a little after it is written, so
	// the gap is the sleep give or take the latency of the two reads.
	if gap := frames[1].At - frames[0].At; gap < 180*ms {
		t.Fatalf("second frame %v after the first, want about 200ms", gap)
	}

	// The real renderer encodes the same frames to a GIF.
	path := filepath.Join(t.TempDir(), "exec.gif")
	if err := Export(t.Context(), tl, Profile{Outputs: Files(path)}); err != nil {
		t.Fatal(err)
	}
	if g := readGIF(t, path); len(g.Image) < 2 {
		t.Fatalf("GIF has %d frames, want at least 2", len(g.Image))
	}
}

// TestExecEmptyPrompt checks that a shell sitting on an empty prompt, which
// draws nothing, is recorded from its first frame.
func TestExecEmptyPrompt(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 2)
	cmd := bash(t, "")
	cmd.Args = []string{cmd.Path, "--norc", "--noprofile", "-i"}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "PS1=", "HISTFILE=/dev/null"}
	p := execTest(t.Context(), t, s, cmd)
	time.Sleep(300 * time.Millisecond)
	if err := s.Mark("typed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Type("exit\r"); err != nil {
		t.Fatal(err)
	}
	wait(t, p, 0)

	var typed time.Duration
	for m := range s.Timeline().Marks() {
		typed = m.At()
	}
	frames := exportFrames(t, s.Timeline(), Profile{FPS: 100, Hold: -1})
	if first := frames[0]; first.At >= typed || frameText(first) != "" {
		t.Fatalf("first frame %q at %v, want a blank one before the typing at %v", frameText(first), first.At, typed)
	}
}

func TestExecEnv(t *testing.T) {
	t.Parallel()
	const script = `printf '%s|%s' "$TERM" "$COLORTERM"`
	term := "xterm-256color"
	if hasTerminfo(append(terminfoPath(os.Environ()), systemTerminfo...), castTermType) {
		term = castTermType
	}
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"inherited", nil, term + "|truecolor"},
		{"set", []string{"TERM=dumb", "COLORTERM=24bit"}, "dumb|24bit"},
		{"without", []string{"PATH=" + os.Getenv("PATH")}, term + "|truecolor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 40, 1)
			cmd := bash(t, script)
			cmd.Env = tt.env
			wait(t, execTest(t.Context(), t, s, cmd), 0)
			if got := strings.TrimSpace(s.Terminal().Text()); got != tt.want {
				t.Fatalf("TERM|COLORTERM = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecEnvRules(t *testing.T) {
	t.Parallel()
	got := execEnv([]string{"A=1", "TERM=screen", "COLORTERM=no"}, true)
	if got[0] != "A=1" || len(got) != 3 || !strings.HasPrefix(got[1], "TERM=") || got[1] == "TERM=screen" || got[2] != "COLORTERM=truecolor" {
		t.Fatalf("inherited env = %q, want the host's TERM and COLORTERM replaced", got)
	}
	if kept := execEnv([]string{"TERM=screen", "COLORTERM=no"}, false); !slices.Equal(kept, []string{"TERM=screen", "COLORTERM=no"}) {
		t.Fatalf("env = %q, want TERM and COLORTERM kept", kept)
	}

	if hasTerminfo(terminfoPath([]string{"TERMINFO=" + t.TempDir()}), castTermType) {
		t.Fatal("found an entry in an empty directory")
	}
	for _, sub := range []string{"x", "78"} {
		dir, home := t.TempDir(), t.TempDir()
		fileTerminfo(t, dir, sub)
		fileTerminfo(t, filepath.Join(home, ".terminfo"), sub)
		for _, env := range [][]string{
			{"TERMINFO=" + dir},
			{"TERMINFO_DIRS=/nonexistent::" + dir},
			{"HOME=/nonexistent", "HOME=" + home},
		} {
			if !hasTerminfo(terminfoPath(env), castTermType) {
				t.Errorf("%s/%s: no entry found with %q", sub, castTermType, env)
			}
		}
	}
}

// fileTerminfo files an empty terminfo entry for xterm-ghostty in dir, under
// sub.
func fileTerminfo(t *testing.T, dir, sub string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sub, castTermType), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecResize(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 3)
	p := execTest(t.Context(), t, s, bash(t, `trap 'stty size; exit' WINCH; stty size; while :; do sleep 0.01; done`))
	waitText(t, s, "3 20")
	if err := s.Resize(30, 5); err != nil {
		t.Fatal(err)
	}
	wait(t, p, 0)
	if text := s.Terminal().Text(); !strings.Contains(text, "5 30") {
		t.Fatalf("after the resize the program saw:\n%s\nwant 5 30", text)
	}
}

func TestExecInput(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 3)
	p := execTest(t.Context(), t, s, bash(t, `read -r line; printf 'got:%s' "$line"`))
	if err := s.Type("hi\r"); err != nil {
		t.Fatal(err)
	}
	wait(t, p, 0)
	if text := s.Terminal().Text(); !strings.Contains(text, "got:hi") {
		t.Fatalf("terminal:\n%s\nwant got:hi", text)
	}
	if n := len(slices.Collect(s.Timeline().Inputs())); n != 1 {
		t.Fatalf("%d input events, want 1", n)
	}
}

func TestExecExitStatus(t *testing.T) {
	t.Parallel()
	for script, want := range map[string]int{"exit 3": 3, "kill -TERM $$": 128 + 15} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 20, 1)
			wait(t, execTest(t.Context(), t, s, bash(t, script)), want)
			var last Event
			for ev := range s.Timeline().Events() {
				last = ev
			}
			if exit, ok := last.(Exit); !ok || exit.Status != want {
				t.Fatalf("last event = %#v, want Exit{%d}", last, want)
			}
		})
	}
}

func TestExecRejects(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 1)
	cmd := bash(t, "sleep 10")
	cmd.Stdout = os.Stdout
	if _, err := Exec(t.Context(), s, cmd); err == nil {
		t.Fatal("Exec with cmd.Stdout set succeeded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	p := execTest(ctx, t, s, bash(t, "sleep 10"))
	t.Cleanup(func() {
		cancel()
		_, _ = p.Wait()
	})
	if _, err := Exec(t.Context(), s, bash(t, "true")); err == nil {
		t.Fatal("second Exec on the session succeeded")
	}
}

// TestExecStops checks that canceling ctx and closing the session each end
// the process and every goroutine Exec started. It is not parallel, so the
// goroutines it counts are its own.
func TestExecStops(t *testing.T) {
	before := runtime.NumGoroutine()
	// bash backgrounds a sleep that holds the terminal open, prints ready,
	// and waits for the sleep, unless the test ends it first.
	const sleeps = "sleep 10 & printf ready; wait"
	tests := []struct {
		name   string
		script string
		stop   func(cancel context.CancelFunc, s *Session)
		status int
		err    error
	}{
		{"cancel", sleeps, func(cancel context.CancelFunc, _ *Session) { cancel() }, 128 + 9, context.Canceled},
		{"close", sleeps, func(_ context.CancelFunc, s *Session) { _ = s.Close() }, 128 + 1, nil},
		// A process that ignores the hangup and holds the terminal open
		// outlives bash, which exits at once; Wait does not wait for it.
		{"linger", "(trap '' HUP; exec sleep 3) & printf ready", nil, 0, nil},
		// More input than the terminal queues, which a program in raw mode
		// never reads, blocks the write of it until the pty closes.
		{"unread input", "stty raw -echo; printf ready; sleep 0.3", func(_ context.CancelFunc, s *Session) {
			_ = s.Type(strings.Repeat("x", 20000))
		}, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTest(t, 20, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p := execTest(ctx, t, s, bash(t, tt.script))
			waitText(t, s, "ready")
			start := time.Now()
			if tt.stop != nil {
				tt.stop(cancel, s)
			}
			status, err := p.Wait()
			if status != tt.status || !errors.Is(err, tt.err) {
				t.Fatalf("Wait = %d, %v, want %d, %v", status, err, tt.status, tt.err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Fatalf("Wait took %v", d)
			}
		})
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines left running, %d before:\n%s", runtime.NumGoroutine(), before, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}
