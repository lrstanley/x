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
	actual := normalize(got, cfg)

	if cfg.update {
		if err := os.WriteFile(path, actual, 0o600); err != nil {
			tb.Errorf("failed to write snapshot %q: %v", path, err)
			return false
		}
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
