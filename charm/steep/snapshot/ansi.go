// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"bytes"
	"os"
	"testing"

	"github.com/aymanbagabas/go-udiff"
	"github.com/lrstanley/x/charm/steep/internal/xansi"
)

// writeBytes handles the i/o and writing of a []byte to a file.
//
// It will fail the test if there are i/o errors.
func writeBytes(tb testing.TB, got []byte, path string, _ ...Option) {
	tb.Helper()
	if err := os.WriteFile(path, got, 0o600); err != nil {
		tb.Fatalf("failed to write snapshot: %v", err)
	}
}

// WriteBytes writes the given bytes to the given path, processing it in the same
// way as [AssertEqual] and [RequireEqual]. If path is empty, it will be generated
// using the test name and associated options.
//
// It will fail the test if there are i/o errors.
func WriteBytes(tb testing.TB, got []byte, path string, opts ...Option) {
	tb.Helper()
	if path == "" {
		cfg := collectOptions(tb, opts...)
		path = snapshotPath(tb, cfg.suffix, ".snap", cfg)
	}
	writeBytes(tb, got, path, opts...)
}

// RequireEqual compares "got" against this test's generated snapshot file.
func RequireEqual[T ~[]byte | ~string](tb testing.TB, got T, opts ...Option) {
	tb.Helper()

	if !AssertEqual(tb, got, opts...) {
		tb.FailNow()
	}
}

// AssertEqual compares "got" against this test's generated snapshot file,
// reporting errors without stopping the test immediately. It returns whether
// the snapshot matched.
func AssertEqual[T ~[]byte | ~string](tb testing.TB, got T, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)

	path := snapshotPath(tb, cfg.suffix, ".snap", cfg)
	actual := []byte(normalize(got, cfg))

	if cfg.update {
		writeBytes(tb, actual, path, opts...)
		return true
	}

	expected, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			errNotExist(tb, path)
			return false
		}
		tb.Errorf("failed to read snapshot %q: %v", path, err)
		return false
	}

	// Keep snapshots stable even when files are checked out or edited with CRLF.
	expected = xansi.NormalizeCRLF(expected)
	if bytes.Equal(expected, actual) {
		return true
	}

	diff := udiff.Unified(path, "actual", string(expected), string(actual))
	tb.Errorf("snapshot %q does not match:\n%s", path, diff)
	return false
}
