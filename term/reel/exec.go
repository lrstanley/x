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
)

// exitQuiet is how long the output of a process that has exited may go
// quiet before [Process.Wait] stops reading it. The output of a pty ends on
// its own once every process holding the terminal has closed it, but a
// process the program left running in the background may hold it open, and
// ConPTY's output only ends when it is closed.
const exitQuiet = 100 * time.Millisecond

// Process is a command [Exec] runs on a [Session].
type Process struct {
	done chan struct{}
	// status and err are the result, set before done is closed.
	status int
	err    error
}

// Exec starts cmd on a pseudo-terminal the size of the session's terminal,
// and runs it as the session's program: what it writes to the terminal is
// written to the session with [Session.Write], recording it, and what the
// session sends it, and the terminal's replies to its queries, are read with
// [Session.Read] and written to it. [Session.Resize] resizes the
// pseudo-terminal too, which tells the program, as SIGWINCH does on Unix.
// While the process runs, nothing else should read the session, and no
// other process can be run on it.
//
// Exec sets the environment cmd runs with: cmd.Env, or the host's when it
// is nil, with TERM set to "xterm-ghostty" when its terminfo entry is
// installed and to "xterm-256color" otherwise, and COLORTERM set to
// "truecolor". A TERM or COLORTERM in cmd.Env is kept, but the host's are
// not, since they describe the host's terminal rather than the session's.
// The entry is looked for as ncurses looks, in the environment cmd runs
// with: in $TERMINFO, ~/.terminfo, each directory in $TERMINFO_DIRS,
// /etc/terminfo, /lib/terminfo, /usr/share/terminfo, and /usr/lib/terminfo,
// under both its first letter and that letter in hex ("x" and "78"). A
// hashed terminfo database is not read.
//
// cmd's Stdin, Stdout, and Stderr must be nil, since the pseudo-terminal is
// all three. On Unix, cmd runs in a session of its own, with the
// pseudo-terminal as its controlling terminal; Exec sets
// cmd.SysProcAttr.Setsid and Setctty. On Windows it runs on a ConPTY, and
// elsewhere Exec returns an error wrapping [errors.ErrUnsupported].
//
// Canceling ctx kills the process, and on Unix every process in its process
// group. Closing the session hangs up on it: on Unix its terminal hangs up,
// which sends it SIGHUP, and on Windows the ConPTY closes, which ends it.
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
	cmd.Env = execEnv(cmd.Environ(), cmd.Env == nil)
	pt, err := s.startProcess(cmd)
	if err != nil {
		return nil, err
	}
	p := &Process{done: make(chan struct{})}
	go p.run(ctx, s, cmd, pt)
	return p, nil
}

// Wait waits for the process to exit and for its output to be written to
// the session, and returns its exit status. The exit is appended to the
// session's timeline as an [Exit] event with the status, after the output,
// unless the session was closed first. On Unix, a process ended by a signal
// has the status 128 plus the signal's number, as shells report it: 137 for
// SIGKILL, 129 for SIGHUP.
//
// The error is ctx's when ctx was canceled before the process exited, or
// one from waiting for it; an exit status other than zero is not an error.
// Wait may be called more than once, and from more than one goroutine, and
// returns the same result each time.
func (p *Process) Wait() (int, error) {
	<-p.done
	return p.status, p.err
}

// startProcess starts cmd on a new pty the size of the terminal, which
// [Session.Resize] resizes from then on.
func (s *Session) startProcess(cmd *exec.Cmd) (*pty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed.Load():
		return nil, ErrClosed
	case s.running:
		return nil, errors.New("reel: Exec: a process is already running on the session")
	}
	pt, err := openPty(s.term.Size())
	if err != nil {
		return nil, fmt.Errorf("reel: Exec: opening a pseudo-terminal: %w", err)
	}
	if err = pt.start(cmd); err != nil {
		return nil, fmt.Errorf("reel: Exec: %w", err)
	}
	s.pty, s.running = pt, true
	return pt, nil
}

// run copies the process's output and input until it exits, then records
// its exit.
func (p *Process) run(ctx context.Context, s *Session, cmd *exec.Cmd, pt *pty) {
	defer close(p.done)

	// hangup stops the copies: Session.Resize stops resizing the pty, and
	// the pty closes, which ends the reads and writes blocked on it.
	hangup := sync.OnceFunc(func() {
		s.mu.Lock()
		s.pty = nil
		s.mu.Unlock()
		// The process has exited or is being hung up on, so nothing is left
		// to report about the pty.
		_ = pt.close()
	})

	// reads counts the reads of output, so the drain below can tell when
	// it has gone quiet.
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

	kill := context.AfterFunc(ctx, func() { _ = killProcess(cmd) })
	werr := waitProcess(cmd)
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
		p.status = exitStatus(cmd.ProcessState)
	}
	var exitErr *exec.ExitError
	switch {
	case canceled:
		p.err = ctx.Err()
	case !errors.As(werr, &exitErr):
		p.err = werr
	}
	// Another process can run once the exit is recorded, after this one's
	// output. Nothing is appended once the session has closed.
	_ = s.change(func() (Event, error) {
		s.running = false
		return Exit{Status: p.status}, nil
	})
}

// drain waits for the output of a process that has exited to end, to go
// quiet for exitQuiet, or for ctx to be done.
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
