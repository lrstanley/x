// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// PanicPathName generates a path for the panic log file, in the format of:
//
//	<baseDir>/panic-<appName>-<timestamp>.log
func PanicPathName(baseDir, appName string) string {
	return filepath.Join(baseDir, fmt.Sprintf("panic-%s-%s.log", appName, time.Now().Format("20060102-150405")))
}

// NewPanicCatcher creates a new panic logger that will write to the specified path
// when a panic occurs, regardless of where it originates within the app. The
// returned closer should be called to ensure that the log file is cleaned up
// if no panic was caught. This approach will ensure that a panic is always logged,
// even if it's not correctly caught, but has the downside of always having to
// create a file before the panic can occur.
//
// The callback passed to the closer is called when the panic logger is closed,
// and can be used to perform any additional cleanup.
//
// See also [PanicPathName] for a helper function to generate the path for you.
func NewPanicCatcher(path string) (closer func(cb func()) error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create log directory:", err)
		os.Exit(1)
	}

	// SetCrashOutput doesn't support [io.Writer] interface, so we HAVE to create
	// a file. The workaround for this is a defer that will delete the file if it's
	// empty. So we will have empty files while the app is running, but it does
	// avoid a bunch of useless empty files building up.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create panic log file:", err)
		os.Exit(1)
	}

	err = debug.SetCrashOutput(f, debug.CrashOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to set crash output:", err)
		os.Exit(1)
	}

	_ = f.Close() // SetCrashOutput duplicates the file descriptor, so can safely close early.

	size := func() int {
		var stat os.FileInfo
		stat, err = os.Stat(path)
		if err != nil {
			return -1
		}
		return int(stat.Size())
	}

	return func(cb func()) error {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		time.Sleep(250 * time.Millisecond) // Allow concurrent goroutines to finish flushing crash logs.

		// Catch main goroutine panics.
		if r := recover(); r != nil && size() == 0 {
			stack := debug.Stack()
			slog.Error("panic occurred", "error", r, "stack", string(stack)) //nolint:sloglint

			f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
			if err == nil {
				_, _ = fmt.Fprintf(f, "panic occurred: %v\n%s", r, string(stack))
				_ = f.Close()
			}
		}

		if cb != nil {
			cb()
		}

		// If the file is empty, remove it.
		if size() == 0 {
			return os.Remove(path)
		}

		fmt.Fprintf(os.Stderr, "\n\npanic occurred, wrote dump to %s\n", path)
		os.Exit(1)
		return nil
	}
}
