// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package reel

import (
	"context"
	"os"
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

// pty is the ConPTY [Exec] runs a process on.
type pty struct {
	*xpty.ConPty
}

func openPty(cols, rows int) (*pty, error) {
	p, err := xpty.NewConPty(cols, rows)
	if err != nil {
		return nil, err
	}
	return &pty{ConPty: p}, nil
}

// start spawns cmd attached to the ConPTY. The ConPTY is closed if it fails.
func (p *pty) start(cmd *exec.Cmd) error {
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		return err
	}
	return nil
}

// close closes the ConPTY, which ends the processes attached to it, and the
// reads and writes blocked on it.
func (p *pty) close() error { return p.Close() }

func killProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }

// waitProcess waits for cmd, which ConPTY started rather than cmd.Start.
func waitProcess(cmd *exec.Cmd) error { return xpty.WaitProcess(context.Background(), cmd) }

func exitStatus(ps *os.ProcessState) int { return ps.ExitCode() }
