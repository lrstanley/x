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

// failureTB records the failure messages the harness reports through it, so
// an outro card can show why a test failed. It calls Helper before each
// report, so failures are still attributed to the caller's line.
type failureTB struct {
	testing.TB

	mu   sync.Mutex
	msgs []string
}

func (f *failureTB) Error(args ...any) {
	f.TB.Helper()
	f.record(strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
	f.TB.Error(args...)
}

func (f *failureTB) Errorf(format string, args ...any) {
	f.TB.Helper()
	f.record(fmt.Sprintf(format, args...))
	f.TB.Errorf(format, args...)
}

func (f *failureTB) Fatal(args ...any) {
	f.TB.Helper()
	f.record(strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
	f.TB.Fatal(args...)
}

func (f *failureTB) Fatalf(format string, args ...any) {
	f.TB.Helper()
	f.record(fmt.Sprintf(format, args...))
	f.TB.Fatalf(format, args...)
}

func (f *failureTB) record(msg string) {
	f.mu.Lock()
	f.msgs = append(f.msgs, msg)
	f.mu.Unlock()
}

func (f *failureTB) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.msgs)
}
