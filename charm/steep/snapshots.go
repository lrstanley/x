// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	"github.com/lrstanley/x/charm/steep/snapshot"
)

// RequireSnapshot compares the latest captured program output against a
// snapshot without waiting for the program to finish.
func (m *Model) RequireSnapshot(tb testing.TB, opts ...snapshot.Option) {
	tb.Helper()

	snapshot.RequireEqual(tb, m.outputBytes(), opts...)
}

// RequirePlainSnapshot compares the latest captured program output against a
// snapshot after stripping ANSI sequences and without waiting for the program
// to finish.
func (m *Model) RequirePlainSnapshot(tb testing.TB, opts ...snapshot.Option) {
	tb.Helper()

	m.RequireSnapshot(tb, appendPlainSnapshotOptions(opts)...)
}

func appendPlainSnapshotOptions(opts []snapshot.Option) []snapshot.Option {
	out := append([]snapshot.Option{}, opts...)
	out = append(out, snapshot.WithStripANSI())
	return out
}
