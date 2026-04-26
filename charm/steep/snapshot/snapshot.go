// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/aymanbagabas/go-udiff"
)

const (
	envUpdateSnapshots = "UPDATE_SNAPSHOTS"
	snapshotExtension  = ".snap"
)

var snapshotCounters sync.Map

// RequireEqual compares "got" against this test's generated snapshot file.
func RequireEqual[T ~[]byte | ~string](tb testing.TB, got T, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(tb, opts...)

	testName := tb.Name()
	tb.Cleanup(func() {
		snapshotCounters.Delete(testName)
	})

	path := snapshotPath(testName, cfg.suffix)
	actual := normalize(got, cfg)

	if cfg.update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			tb.Fatalf("failed to create snapshot directory: %v", err)
		}
		if err := os.WriteFile(path, actual, 0o600); err != nil {
			tb.Fatalf("failed to write snapshot %q: %v", path, err)
		}
	}

	expected, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			tb.Fatalf("snapshot %q does not exist; set %s=true to create it", path, envUpdateSnapshots)
		}
		tb.Fatalf("failed to read snapshot %q: %v", path, err)
	}

	// Keep snapshots stable even when files are checked out or edited with CRLF.
	expected = normalizeCRLF(expected)
	if bytes.Equal(expected, actual) {
		return
	}

	diff := udiff.Unified(path, "actual", string(expected), string(actual))
	tb.Fatalf("snapshot %q does not match:\n%s", path, diff)
}

// normalize converts supported values to a stable byte representation.
func normalize[T ~[]byte | ~string](got T, cfg options) []byte {
	value := reflect.ValueOf(got)
	var bts []byte
	if value.Kind() == reflect.String {
		bts = []byte(value.String())
	} else if value.Kind() == reflect.Slice {
		bts = bytes.Clone(value.Bytes())
	} else {
		bts = fmt.Append(nil, got)
	}

	for _, transform := range cfg.transforms {
		bts = transform(bts)
	}

	return bts
}

// snapshotPath builds the testdata path for a test name and optional suffix.
func snapshotPath(testName, suffix string) string {
	count := nextCount(testName)
	segments := strings.Split(testName, "/")
	for i, segment := range segments {
		segments[i] = sanitizeFilename(segment)
	}

	base := segments[len(segments)-1]
	if count > 1 {
		base = fmt.Sprintf("%s.%02d", base, count)
	}
	if suffix != "" {
		base = base + "-" + sanitizeFilename(suffix)
	}
	segments[len(segments)-1] = base + snapshotExtension

	return filepath.Join(append([]string{"testdata"}, segments...)...)
}

// nextCount tracks repeated snapshot assertions within the same test.
func nextCount(testName string) uint64 {
	counter, _ := snapshotCounters.LoadOrStore(testName, new(atomic.Uint64))
	typedCounter, ok := counter.(*atomic.Uint64)
	if !ok {
		return 1
	}
	return typedCounter.Add(1)
}

// sanitizeFilename converts test and suffix names into filesystem-safe path segments.
func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "snapshot"
	}

	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '.' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "snapshot"
	}
	return out
}
