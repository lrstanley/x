// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package main

import (
	"io"
	"os"
	"syscall"
)

// canInterrupt is whether [interruptible]'s stop ends a read.
const canInterrupt = true

// interruptible returns a reader of the controlling terminal whose reads
// stop ends. It reads a non-blocking open of /dev/tty of its own, through
// the runtime's poller: standard input, output, and error usually share one
// open of the terminal, so making standard input non-blocking would make
// writes to standard output fail when the terminal falls behind.
func interruptible() (r io.Reader, stop func(), err error) {
	fd, err := syscall.Open("/dev/tty", syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, &os.PathError{Op: "open", Path: "/dev/tty", Err: err}
	}
	tty := os.NewFile(uintptr(fd), "/dev/tty") //nolint:gosec // fd came from Open.
	// Close wakes a read in the poller, and waits for it to end.
	return tty, func() { _ = tty.Close() }, nil
}
