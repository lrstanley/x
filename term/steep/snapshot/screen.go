// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// compareScreens reports whether a and b are equal once encoded as JSON.
func compareScreens(tb testing.TB, a, b *reel.Screen) bool {
	tb.Helper()
	ad, aerr := json.Marshal(a)
	bd, berr := json.Marshal(b)
	if err := errors.Join(aerr, berr); err != nil {
		tb.Fatalf("failed to marshal snapshot: %v", err)
	}
	return bytes.Equal(ad, bd)
}

// AsScreenBuffer converts plain text into a cell grid. Each rune is one cell.
// The trailing newline does not add an empty row.
func AsScreenBuffer(tb testing.TB, v string, opts ...Option) *vt.Buffer {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	text := normalize(v, cfg)
	if text == "" {
		return vt.NewBuffer(0, 0)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	width := 0
	for _, line := range lines {
		if n := utf8.RuneCountInString(line); n > width {
			width = n
		}
	}
	buf := vt.NewBuffer(width, len(lines))
	for y, line := range lines {
		x := 0
		for _, r := range line {
			buf.SetCell(x, y, &vt.Cell{Content: string(r), Width: 1})
			x++
		}
	}
	return buf
}

// writeScreen handles the i/o and encoding of a [reel.Screen] to a JSON file.
//
// It will fail the test if there are i/o errors.
func writeScreen(tb testing.TB, snap *reel.Screen, path string, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	buf := &bytes.Buffer{}
	var encOpts []jsontext.Options
	if cfg.indent {
		encOpts = append(encOpts, jsontext.Multiline(true))
	}
	enc := jsontext.NewEncoder(buf, encOpts...)
	err := json.MarshalEncode(enc, snap)
	if err != nil {
		tb.Fatalf("failed to encode snapshot: %v", err)
	}
	err = os.WriteFile(path, buf.Bytes(), 0o600)
	if err != nil {
		tb.Fatalf("failed to write snapshot: %v", err)
	}
}

// WriteScreen writes the given screen snapshot to the given path, processing it
// in the same way as [AssertScreenEqual] and [RequireScreenEqual]. If path is
// empty, it will be generated using the test name and associated options.
//
// It will fail the test if there are i/o errors.
func WriteScreen(tb testing.TB, screen *reel.Screen, path string, opts ...Option) {
	tb.Helper()
	if path == "" {
		cfg := collectOptions(tb, opts...)
		path = snapshotPath(tb, cfg.suffix, ".json", cfg)
	}
	writeScreen(tb, screen, path, opts...)
}

// AssertScreenEqual compares screen, such as from [reel.Session.Screen],
// against the stored JSON snapshot of it.
func AssertScreenEqual(tb testing.TB, screen *reel.Screen, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	path := snapshotPath(tb, cfg.suffix, ".json", cfg)

	if cfg.update {
		cfg.report(Result{Path: path, Updated: true})
		writeScreen(tb, screen, path, opts...)
		return true
	}

	cfg.report(Result{Path: path})
	expectedb, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.report(Result{Path: path, Missing: true})
			errNotExist(tb, path)
			return false
		}
		tb.Fatalf("failed to read snapshot: %v", err)
	}
	expected := &reel.Screen{}
	err = json.Unmarshal(expectedb, expected)
	if err != nil {
		tb.Fatalf("failed to unmarshal snapshot: %v", err)
	}
	matches := compareScreens(tb, screen, expected)
	if !matches {
		tb.Errorf("snapshot %q does not match", path)
		return false
	}
	return true
}

// RequireScreenEqual is [AssertScreenEqual], failing the test immediately if
// the snapshot does not match.
func RequireScreenEqual(tb testing.TB, screen *reel.Screen, opts ...Option) {
	tb.Helper()

	if !AssertScreenEqual(tb, screen, opts...) {
		tb.FailNow()
	}
}
