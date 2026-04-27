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

// WaitFor waits until condition returns true for the latest view output.
func WaitFor[T ~string | ~[]byte](tb testing.TB, model View, condition func(view T) bool, opts ...Option) T {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)
	ctx := tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		out := T(model.View())
		if condition(out) {
			return out
		}
		remainingTimeout := time.Until(deadline)
		if remainingTimeout <= 0 {
			tb.Fatalf("timeout waiting for condition\nlast output:\n%s", out)
		}

		timer.Reset(min(cfg.checkInterval, remainingTimeout))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			tb.Fatalf("wait for condition canceled: %v", ctx.Err())
		}
	}
}

// WaitContainsBytes waits until output contains contents.
func WaitContainsBytes(tb testing.TB, model View, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitFor(tb, model, func(bts []byte) bool {
		return bytes.Contains(bts, contents)
	}, opts...)
}

// WaitContainsString waits until output contains contents.
func WaitContainsString(tb testing.TB, model View, contents string, opts ...Option) string {
	tb.Helper()
	return WaitFor(tb, model, func(str string) bool {
		return strings.Contains(str, contents)
	}, opts...)
}

// WaitContainsStrings waits until output contains all contents.
func WaitContainsStrings(tb testing.TB, model View, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitFor(tb, model, func(str string) bool {
		for _, content := range contents {
			if !strings.Contains(str, content) {
				return false
			}
		}
		return true
	}, opts...)
}

// WaitNotContainsBytes waits until output contains none of the contents.
func WaitNotContainsBytes(tb testing.TB, model View, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitFor(tb, model, func(bts []byte) bool {
		return !bytes.Contains(bts, contents)
	}, opts...)
}

// WaitNotContainsString waits until output contains none of the contents.
func WaitNotContainsString(tb testing.TB, model View, contents string, opts ...Option) string {
	tb.Helper()
	return WaitFor(tb, model, func(str string) bool {
		return !strings.Contains(str, contents)
	}, opts...)
}

// WaitNotContainsStrings waits until output contains none of the contents.
func WaitNotContainsStrings(tb testing.TB, model View, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitFor(tb, model, func(str string) bool {
		for _, content := range contents {
			if strings.Contains(str, content) {
				return false
			}
		}
		return true
	}, opts...)
}

// WaitSettleView waits until the rendered view string has not changed for the
// configured settle timeout. The model must implement [View]; each check calls
// View() and compares the string to the previous sample. See also
// [WithSettleTimeout], [WithCheckInterval], and [WithTimeout].
func WaitSettleView(tb testing.TB, model View, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)
	ctx := tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	prev := model.View()
	lastChange := time.Now()

	for {
		v := model.View()
		now := time.Now()
		if v != prev {
			prev = v
			lastChange = now
		}
		quietFor := now.Sub(lastChange)

		if quietFor >= cfg.settleTimeout {
			return
		}

		remainingTimeout := deadline.Sub(now)
		if remainingTimeout <= 0 {
			tb.Fatalf(
				"timeout waiting for View() to settle after %s; last observed view change was %s ago",
				cfg.timeout,
				quietFor,
			)
			return
		}

		timer.Reset(min(cfg.checkInterval, cfg.settleTimeout-quietFor, remainingTimeout))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			tb.Fatalf("wait for View() to settle canceled: %v", ctx.Err())
		}
	}
}

// AssertStringContains reports an error unless all substrings appear in output.
// It returns whether the output matched and allows the test to continue.
func AssertStringContains(tb testing.TB, model View, contents ...string) bool {
	tb.Helper()

	out := model.View()
	matched := true
	for _, sub := range contents {
		if !strings.Contains(out, sub) {
			tb.Errorf("expected output to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	return matched
}

// RequireStringContains fails the test immediately unless all substrings appear
// in output.
func RequireStringContains(tb testing.TB, model View, contents ...string) {
	tb.Helper()

	if !AssertStringContains(tb, model, contents...) {
		tb.FailNow()
	}
}

// AssertStringNotContains reports an error if any substring appears in output.
// It returns whether the output matched and allows the test to continue.
func AssertStringNotContains(tb testing.TB, model View, contents ...string) bool {
	tb.Helper()

	out := model.View()
	matched := true
	for _, sub := range contents {
		if strings.Contains(out, sub) {
			tb.Errorf("expected output not to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	return matched
}

// RequireStringNotContains fails the test immediately if any substring appears
// in output.
func RequireStringNotContains(tb testing.TB, model View, contents ...string) {
	tb.Helper()

	if !AssertStringNotContains(tb, model, contents...) {
		tb.FailNow()
	}
}

// AssertHeight reports an error unless output has n rows. Note that this behaves
// differently to [charm.land/lipgloss/v2.Height] which always assumes a minimum
// height of 1.
// It returns whether the output matched and allows the test to continue.
func AssertHeight(tb testing.TB, model View, n int) bool {
	tb.Helper()

	_, goth := Dimensions(model.View())
	if goth != n {
		tb.Errorf("expected output height %d, got %d", n, goth)
		return false
	}
	return true
}

// RequireHeight fails the test immediately unless output has n rows.
func RequireHeight(tb testing.TB, model View, n int) {
	tb.Helper()

	if !AssertHeight(tb, model, n) {
		tb.FailNow()
	}
}

// AssertWidth reports an error unless output has n columns. It returns whether
// the output matched and allows the test to continue.
func AssertWidth(tb testing.TB, model View, n int) bool {
	tb.Helper()

	gotw, _ := Dimensions(model.View())
	if gotw != n {
		tb.Errorf("expected output width %d, got %d", n, gotw)
		return false
	}
	return true
}

// RequireWidth fails the test immediately unless output has n columns.
func RequireWidth(tb testing.TB, model View, n int) {
	tb.Helper()

	if !AssertWidth(tb, model, n) {
		tb.FailNow()
	}
}

// AssertDimensions reports an error unless output has specified dimensions. Note
// that this behaves differently to [charm.land/lipgloss/v2.Size] which always
// assumes a minimum height of 1.
// It returns whether the output matched and allows the test to continue.
func AssertDimensions(tb testing.TB, model View, width, height int) bool {
	tb.Helper()

	gotw, goth := Dimensions(model.View())
	if gotw != width || goth != height {
		tb.Errorf("expected output dimensions %dx%d, got %dx%d", width, height, gotw, goth)
		return false
	}
	return true
}

// RequireDimensions fails the test immediately unless output has specified dimensions.
func RequireDimensions(tb testing.TB, model View, width, height int) {
	tb.Helper()

	if !AssertDimensions(tb, model, width, height) {
		tb.FailNow()
	}
}

// Dimensions returns the width and height of the output.
func Dimensions(out string) (w, h int) {
	if out == "" {
		return 0, 0
	}

	var width int
	var height int
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		width = max(width, ansi.StringWidth(line))
		height++
	}
	return width, height
}
