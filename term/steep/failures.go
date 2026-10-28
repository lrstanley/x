// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

// failureTB records the failure messages the harness reports through it, for
// [Result.Failures], so an outro card can show why a test failed, and passes
// each to note, when set, with its number, counted from 1, before reporting
// it. It calls Helper before each
// report, so failures are still attributed to the caller's line.
type failureTB struct {
	testing.TB

	// note is set once, before the program starts. It is called with mu
	// held, so failures are noted in the order of msgs, and must not report
	// through the failureTB.
	note func(n int, msg string)

	mu   sync.Mutex
	msgs []string
}

func (f *failureTB) Error(args ...any) {
	f.TB.Helper()
	f.record(sprintln(args...))
	f.TB.Error(args...)
}

func (f *failureTB) Errorf(format string, args ...any) {
	f.TB.Helper()
	f.record(fmt.Sprintf(format, args...))
	f.TB.Errorf(format, args...)
}

func (f *failureTB) Fatal(args ...any) {
	f.TB.Helper()
	f.record(sprintln(args...))
	f.TB.Fatal(args...)
}

func (f *failureTB) Fatalf(format string, args ...any) {
	f.TB.Helper()
	f.record(fmt.Sprintf(format, args...))
	f.TB.Fatalf(format, args...)
}

// sprintln formats args like [testing.TB.Error] does.
func sprintln(args ...any) string {
	return strings.TrimSuffix(fmt.Sprintln(args...), "\n")
}

func (f *failureTB) record(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	if f.note != nil {
		f.note(len(f.msgs), msg)
	}
}

func (f *failureTB) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.msgs)
}
