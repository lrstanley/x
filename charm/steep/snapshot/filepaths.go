// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode"
)

// snapshotPath builds the testdata path for a test name and optional suffix,
// and creates the directory if it doesn't exist.
func snapshotPath(tb testing.TB, suffix, ext string, cfg options) string {
	tb.Helper()

	count := nextCount(tb, ext)
	segments := strings.Split(tb.Name(), "/")
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
	segments[len(segments)-1] = base + ext

	fn := filepath.Join(append([]string{cfg.dir}, segments...)...)

	if cfg.update {
		err := os.MkdirAll(filepath.Dir(fn), 0o750)
		if err != nil {
			tb.Fatalf("failed to create snapshot directory: %v", err)
		}
	}

	return fn
}

var snapshotCounters sync.Map

// nextCount tracks repeated snapshot assertions within the same test.
func nextCount(tb testing.TB, ext string) uint64 {
	tb.Helper()
	key := tb.Name() + ":" + ext
	counter, _ := snapshotCounters.LoadOrStore(key, new(atomic.Uint64))
	typedCounter, ok := counter.(*atomic.Uint64)
	if !ok {
		return 1
	}
	tb.Cleanup(func() { snapshotCounters.Delete(key) })
	return typedCounter.Add(1)
}

// sanitizeFilename converts test and suffix names into filesystem-safe path
// segments.
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
