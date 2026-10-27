// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !unix && !windows

package pty

import (
	"errors"
	"os"
	"os/exec"
)

// PTY stands in for a pseudo-terminal where there is none. [Open] fails, so
// there is never one.
type PTY struct{}

// Open returns [errors.ErrUnsupported].
func Open(int, int) (*PTY, error) { return nil, errors.ErrUnsupported }

// Read returns [errors.ErrUnsupported].
func (*PTY) Read([]byte) (int, error) { return 0, errors.ErrUnsupported }

// Write returns [errors.ErrUnsupported].
func (*PTY) Write([]byte) (int, error) { return 0, errors.ErrUnsupported }

// Resize returns [errors.ErrUnsupported].
func (*PTY) Resize(int, int) error { return errors.ErrUnsupported }

// Start returns [errors.ErrUnsupported].
func (*PTY) Start(*exec.Cmd) error { return errors.ErrUnsupported }

// Close does nothing.
func (*PTY) Close() error { return nil }

// Kill returns [errors.ErrUnsupported].
func Kill(*exec.Cmd) error { return errors.ErrUnsupported }

// Wait returns [errors.ErrUnsupported].
func Wait(*exec.Cmd) error { return errors.ErrUnsupported }

// ExitStatus returns the exit code of a process.
func ExitStatus(ps *os.ProcessState) int { return ps.ExitCode() }
