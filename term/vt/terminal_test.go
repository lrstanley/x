// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteTextAndLinefeed(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 3)
	if _, err := term.Write([]byte("A\nB")); err != nil {
		t.Fatal(err)
	}
	cur := term.Cursor()
	if cur.X != 1 || cur.Y != 1 {
		t.Fatalf("cursor after LF = %d,%d, want 1,1", cur.X, cur.Y)
	}
	text := term.Text()
	if !strings.HasPrefix(text, "A") {
		t.Fatalf("text = %q", text)
	}
	line, _, _ := strings.Cut(text, "\n")
	if len(line) != 8 {
		t.Fatalf("line width = %d, text %q", len(line), text)
	}
}

func TestEnquiryReply(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32)
		n, err := term.Read(buf)
		if err != nil {
			got <- []byte(err.Error())
			return
		}
		got <- append([]byte(nil), buf[:n]...)
	}()
	if _, err := term.Write([]byte{0x05}); err != nil {
		t.Fatal(err)
	}
	select {
	case buf := <-got:
		if len(buf) == 0 {
			t.Fatal("empty enquiry reply")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enquiry produced no reply")
	}
}

func TestLiveInstancesNeverExceedCap(t *testing.T) {
	t.Parallel()
	const capN = 2
	rt := NewRuntime(capN)
	ctx := context.Background()
	var live sync.WaitGroup
	hold := make(chan struct{})
	errCh := make(chan error, capN+1)
	var mu sync.Mutex
	open := 0
	maxOpen := 0
	for range capN {
		live.Add(1)
		go func() {
			defer live.Done()
			term, err := rt.Open(ctx, 8, 2)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			open++
			if open > maxOpen {
				maxOpen = open
			}
			mu.Unlock()
			<-hold
			mu.Lock()
			open--
			mu.Unlock()
			term.Close()
		}()
	}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return open == capN
	})
	blocked := make(chan struct{})
	go func() {
		term, err := rt.Open(ctx, 8, 2)
		if err != nil {
			errCh <- err
			return
		}
		close(blocked)
		term.Close()
	}()
	select {
	case <-blocked:
		t.Fatal("open succeeded past the cap")
	case <-time.After(50 * time.Millisecond):
	}
	close(hold)
	live.Wait()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked open did not proceed after close")
	}
	mu.Lock()
	defer mu.Unlock()
	if maxOpen > capN {
		t.Fatalf("live instances = %d, cap = %d", maxOpen, capN)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestResetOnCloseReusesInstance(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	ctx := context.Background()
	first, err := rt.Open(ctx, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := rt.Open(ctx, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if strings.Contains(second.Text(), "hello") {
		t.Fatalf("reused terminal kept previous text %q", second.Text())
	}
}

func BenchmarkWriteOnly(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := term.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScreenOnly(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	if _, err := term.Write(payload); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		term.gridFresh = false
		if term.Screen() == nil {
			b.Fatal("nil screen")
		}
	}
}

func BenchmarkWriteAndText(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := term.Write(payload); err != nil {
			b.Fatal(err)
		}
		_ = term.Text()
	}
}

func BenchmarkWriteAndScreen(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := term.Write(payload); err != nil {
			b.Fatal(err)
		}
		if term.Screen() == nil {
			b.Fatal("nil screen")
		}
	}
}

func TestRenderHoldSkipsFrame(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("A\x1b[?2026hB")); err != nil {
		t.Fatal(err)
	}
	if !term.Held() {
		t.Fatal("hold did not start")
	}
	if !strings.Contains(term.Text(), "A") || strings.Contains(term.Text(), "B") {
		t.Fatalf("held text = %q", term.Text())
	}
	if _, err := term.Write([]byte("\x1b[?2026l")); err != nil {
		t.Fatal(err)
	}
	if term.Held() {
		t.Fatal("hold did not end")
	}
	if !strings.Contains(term.Text(), "B") {
		t.Fatalf("released text = %q", term.Text())
	}
}

func TestScrollbackReenabledOnCheckout(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	term, err := rt.Open(context.Background(), 4, 2, WithScrollbackLines(100))
	if err != nil {
		t.Fatal(err)
	}
	if err = term.SetScrollbackMaxLines(0); err != nil {
		t.Fatal(err)
	}
	if _, err = term.Write([]byte("a\nb\nc\nd\n")); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() != 0 {
		t.Fatalf("disabled scrollback = %d", term.ScrollbackLen())
	}
	if err = term.Close(); err != nil {
		t.Fatal(err)
	}

	term, err = rt.Open(context.Background(), 4, 2, WithScrollbackLines(100))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if _, err = term.Write([]byte("a\nb\nc\nd\n")); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() == 0 {
		t.Fatal("scrollback stayed disabled after checkout")
	}
}

func TestScrollbackCap(t *testing.T) {
	t.Parallel()
	term := openTest(t, 4, 2)
	var b strings.Builder
	for range 6 {
		b.WriteString("x\n")
	}
	if _, err := term.Write([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() == 0 {
		t.Fatal("expected scrollback")
	}
	lines := term.Scrollback()
	if len(lines) != term.ScrollbackLen() {
		t.Fatalf("lines %d count %d", len(lines), term.ScrollbackLen())
	}
	if err := term.ClearScrollback(); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() != 0 {
		t.Fatalf("cleared scrollback = %d", term.ScrollbackLen())
	}
}

func TestProgramStatusAndPrompt(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	seq := "\x1b]7501;state=working:id=build:app=compiler\x1b\\" +
		"\x1b]7501;state=blocked:id=build/test\x1b\\" +
		"\x1b]133;A\x1b\\"
	if _, err := term.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	got := term.ProgramStatuses()
	if len(got) != 0 {
		t.Fatalf("working records survived prompt: %+v", got)
	}
	if term.Prompt().Kind != PromptStart {
		t.Fatalf("prompt = %+v", term.Prompt())
	}
}

// testRuntime is this package's pool. Tests do not use a process-wide one.
var testRuntime = NewRuntime(0)

func openTest(t *testing.T, cols, rows int) *Terminal {
	t.Helper()
	term, err := testRuntime.Open(context.Background(), cols, rows, WithScrollbackLines(100))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	return term
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not reached")
}
