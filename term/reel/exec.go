// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lrstanley/x/term/reel/internal/pty"
)

// exitQuiet is how long an exited process's output may go quiet before
// [Process.Wait] stops reading it. Waiting for EOF alone is not enough: a
// background child may hold the pty open, and ConPTY output only ends when
// it is closed.
const exitQuiet = 100 * time.Millisecond

// Process is a command [Exec] runs on a [Session].
type Process struct {
	done chan struct{}
	// status and err are set before done is closed.
	status int
	err    error
}

// Exec starts cmd on a pseudo-terminal the size of the session's terminal
// and runs it as the session's program: its output is written to the
// session (and recorded), and the session's input and the terminal's query
// replies are written to it. [Session.Resize] also resizes the
// pseudo-terminal. While the process runs, nothing else should read the
// session, and no other process can be run on it.
//
// The environment is cmd.Env, or the host's when nil. TERM is set to
// "xterm-ghostty" if its terminfo entry is installed (searched as ncurses
// does, excluding hashed databases) and "xterm-256color" otherwise, and
// COLORTERM to "truecolor". TERM and COLORTERM from cmd.Env are kept; the
// host's are replaced, since they describe the host's terminal.
//
// cmd's Stdin, Stdout, and Stderr must be nil. On Unix, cmd runs in a new
// session with the pseudo-terminal as its controlling terminal (Exec sets
// cmd.SysProcAttr.Setsid and Setctty). On Windows it runs on a ConPTY;
// elsewhere Exec returns an error wrapping [errors.ErrUnsupported].
//
// Canceling ctx kills the process (on Unix, its whole process group).
// Closing the session hangs up on it: SIGHUP on Unix, ConPTY close on
// Windows.
func Exec(ctx context.Context, s *Session, cmd *exec.Cmd) (*Process, error) {
	switch {
	case cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil:
		return nil, errors.New("reel: Exec: cmd.Stdin, Stdout, and Stderr must be nil")
	case cmd.Process != nil:
		return nil, errors.New("reel: Exec: cmd already started")
	case cmd.Err != nil:
		return nil, cmd.Err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd.Env = pty.Env(cmd.Environ(), cmd.Env == nil, castTermType)
	pt, err := s.startProcess(cmd)
	if err != nil {
		return nil, err
	}
	p := &Process{done: make(chan struct{})}
	go p.run(ctx, s, cmd, pt)
	return p, nil
}

// Wait waits for the process to exit and its output to reach the session,
// and returns its exit status. An [Exit] event is appended after the output
// unless the session was closed first. On Unix, a process killed by a
// signal has status 128+signal, as shells report it (137 for SIGKILL).
//
// The error is ctx's if it was canceled before the process exited, or one
// from waiting on it; a nonzero exit status is not an error. Wait is safe
// to call repeatedly and concurrently, and always returns the same result.
func (p *Process) Wait() (int, error) {
	<-p.done
	return p.status, p.err
}

// startProcess starts cmd on a new pty the size of the terminal, and
// registers it for [Session.Resize].
func (s *Session) startProcess(cmd *exec.Cmd) (*pty.PTY, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed.Load():
		return nil, ErrClosed
	case s.running:
		return nil, errors.New("reel: Exec: a process is already running on the session")
	}
	pt, err := pty.Open(s.term.Size())
	if err != nil {
		return nil, fmt.Errorf("reel: Exec: opening a pseudo-terminal: %w", err)
	}
	if err = pt.Start(cmd); err != nil {
		return nil, fmt.Errorf("reel: Exec: %w", err)
	}
	s.pty, s.running = pt, true
	return pt, nil
}

// run copies the process's output and input until it exits, then records
// the exit.
func (p *Process) run(ctx context.Context, s *Session, cmd *exec.Cmd, pt *pty.PTY) {
	defer close(p.done)

	// hangup detaches the pty from Session.Resize and closes it, unblocking
	// the copies.
	hangup := sync.OnceFunc(func() {
		s.mu.Lock()
		s.pty = nil
		s.mu.Unlock()
		_ = pt.Close()
	})

	// reads lets drain tell when the output has gone quiet.
	var reads atomic.Uint64
	outDone, inDone := make(chan struct{}), make(chan struct{})
	inCtx, stopInput := context.WithCancel(ctx)
	defer stopInput()
	go func() {
		defer close(outDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := pt.Read(buf)
			if n > 0 {
				reads.Add(1)
				if _, werr := s.Write(buf[:n]); werr != nil {
					// The session closed.
					hangup()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer close(inDone)
		buf := make([]byte, 4096)
		for {
			n, err := s.readContext(inCtx, buf)
			if n > 0 {
				if _, werr := pt.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if s.closed.Load() {
					hangup()
				}
				return
			}
		}
	}()

	kill := context.AfterFunc(ctx, func() { _ = pty.Kill(cmd) })
	werr := pty.Wait(cmd)
	canceled := !kill()
	if !canceled {
		drain(ctx, outDone, &reads)
	}
	hangup()
	<-outDone
	stopInput()
	<-inDone

	p.status = -1
	if cmd.ProcessState != nil {
		p.status = pty.ExitStatus(cmd.ProcessState)
	}
	var exitErr *exec.ExitError
	switch {
	case canceled:
		p.err = ctx.Err()
	case !errors.As(werr, &exitErr):
		p.err = werr
	}
	// Clearing running with the Exit event lets the next process start only
	// after this one's exit is recorded. No-op once the session has closed.
	_ = s.change(func() (Event, error) {
		s.running = false
		return Exit{Status: p.status}, nil
	})
}

// drain waits until an exited process's output ends, goes quiet for
// exitQuiet, or ctx is done.
func drain(ctx context.Context, outDone <-chan struct{}, reads *atomic.Uint64) {
	timer := time.NewTimer(exitQuiet)
	defer timer.Stop()
	last := reads.Load()
	for {
		select {
		case <-outDone:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			n := reads.Load()
			if n == last {
				return
			}
			last = n
			timer.Reset(exitQuiet)
		}
	}
}
