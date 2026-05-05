// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"github.com/lrstanley/x/charm/steep/snapshot"
)

// AssertViewSnapshot compares the latest captured view against a snapshot file
// without waiting for the [tea.Program] to finish. It allows the test to continue.
//
// See also [Harness.RequireViewSnapshot], [Harness.AssertViewSnapshotNoANSI],
// and [Harness.RequireViewSnapshotNoANSI].
func (h *Harness) AssertViewSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...)
	return h
}

// RequireViewSnapshot compares the latest captured view against a snapshot file
// without waiting for the [tea.Program] to finish, failing the test immediately if
// the snapshot does not match.
//
// See also [Harness.AssertViewSnapshot], [Harness.AssertViewSnapshotNoANSI],
// and [Harness.RequireViewSnapshotNoANSI].
func (h *Harness) RequireViewSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	if !snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...) {
		h.tb.FailNow()
	}
	return h
}

func (h *Harness) snapshotOpts(opts []snapshot.Option) []snapshot.Option {
	if !collectOptions(h.mergedOpts()...).stripANSI {
		return opts
	}
	return append([]snapshot.Option{snapshot.WithStripANSI()}, opts...)
}

// AssertViewSnapshotNoANSI compares the latest captured view against a snapshot
// file after stripping ANSI sequences and without waiting for the [tea.Program] to
// finish. It allows the test to continue.
//
// Note that you can also use [WithStripANSI] on the harness constructor to
// automatically strip ANSI sequences for all comparison-related methods. Example:
//
//	steep.NewHarness(t, model, steep.WithStripANSI())
//	h.WaitString("hello")
//	h.AssertViewSnapshot() // No need to call h.AssertViewSnapshotNoANSI.
//
// See also [Harness.AssertViewSnapshot], [Harness.RequireViewSnapshot],
// and [Harness.RequireViewSnapshotNoANSI].
func (h *Harness) AssertViewSnapshotNoANSI(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	return h.AssertViewSnapshot(append(opts, snapshot.WithStripANSI())...)
}

// RequireViewSnapshotNoANSI compares the latest captured view against a
// snapshot file after stripping ANSI sequences and without waiting for the
// [tea.Program] to finish, failing the test immediately if the snapshot does not
// match.
//
// Note that you can also use [WithStripANSI] on the harness constructor to
// automatically strip ANSI sequences for all comparison-related methods. Example:
//
//	steep.NewHarness(t, model, steep.WithStripANSI())
//	h.WaitString("hello")
//	h.RequireViewSnapshot() // No need to call h.RequireViewSnapshotNoANSI.
//
// See also [Harness.AssertViewSnapshot], [Harness.RequireViewSnapshot],
// and [Harness.AssertViewSnapshotNoANSI].
func (h *Harness) RequireViewSnapshotNoANSI(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	return h.RequireViewSnapshot(append(opts, snapshot.WithStripANSI())...)
}
