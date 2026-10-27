// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package reel

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/charmbracelet/x/xpty"
)

// pty is the pseudo-terminal [Exec] runs a process on.
type pty struct {
	*xpty.UnixPty
	// master is a copy of the master that Go's poller watches, which reads
	// and writes go through. creack/pty leaves the master in blocking mode,
	// where closing it ends neither a read blocked on it nor a write blocked
	// on a program that stopped reading its input; closing this copy ends
	// both. The master itself is kept for resizing.
	master *os.File
}

func openPty(cols, rows int) (*pty, error) {
	p, err := xpty.NewUnixPty(cols, rows)
	if err != nil {
		return nil, err
	}
	master, err := pollable(p.Master())
	if err != nil {
		return nil, errors.Join(err, p.Close())
	}
	return &pty{UnixPty: p, master: master}, nil
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

func (p *pty) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *pty) Write(b []byte) (int, error) { return p.master.Write(b) }

// start starts cmd in a session of its own, with the slave as its
// controlling terminal and its standard streams. The pty is closed if it
// fails.
func (p *pty) start(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Ctty is the child's descriptor 0, its standard input.
	cmd.SysProcAttr.Setsid, cmd.SysProcAttr.Setctty, cmd.SysProcAttr.Ctty = true, true, 0
	if err := p.Start(cmd); err != nil {
		return errors.Join(err, p.master.Close(), p.Close())
	}
	// The parent's copy of the slave would keep the master's reads from
	// ending when the process exits. Failing to close it only leaves that
	// to the drain after the exit.
	_ = p.Slave().Close()
	return nil
}

// close closes the master, which hangs up the terminal, sending SIGHUP to
// its foreground process group, and ends the reads and writes blocked on
// it.
func (p *pty) close() error {
	return errors.Join(p.master.Close(), p.Master().Close())
}

// killProcess kills the process group cmd leads, which is its session's.
func killProcess(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func waitProcess(cmd *exec.Cmd) error { return cmd.Wait() }

// exitStatus returns the status a shell reports for a process: its exit
// code, or 128 plus the signal that ended it.
func exitStatus(ps *os.ProcessState) int {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}
