// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !unix && !windows

package reel

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// pty stands in for a pseudo-terminal where there is none.
type pty struct{ io.ReadWriter }

func openPty(int, int) (*pty, error) { return nil, errors.ErrUnsupported }

func (*pty) start(*exec.Cmd) error       { return errors.ErrUnsupported }
func (*pty) close() error                { return nil }
func (*pty) Resize(int, int) error       { return errors.ErrUnsupported }
func killProcess(*exec.Cmd) error        { return errors.ErrUnsupported }
func waitProcess(*exec.Cmd) error        { return errors.ErrUnsupported }
func exitStatus(ps *os.ProcessState) int { return ps.ExitCode() }
