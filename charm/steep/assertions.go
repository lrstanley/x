// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type outputter interface {
	outputBytes() []byte
}

// WaitFor waits until condition returns true for the latest view output.
func WaitFor(tb testing.TB, model outputter, condition func(bts []byte) bool, opts ...Option) []byte {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)

	for {
		out := model.outputBytes()
		if condition(out) {
			return out
		}
		if time.Now().After(deadline) {
			tb.Fatalf("timeout waiting for condition\nlast output:\n%s", string(out))
		}
		time.Sleep(cfg.checkInterval)
	}
}

// WaitContains waits until output contains all substrings.
func WaitContains(tb testing.TB, model outputter, substr ...[]byte) []byte {
	tb.Helper()
	return WaitFor(tb, model, func(bts []byte) bool {
		for _, sub := range substr {
			if !bytes.Contains(bts, sub) {
				return false
			}
		}
		return true
	})
}

// WaitContainsString waits until output contains all substrings.
func WaitContainsString(tb testing.TB, model outputter, substr ...string) []byte {
	tb.Helper()
	return WaitContains(tb, model, stringsToBytes(substr)...)
}

// WaitNotContains waits until output contains none of the substrings.
func WaitNotContains(tb testing.TB, model outputter, substr ...[]byte) []byte {
	tb.Helper()
	return WaitFor(tb, model, func(bts []byte) bool {
		for _, sub := range substr {
			if bytes.Contains(bts, sub) {
				return false
			}
		}
		return true
	})
}

// WaitNotContainsString waits until output contains none of the substrings.
func WaitNotContainsString(tb testing.TB, model outputter, substr ...string) []byte {
	tb.Helper()
	return WaitNotContains(tb, model, stringsToBytes(substr)...)
}

func expectStringContains(tb testing.TB, model outputter, substr ...string) {
	tb.Helper()

	out := model.outputBytes()
	for _, sub := range substr {
		if !bytes.Contains(out, []byte(sub)) {
			tb.Fatalf("expected output to contain %q\noutput:\n%s", sub, string(out))
		}
	}
}

func expectStringNotContains(tb testing.TB, model outputter, substr ...string) {
	tb.Helper()

	out := model.outputBytes()
	for _, sub := range substr {
		if bytes.Contains(out, []byte(sub)) {
			tb.Fatalf("expected output not to contain %q\noutput:\n%s", sub, string(out))
		}
	}
}

func stringsToBytes(values []string) [][]byte {
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		out = append(out, []byte(value))
	}
	return out
}

func expectHeight(tb testing.TB, model outputter, height int) {
	tb.Helper()

	got := dimensions(string(model.outputBytes()))
	if got.height != height {
		tb.Fatalf("expected output height %d, got %d", height, got.height)
	}
}

func expectWidth(tb testing.TB, model outputter, width int) {
	tb.Helper()

	got := dimensions(string(model.outputBytes()))
	if got.width != width {
		tb.Fatalf("expected output width %d, got %d", width, got.width)
	}
}

func expectDimensions(tb testing.TB, model outputter, width, height int) {
	tb.Helper()

	got := dimensions(string(model.outputBytes()))
	if got.width != width || got.height != height {
		tb.Fatalf("expected output dimensions %dx%d, got %dx%d", width, height, got.width, got.height)
	}
}

type outputDimensions struct {
	width  int
	height int
}

func dimensions(out string) outputDimensions {
	if out == "" {
		return outputDimensions{}
	}

	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	width := 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	return outputDimensions{
		width:  width,
		height: len(lines),
	}
}
