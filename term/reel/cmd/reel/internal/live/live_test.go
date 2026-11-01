// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package live

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// serve starts a session of sh in a new directory, and returns the
// directory and the session's result once it stops.
func serve(t *testing.T, linger time.Duration) (dir string, done <-chan error) {
	t.Helper()
	// Socket paths are limited to about 100 bytes, which t.TempDir can pass.
	dir, err := os.MkdirTemp("", "reel") //nolint:usetesting // See above.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ready := make(chan error, 1)
	served := make(chan error, 1)
	go func() {
		served <- Serve(t.Context(), Config{
			Dir: dir, Name: "sh", Cols: 40, Rows: 5, Linger: linger,
			Command: []string{"sh"}, Env: []string{"PS1=$ "},
			Ready: func(err error) { ready <- err },
		})
	}()
	if err = <-ready; err != nil {
		t.Fatal(err)
	}
	return dir, served
}

func call(t *testing.T, dir string, req Request) *Response {
	t.Helper()
	resp, err := Call(t.Context(), dir, "sh", req)
	if err != nil {
		t.Fatalf("%s: %v", req.Op, err)
	}
	return resp
}

func TestSession(t *testing.T) {
	dir, done := serve(t, time.Minute)
	// Typed before the shell reads it, the line would be echoed ahead of the
	// prompt, so the output would follow the prompt on its line.
	call(t, dir, Request{Op: OpWait, Wait: &Wait{Text: "$ ", Timeout: 5 * time.Second}})
	call(t, dir, Request{Op: OpType, Text: "echo hel''lo\r"})
	if resp := call(t, dir, Request{Op: OpWait, Wait: &Wait{Regexp: "(?m)^hello", Timeout: 5 * time.Second}}); !strings.Contains(resp.Text, "hello") {
		t.Errorf("wait text = %q", resp.Text)
	}
	call(t, dir, Request{Op: OpMark, Label: "step:1"})
	if resp := call(t, dir, Request{Op: OpScreen, JSON: true}); resp.Screen == nil || resp.Screen.Cols != 40 || strings.HasSuffix(resp.Text, " ") {
		t.Errorf("screen = %+v, text %q; want the 40-column screen, tidied", resp.Screen, resp.Text)
	}
	call(t, dir, Request{Op: OpDo, Text: "Type \"echo do-ne\"\nEnter\nWait+Screen /do-ne\\n/"})

	_, err := Call(t.Context(), dir, "sh", Request{Op: OpWait, Wait: &Wait{Text: "never", Timeout: 100 * time.Millisecond}})
	var lerr *Error
	if !errors.As(err, &lerr) || lerr.Code != CodeTimeout || !strings.Contains(lerr.Screen, "do-ne") {
		t.Errorf("wait error = %#v, want a timeout with the screen", err)
	}
	_, err = Call(t.Context(), dir, "sh", Request{Op: "dance"})
	if !errors.As(err, &lerr) || lerr.Code != CodeInvalid {
		t.Errorf("unknown op error = %v, want invalid", err)
	}

	infos, err := List(t.Context(), dir)
	if err != nil || len(infos) != 1 || infos[0].Name != "sh" || infos[0].Exited {
		t.Fatalf("List = %+v, %v", infos, err)
	}
	tl, err := reel.ReadTimeline(bytes.NewReader(call(t, dir, Request{Op: OpCast}).Cast))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tl.Between("step:1", ""); err != nil {
		t.Errorf("cast lacks the mark: %v", err)
	}

	call(t, dir, Request{Op: OpStop})
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = Call(t.Context(), dir, "sh", Request{Op: OpInfo}); !errors.Is(err, ErrNoSession) {
		t.Errorf("after stop: %v, want no session", err)
	}
}

// TestSessionExited checks that a session stays up after its program
// exits, refusing input, until it lingers out.
func TestSessionExited(t *testing.T) {
	dir, done := serve(t, 300*time.Millisecond)
	call(t, dir, Request{Op: OpType, Text: "exit 3\r"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		info := call(t, dir, Request{Op: OpInfo}).Info
		if info.Exited {
			if info.Status != 3 {
				t.Errorf("status = %d, want 3", info.Status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the program never exited")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := Call(t.Context(), dir, "sh", Request{Op: OpType, Text: "x"})
	var lerr *Error
	if !errors.As(err, &lerr) || lerr.Code != CodeExited {
		t.Errorf("type after exit = %v, want exited", err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not linger out")
	}
}

func TestServeRejects(t *testing.T) {
	dir, _ := serve(t, time.Minute)
	err := Serve(context.Background(), Config{Dir: dir, Name: "sh", Cols: 10, Rows: 2, Command: []string{"sh"}})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second session = %v, want already running", err)
	}
	err = Serve(context.Background(), Config{Dir: dir, Name: "../x", Cols: 10, Rows: 2, Command: []string{"sh"}})
	if err == nil || !strings.Contains(err.Error(), "invalid session name") {
		t.Errorf("bad name = %v", err)
	}
}
