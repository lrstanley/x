// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"errors"
	"fmt"
)

var (
	// ErrUnavailable is returned when libghostty-vt could not be loaded or
	// resolved from this process.
	ErrUnavailable = errors.New("vt: libghostty-vt is not available")

	// ErrClosed is returned when a terminal handle has already been freed.
	ErrClosed = errors.New("vt: terminal is closed")
)

// GhosttyError wraps a non-success status code from libghostty-vt.
type GhosttyError struct {
	Code int32
}

func (e *GhosttyError) Error() string {
	return fmt.Sprintf("vt: ghostty returned status %d", e.Code)
}

func ghosttyErr(code int32) error {
	if code == ghosttySuccess {
		return nil
	}
	return &GhosttyError{Code: code}
}
