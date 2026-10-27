// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package pty runs a process on a pseudo-terminal: a pty on Unix, and a
// ConPTY on Windows. Elsewhere, [Open] returns [errors.ErrUnsupported].
// [Env] returns the environment a process on one runs with.
package pty
