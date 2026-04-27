// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/charm/steep/internal/xansi"
)

// WaitView waits until condition returns true for the latest view output.
func WaitView[T ~string | ~[]byte](tb testing.TB, model View, condition func(view T) bool, opts ...Option) T {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)
	ctx := tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		raw := model.View()
		if cfg.stripANSI {
			raw = xansi.StripANSI(raw)
		}
		out := T(raw)
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

// WaitBytes waits until output contains contents.
func WaitBytes(tb testing.TB, model View, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitView(tb, model, func(bts []byte) bool {
		return bytes.Contains(bts, contents)
	}, opts...)
}

// WaitString waits until output contains contents.
func WaitString(tb testing.TB, model View, contents string, opts ...Option) string {
	tb.Helper()
	return WaitView(tb, model, func(str string) bool {
		return strings.Contains(str, contents)
	}, opts...)
}

// WaitStrings waits until output contains all contents.
func WaitStrings(tb testing.TB, model View, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitView(tb, model, func(str string) bool {
		for _, content := range contents {
			if !strings.Contains(str, content) {
				return false
			}
		}
		return true
	}, opts...)
}

// WaitNotBytes waits until output contains none of the contents.
func WaitNotBytes(tb testing.TB, model View, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitView(tb, model, func(bts []byte) bool {
		return !bytes.Contains(bts, contents)
	}, opts...)
}

// WaitNotString waits until output contains none of the contents.
func WaitNotString(tb testing.TB, model View, contents string, opts ...Option) string {
	tb.Helper()
	return WaitView(tb, model, func(str string) bool {
		return !strings.Contains(str, contents)
	}, opts...)
}

// WaitNotStrings waits until output contains none of the contents.
func WaitNotStrings(tb testing.TB, model View, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitView(tb, model, func(str string) bool {
		for _, content := range contents {
			if strings.Contains(str, content) {
				return false
			}
		}
		return true
	}, opts...)
}

// WaitMatch waits until the latest view output matches the regular expression
// pattern. pattern is compiled with [regexp.Compile]; a compile error fails the
// test immediately.
func WaitMatch(tb testing.TB, model View, pattern string, opts ...Option) string {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	return WaitView(tb, model, func(str string) bool {
		return re.MatchString(str)
	}, opts...)
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern. pattern is compiled with [regexp.Compile]; a compile
// error fails the test immediately.
func WaitNotMatch(tb testing.TB, model View, pattern string, opts ...Option) string {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	return WaitView(tb, model, func(str string) bool {
		return !re.MatchString(str)
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
	if cfg.stripANSI {
		prev = xansi.StripANSI(prev)
	}
	lastChange := time.Now()

	for {
		v := model.View()
		if cfg.stripANSI {
			v = xansi.StripANSI(v)
		}
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

// AssertString reports an error unless content appears in output. It
// returns whether the output matched and allows the test to continue.
func AssertString(tb testing.TB, model View, content string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if !strings.Contains(out, content) {
		tb.Errorf("expected output to contain %q\noutput:\n%s", content, out)
		return false
	}
	return true
}

// RequireString fails the test immediately unless content appears in output.
func RequireString(tb testing.TB, model View, content string, opts ...Option) {
	tb.Helper()

	if !AssertString(tb, model, content, opts...) {
		tb.FailNow()
	}
}

// AssertStrings reports an error unless every substring in contents
// appears in output. It returns whether the output matched and allows the test
// to continue.
func AssertStrings(tb testing.TB, model View, contents []string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	matched := true
	for _, sub := range contents {
		if !strings.Contains(out, sub) {
			tb.Errorf("expected output to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	return matched
}

// RequireStrings fails the test immediately unless every substring in
// contents appears in output.
func RequireStrings(tb testing.TB, model View, contents []string, opts ...Option) {
	tb.Helper()

	if !AssertStrings(tb, model, contents, opts...) {
		tb.FailNow()
	}
}

// AssertNotString reports an error if content appears in output. It
// returns whether the output matched and allows the test to continue.
func AssertNotString(tb testing.TB, model View, content string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if strings.Contains(out, content) {
		tb.Errorf("expected output not to contain %q\noutput:\n%s", content, out)
		return false
	}
	return true
}

// RequireNotString fails the test immediately if content appears in output.
func RequireNotString(tb testing.TB, model View, content string, opts ...Option) {
	tb.Helper()

	if !AssertNotString(tb, model, content, opts...) {
		tb.FailNow()
	}
}

// AssertNotStrings reports an error if any substring in contents
// appears in output. It returns whether the output matched and allows the test
// to continue.
func AssertNotStrings(tb testing.TB, model View, contents []string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	matched := true
	for _, sub := range contents {
		if strings.Contains(out, sub) {
			tb.Errorf("expected output not to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	return matched
}

// RequireNotStrings fails the test immediately if any substring in
// contents appears in output.
func RequireNotStrings(tb testing.TB, model View, contents []string, opts ...Option) {
	tb.Helper()

	if !AssertNotStrings(tb, model, contents, opts...) {
		tb.FailNow()
	}
}

// AssertMatch reports an error unless output matches the regular expression
// pattern. pattern is compiled with [regexp.Compile]; a compile error fails
// the test immediately.
// It returns whether the output matched and allows the test to continue.
func AssertMatch(tb testing.TB, model View, pattern string, opts ...Option) bool {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if !re.MatchString(out) {
		tb.Errorf("expected output to match %q\noutput:\n%s", pattern, out)
		return false
	}
	return true
}

// RequireMatch fails the test immediately unless output matches the regular
// expression pattern.
func RequireMatch(tb testing.TB, model View, pattern string, opts ...Option) {
	tb.Helper()

	if !AssertMatch(tb, model, pattern, opts...) {
		tb.FailNow()
	}
}

// AssertNotMatch reports an error if output matches the regular expression
// pattern. pattern is compiled with [regexp.Compile]; a compile error fails
// the test immediately.
// It returns whether the output matched and allows the test to continue.
func AssertNotMatch(tb testing.TB, model View, pattern string, opts ...Option) bool {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if re.MatchString(out) {
		tb.Errorf("expected output not to match %q\noutput:\n%s", pattern, out)
		return false
	}
	return true
}

// RequireNotMatch fails the test immediately if output matches the regular
// expression pattern.
func RequireNotMatch(tb testing.TB, model View, pattern string, opts ...Option) {
	tb.Helper()

	if !AssertNotMatch(tb, model, pattern, opts...) {
		tb.FailNow()
	}
}

// AssertHeight reports an error unless output has n rows. Note that this behaves
// differently to [charm.land/lipgloss/v2.Height] which always assumes a minimum
// height of 1.
// It returns whether the output matched and allows the test to continue.
func AssertHeight(tb testing.TB, model View, n int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	_, goth := Dimensions(out)
	if goth != n {
		tb.Errorf("expected output height %d, got %d", n, goth)
		return false
	}
	return true
}

// RequireHeight fails the test immediately unless output has n rows.
func RequireHeight(tb testing.TB, model View, n int, opts ...Option) {
	tb.Helper()

	if !AssertHeight(tb, model, n, opts...) {
		tb.FailNow()
	}
}

// AssertWidth reports an error unless output has n columns. It returns whether
// the output matched and allows the test to continue.
func AssertWidth(tb testing.TB, model View, n int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	gotw, _ := Dimensions(out)
	if gotw != n {
		tb.Errorf("expected output width %d, got %d", n, gotw)
		return false
	}
	return true
}

// RequireWidth fails the test immediately unless output has n columns.
func RequireWidth(tb testing.TB, model View, n int, opts ...Option) {
	tb.Helper()

	if !AssertWidth(tb, model, n, opts...) {
		tb.FailNow()
	}
}

// AssertDimensions reports an error unless output has specified dimensions. Note
// that this behaves differently to [charm.land/lipgloss/v2.Size] which always
// assumes a minimum height of 1.
// It returns whether the output matched and allows the test to continue.
func AssertDimensions(tb testing.TB, model View, width, height int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := model.View()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	gotw, goth := Dimensions(out)
	if gotw != width || goth != height {
		tb.Errorf("expected output dimensions %dx%d, got %dx%d", width, height, gotw, goth)
		return false
	}
	return true
}

// RequireDimensions fails the test immediately unless output has specified dimensions.
func RequireDimensions(tb testing.TB, model View, width, height int, opts ...Option) {
	tb.Helper()

	if !AssertDimensions(tb, model, width, height, opts...) {
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
