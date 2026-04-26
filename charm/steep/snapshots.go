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
func (m *Model) RequireSnapshot(tb testing.TB, opts ...snapshot.Option) *Model {
	tb.Helper()
	snapshot.RequireEqual(tb, m.View(), opts...)
	return m
}

// RequireSnapshotNoANSI compares the latest captured program output against a
// snapshot after stripping ANSI sequences and without waiting for the program
// to finish.
func (m *Model) RequireSnapshotNoANSI(tb testing.TB, opts ...snapshot.Option) *Model {
	tb.Helper()
	m.RequireSnapshot(tb, append(opts, snapshot.WithStripANSI())...)
	return m
}
