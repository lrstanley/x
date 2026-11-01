// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package live runs a program in a recorded terminal that other processes
// drive, one request at a time: a server holds the [reel.Session], and
// listens on a Unix socket named for the session in a directory only the
// user can open.
//
// Each connection carries one [Request] and its [Response], as JSON lines.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// Ops are what a [Request] asks of a session.
const (
	OpInfo   = "info"   // The session's [Info].
	OpType   = "type"   // Type Text, with Delay after each key.
	OpKey    = "key"    // Press each of Keys, Repeat times, with Delay after each.
	OpPaste  = "paste"  // Paste Text.
	OpClick  = "click"  // Click Button at X, Y.
	OpScroll = "scroll" // Scroll the viewport Repeat rows up, or down when negative.
	OpResize = "resize" // Resize to Cols by Rows.
	OpMark   = "mark"   // Mark Label.
	OpWait   = "wait"   // Wait for Wait; the Response has the screen's text.
	OpScreen = "screen" // The screen's text, and with JSON its [reel.Screen].
	OpCast   = "cast"   // The timeline so far, as an asciicast file.
	OpDo     = "do"     // Run the commands of the tape Text.
	OpStop   = "stop"   // End the program and the server.
)

// Request is one thing asked of a session.
type Request struct {
	Op     string        `json:"op"`
	Text   string        `json:"text,omitempty"`
	Keys   []string      `json:"keys,omitempty"`
	Repeat int           `json:"repeat,omitempty"`
	Delay  time.Duration `json:"delay,omitempty"`
	Button string        `json:"button,omitempty"`
	X      int           `json:"x,omitempty"`
	Y      int           `json:"y,omitempty"`
	Cols   int           `json:"cols,omitempty"`
	Rows   int           `json:"rows,omitempty"`
	Label  string        `json:"label,omitempty"`
	Wait   *Wait         `json:"wait,omitempty"`
	JSON   bool          `json:"json,omitempty"`
}

// Wait is what an [OpWait] waits for: one of Text, Regexp, Gone, or Idle.
type Wait struct {
	// Text waits for the text to show.
	Text string `json:"text,omitempty"`
	// Regexp waits for a match of the regular expression.
	Regexp string `json:"regexp,omitempty"`
	// Gone waits for the text not to show.
	Gone string `json:"gone,omitempty"`
	// Idle waits for the screen not to change for this long.
	Idle time.Duration `json:"idle,omitempty"`
	// CursorLine looks at only the cursor's line, for Text, Regexp, or Gone.
	CursorLine bool `json:"cursor_line,omitempty"`
	// Timeout ends the wait with an error; zero means 15 seconds.
	Timeout time.Duration `json:"timeout,omitempty"`
}

// Error codes of a [Response].
const (
	CodeTimeout = "timeout" // A wait timed out; Text is the last screen.
	CodeExited  = "exited"  // The program has exited, so it takes no input.
	CodeInvalid = "invalid" // The request is malformed.
	CodeFailed  = "failed"  // Anything else.
)

// Response is a session's answer to a [Request].
type Response struct {
	Error  string       `json:"error,omitempty"`
	Code   string       `json:"code,omitempty"`
	Text   string       `json:"text,omitempty"`
	Screen *reel.Screen `json:"screen,omitempty"`
	Cast   []byte       `json:"cast,omitempty"`
	Info   *Info        `json:"info,omitempty"`
}

// Info describes a session.
type Info struct {
	Name    string    `json:"name"`
	PID     int       `json:"pid"`
	Command []string  `json:"command"`
	Cols    int       `json:"cols"`
	Rows    int       `json:"rows"`
	Started time.Time `json:"started"`
	// Exited is set once the program has exited, with its Status.
	Exited bool `json:"exited,omitempty"`
	Status int  `json:"status,omitempty"`
}

// Error is a [Response] error, as [Call] returns it.
type Error struct {
	Code    string
	Message string
	// Screen is the last screen a wait that timed out saw.
	Screen string
}

func (e *Error) Error() string {
	if e.Screen == "" {
		return e.Message
	}
	return e.Message + "; the screen was:\n" + e.Screen
}

// ErrNoSession is the error for a session that is not running.
var ErrNoSession = errors.New("no such session")

// Dir returns the directory the sessions' sockets are in, creating it
// only the user can open: $REEL_RUNTIME_DIR, or reel in $XDG_RUNTIME_DIR,
// or a directory of the user's in the temporary directory.
func Dir() (string, error) {
	dir := os.Getenv("REEL_RUNTIME_DIR")
	if dir == "" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			dir = filepath.Join(xdg, "reel")
		} else {
			dir = filepath.Join(os.TempDir(), fmt.Sprintf("reel-%d", os.Getuid()))
		}
	}
	// The directory is the user's own choice, or their runtime directory.
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // G703: see above.
		return "", err
	}
	if err := checkDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// Socket returns the path of the socket of the session name.
func Socket(dir, name string) string { return filepath.Join(dir, name+".sock") }

// Log returns the path of the log of the server of the session name.
func Log(dir, name string) string { return filepath.Join(dir, name+".log") }

// errInvalidName is the error for a name [ValidName] rejects.
func errInvalidName(name string) error {
	return fmt.Errorf("invalid session name %q: use letters, digits, '.', '_', or '-'", name)
}

// ValidName reports whether name can name a session: letters, digits, and
// ".", "_", or "-", not starting with ".".
func ValidName(name string) bool {
	valid := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
	}
	return name != "" && name[0] != '.' && strings.IndexFunc(name, func(r rune) bool { return !valid(r) }) < 0
}

// Call sends req to the session name and returns its response. An error
// response is an *[Error]; a session that is not running is [ErrNoSession].
func Call(ctx context.Context, dir, name string, req Request) (*Response, error) {
	if !ValidName(name) {
		return nil, errInvalidName(name)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", Socket(dir, name))
	if err != nil {
		// Only a missing socket, or one nothing listens on, is a session
		// that is gone; anything else, such as a timeout, is reported.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			err = ErrNoSession
		}
		return nil, fmt.Errorf("session %q: %w", name, err)
	}
	defer conn.Close()
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()
	err = json.NewEncoder(conn).Encode(req)
	if err != nil {
		return nil, err
	}
	var resp Response
	err = json.NewDecoder(conn).Decode(&resp)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("session %q: %w", name, err)
	}
	if resp.Error != "" {
		return &resp, &Error{Code: resp.Code, Message: resp.Error, Screen: resp.Text}
	}
	return &resp, nil
}

// List returns the running sessions, by name, skipping the sockets of
// sessions that are gone, which only [Serve] removes, under the name's
// lock.
func List(ctx context.Context, dir string) ([]Info, error) {
	socks, err := filepath.Glob(filepath.Join(dir, "*.sock"))
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, sock := range socks {
		name := strings.TrimSuffix(filepath.Base(sock), ".sock")
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		resp, callErr := Call(cctx, dir, name, Request{Op: OpInfo})
		cancel()
		if errors.Is(callErr, ErrNoSession) {
			continue
		}
		if callErr != nil {
			return nil, callErr
		}
		out = append(out, *resp.Info)
	}
	return out, nil
}
