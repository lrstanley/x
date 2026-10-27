// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/charmbracelet/x/xpty"
)

// PTY is a pseudo-terminal a process runs on.
type PTY struct {
	unix *xpty.UnixPty
	// master is a copy of the master that Go's poller watches, which reads
	// and writes go through. creack/pty leaves the master in blocking mode,
	// where closing it ends neither a read blocked on it nor a write blocked
	// on a program that stopped reading its input; closing this copy ends
	// both. The master itself is kept for resizing.
	master *os.File
}

// Open opens a pseudo-terminal of cols by rows cells.
func Open(cols, rows int) (*PTY, error) {
	p, err := xpty.NewUnixPty(cols, rows)
	if err != nil {
		return nil, err
	}
	master, err := pollable(p.Master())
	if err != nil {
		return nil, errors.Join(err, p.Close())
	}
	return &PTY{unix: p, master: master}, nil
}

// pollable returns a non-blocking copy of f that Go's poller watches, or an
// error when it cannot watch it. The copy shares f's open file, which is
// non-blocking from then on.
func pollable(f *os.File) (*os.File, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Dup(int(f.Fd()))
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	if err = syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	c := os.NewFile(uintptr(fd), f.Name())
	// Deadlines are only for files the poller watches.
	if err = c.SetDeadline(time.Time{}); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("the poller cannot watch the master: %w", err)
	}
	return c, nil
}

// Read reads what the process writes to the terminal.
func (p *PTY) Read(b []byte) (int, error) { return p.master.Read(b) }

// Write writes b to the process's terminal, as typed input.
func (p *PTY) Write(b []byte) (int, error) { return p.master.Write(b) }

// Resize resizes the terminal to cols by rows cells, which sends SIGWINCH
// to its foreground process group.
func (p *PTY) Resize(cols, rows int) error { return p.unix.Resize(cols, rows) }

// Start starts cmd in a session of its own, with the slave as its
// controlling terminal and its standard streams. The PTY is closed if it
// fails.
func (p *PTY) Start(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Ctty is the child's descriptor 0, its standard input.
	cmd.SysProcAttr.Setsid, cmd.SysProcAttr.Setctty, cmd.SysProcAttr.Ctty = true, true, 0
	if err := p.unix.Start(cmd); err != nil {
		return errors.Join(err, p.master.Close(), p.unix.Close())
	}
	// The parent's copy of the slave would keep the master's reads from
	// ending when the process exits. Failing to close it only leaves that
	// to the drain after the exit.
	_ = p.unix.Slave().Close()
	return nil
}

// Close closes the master, which hangs up the terminal, sending SIGHUP to
// its foreground process group, and ends the reads and writes blocked on
// it.
func (p *PTY) Close() error {
	return errors.Join(p.master.Close(), p.unix.Master().Close())
}

// Kill kills the process group cmd leads, which is its session's.
func Kill(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// Wait waits for cmd, which [PTY.Start] started, to exit.
func Wait(cmd *exec.Cmd) error { return cmd.Wait() }

// ExitStatus returns the status a shell reports for a process: its exit
// code, or 128 plus the signal that ended it.
func ExitStatus(ps *os.ProcessState) int {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}
