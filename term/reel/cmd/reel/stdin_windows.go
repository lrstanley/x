// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package main

import (
	"io"
	"os"
)

// canInterrupt is whether [interruptible]'s stop ends a read.
const canInterrupt = false

// interruptible returns standard input: a console read cannot be
// interrupted, so stop does nothing, and the reader returns at the next
// key or reel's exit.
func interruptible() (r io.Reader, stop func(), err error) {
	return os.Stdin, func() {}, nil
}
