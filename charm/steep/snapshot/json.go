// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"testing"
)

// WriteJSON writes the JSON representation of the provided string to a file,
// processing it in the same way as [AssertJSONEqual] and [RequireJSONEqual]. If
// path is empty, it will be generated using the test name and associated options.
//
// It will fail the test if there are i/o errors.
func WriteJSON(tb testing.TB, v, path string, opts ...Option) {
	tb.Helper()
	snap := &ScreenSnapshot{}
	snap.WithScreenBuffer(tb, AsScreenBuffer(tb, v, opts...), opts...)
	if path == "" {
		cfg := collectOptions(tb, opts...)
		path = snapshotPath(tb, cfg.suffix, ".snap", cfg)
	}
	writeScreen(tb, snap, path, opts...)
}

// AssertJSONEqual compares the provided string against the JSON representation of
// a [ScreenSnapshot], reporting errors without stopping the test immediately (in
// most cases).
func AssertJSONEqual(tb testing.TB, v string, opts ...Option) bool {
	tb.Helper()

	snap := &ScreenSnapshot{}
	snap.WithScreenBuffer(tb, AsScreenBuffer(tb, v, opts...), opts...)

	return AssertScreenEqual(tb, snap, opts...)
}

// RequireJSONEqual compares the provided string against the JSON representation of
// a [ScreenSnapshot], failing the test immediately if the snapshot does not match.
func RequireJSONEqual(tb testing.TB, v string, opts ...Option) {
	tb.Helper()

	if !AssertJSONEqual(tb, v, opts...) {
		tb.FailNow()
	}
}
