// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package pty

import (
	"context"
	"os"
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

// PTY is the ConPTY a process runs on.
type PTY struct {
	conpty *xpty.ConPty
}

// Open opens a ConPTY of cols by rows cells.
func Open(cols, rows int) (*PTY, error) {
	p, err := xpty.NewConPty(cols, rows)
	if err != nil {
		return nil, err
	}
	return &PTY{conpty: p}, nil
}

// Read reads what the process writes to the terminal.
func (p *PTY) Read(b []byte) (int, error) { return p.conpty.Read(b) }

// Write writes b to the process's terminal, as typed input.
func (p *PTY) Write(b []byte) (int, error) { return p.conpty.Write(b) }

// Resize resizes the ConPTY to cols by rows cells.
func (p *PTY) Resize(cols, rows int) error { return p.conpty.Resize(cols, rows) }

// Start spawns cmd attached to the ConPTY. The ConPTY is closed if it fails.
func (p *PTY) Start(cmd *exec.Cmd) error {
	if err := p.conpty.Start(cmd); err != nil {
		_ = p.conpty.Close()
		return err
	}
	return nil
}

// Close closes the ConPTY, which ends the processes attached to it, and the
// reads and writes blocked on it.
func (p *PTY) Close() error { return p.conpty.Close() }

// Kill kills the process cmd started.
func Kill(cmd *exec.Cmd) error { return cmd.Process.Kill() }

// Wait waits for cmd, which ConPTY started rather than cmd.Start, to exit.
func Wait(cmd *exec.Cmd) error { return xpty.WaitProcess(context.Background(), cmd) }

// ExitStatus returns the exit code of a process.
func ExitStatus(ps *os.ProcessState) int { return ps.ExitCode() }
