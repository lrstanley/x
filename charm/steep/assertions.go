// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"context"
	"iter"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/charm/steep/internal/xansi"
)

type Viewable func() string

// WaitViewFunc waits until condition returns true for the latest view output.
//
// See also [Harness.WaitBytesFunc], [Harness.WaitStringFunc], [WaitBytes],
// [WaitMatch], and [WaitStrings].
func WaitViewFunc[T ~string | ~[]byte](
	tb testing.TB,
	view Viewable,
	condition func(view T) bool,
	opts ...Option,
) T {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)
	ctx := tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		raw := view()
		if cfg.stripANSI {
			raw = xansi.StripANSI(raw)
		}
		out := T(raw)
		if condition(out) {
			return out
		}
		remainingTimeout := time.Until(deadline)
		if remainingTimeout <= 0 {
			cfg.Fatalf(tb, "timeout waiting for condition\nlast output:\n%s", out)
		}

		timer.Reset(min(cfg.checkInterval, remainingTimeout))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			cfg.Fatalf(tb, "wait for condition canceled: %v", ctx.Err())
		}
	}
}

// WaitBytes waits until output contains contents.
//
// See also [Harness.WaitBytes], [WaitViewFunc], [WaitStrings], and [WaitNotBytes].
func WaitBytes(tb testing.TB, model Viewable, contents []byte, opts ...Option) []byte {
	tb.Helper()
	opts = append(opts, withReason("WaitBytes(%q)", contents))
	return WaitViewFunc(tb, model, func(bts []byte) bool {
		return bytes.Contains(bts, contents)
	}, opts...)
}

// WaitString waits until output contains contents.
//
// See also [Harness.WaitString], [WaitViewFunc], [WaitStrings], and [WaitNotString].
func WaitString(tb testing.TB, model Viewable, contents string, opts ...Option) string {
	tb.Helper()
	opts = append(opts, withReason("WaitString(%q)", contents))
	return WaitViewFunc(tb, model, func(str string) bool {
		return strings.Contains(str, contents)
	}, opts...)
}

// WaitStrings waits until output contains all contents.
//
// See also [Harness.WaitStrings], [WaitString], and [WaitNotStrings].
func WaitStrings(tb testing.TB, model Viewable, contents []string, opts ...Option) string {
	tb.Helper()
	opts = append(opts, withReason("WaitStrings(%q)", contents))
	return WaitViewFunc(tb, model, func(str string) bool {
		for _, content := range contents {
			if !strings.Contains(str, content) {
				return false
			}
		}
		return true
	}, opts...)
}

// WaitNotBytes waits until output contains none of the contents.
//
// See also [Harness.WaitNotBytes], [WaitBytes], and [WaitStrings].
func WaitNotBytes(tb testing.TB, model Viewable, contents []byte, opts ...Option) []byte {
	tb.Helper()
	opts = append(opts, withReason("WaitNotBytes(%q)", contents))
	return WaitViewFunc(tb, model, func(bts []byte) bool {
		return !bytes.Contains(bts, contents)
	}, opts...)
}

// WaitNotString waits until output contains none of the contents.
//
// See also [Harness.WaitNotString], [WaitString], and [WaitNotStrings].
func WaitNotString(tb testing.TB, model Viewable, contents string, opts ...Option) string {
	tb.Helper()
	opts = append(opts, withReason("WaitNotString(%q)", contents))
	return WaitViewFunc(tb, model, func(str string) bool {
		return !strings.Contains(str, contents)
	}, opts...)
}

// WaitNotStrings waits until output contains none of the contents.
//
// See also [Harness.WaitNotStrings], [WaitStrings], and [WaitNotString].
func WaitNotStrings(tb testing.TB, model Viewable, contents []string, opts ...Option) string {
	tb.Helper()
	opts = append(opts, withReason("WaitNotStrings(%q)", contents))
	return WaitViewFunc(tb, model, func(str string) bool {
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
//
// See also [Harness.WaitMatch], [AssertMatch], [WaitNotMatch], and [RequireMatch].
func WaitMatch(tb testing.TB, model Viewable, pattern string, opts ...Option) string {
	tb.Helper()
	opts = append(opts, withReason("WaitMatch(%q)", pattern))
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	return WaitViewFunc(tb, model, func(str string) bool {
		return re.MatchString(str)
	}, opts...)
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern. pattern is compiled with [regexp.Compile]; a compile
// error fails the test immediately.
//
// See also [Harness.WaitNotMatch], [WaitMatch], [AssertNotMatch], and
// [RequireNotMatch].
func WaitNotMatch(tb testing.TB, model Viewable, pattern string, opts ...Option) string {
	tb.Helper()
	opts = append(opts, withReason("WaitNotMatch(%q)", pattern))
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	return WaitViewFunc(tb, model, func(str string) bool {
		return !re.MatchString(str)
	}, opts...)
}

// WaitSettle waits until the rendered view string has not changed for the
// configured settle timeout. The model must implement [Viewable]; each check calls
// View() and compares the string to the previous sample.
//
// See also [Harness.WaitSettle], [Harness.WaitSettleMessages],
// [WithSettleTimeout], [WithCheckInterval], and [WithTimeout].
func WaitSettle(tb testing.TB, view Viewable, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(opts...)
	deadline := time.Now().Add(cfg.timeout)
	ctx := tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	prev := view()
	if cfg.stripANSI {
		prev = xansi.StripANSI(prev)
	}
	lastChange := time.Now()

	for {
		v := view()
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
				"timeout waiting for view to settle after %s; last observed view change was %s ago",
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
			tb.Fatalf("wait for view to settle canceled: %v", ctx.Err())
		}
	}
}

// AssertString reports an error unless content appears in output. It
// returns whether the output matched and allows the test to continue.
//
// See also [Harness.AssertString], [AssertStrings], [AssertNotString], and
// [WaitString].
func AssertString(tb testing.TB, view Viewable, content string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if !strings.Contains(out, content) {
		cfg.Errorf(tb, "expected output to contain %q\noutput:\n%s", content, out)
		return false
	}
	return true
}

// RequireString fails the test immediately unless content appears in output.
//
// See also [Harness.RequireString], [AssertString], and [RequireStrings].
func RequireString(tb testing.TB, view Viewable, content string, opts ...Option) {
	tb.Helper()

	if !AssertString(tb, view, content, opts...) {
		tb.FailNow()
	}
}

// AssertStrings reports an error unless every substring in contents
// appears in output. It returns whether the output matched and allows the test
// to continue.
//
// See also [Harness.AssertStrings], [AssertString], [RequireStrings], and
// [WaitStrings].
func AssertStrings(tb testing.TB, view Viewable, contents []string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	matched := true
	for _, sub := range contents {
		if !strings.Contains(out, sub) {
			cfg.Errorf(tb, "expected output to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	return matched
}

// RequireStrings fails the test immediately unless every substring in
// contents appears in output.
//
// See also [Harness.RequireStrings], [AssertStrings], and [RequireString].
func RequireStrings(tb testing.TB, view Viewable, contents []string, opts ...Option) {
	tb.Helper()

	if !AssertStrings(tb, view, contents, opts...) {
		tb.FailNow()
	}
}

// AssertNotString reports an error if content appears in output. It
// returns whether the output matched and allows the test to continue.
//
// See also [Harness.AssertNotString], [AssertString], [AssertNotStrings], and
// [WaitNotString].
func AssertNotString(tb testing.TB, view Viewable, content string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if strings.Contains(out, content) {
		cfg.Errorf(tb, "expected output not to contain %q\noutput:\n%s", content, out)
		return false
	}
	return true
}

// RequireNotString fails the test immediately if content appears in output.
//
// See also [Harness.RequireNotString], [AssertNotString], [RequireString],
// [AssertNotStrings], and [RequireNotStrings].
func RequireNotString(tb testing.TB, view Viewable, content string, opts ...Option) {
	tb.Helper()

	if !AssertNotString(tb, view, content, opts...) {
		tb.FailNow()
	}
}

// AssertNotStrings reports an error if any substring in contents
// appears in output. It returns whether the output matched and allows the test
// to continue.
//
// See also [Harness.AssertNotStrings], [AssertStrings], [AssertNotString],
// [WaitStrings], and [WaitNotStrings].
func AssertNotStrings(tb testing.TB, view Viewable, contents []string, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	matched := true
	for _, sub := range contents {
		if strings.Contains(out, sub) {
			cfg.Errorf(tb, "expected output not to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	return matched
}

// RequireNotStrings fails the test immediately if any substring in
// contents appears in output.
//
// See also [Harness.RequireNotStrings], [AssertNotStrings], [RequireStrings],
// and [RequireNotString].
func RequireNotStrings(tb testing.TB, view Viewable, contents []string, opts ...Option) {
	tb.Helper()

	if !AssertNotStrings(tb, view, contents, opts...) {
		tb.FailNow()
	}
}

// AssertMatch reports an error unless output matches the regular expression
// pattern. pattern is compiled with [regexp.Compile]; a compile error fails
// the test immediately.
// It returns whether the output matched and allows the test to continue.
//
// See also [Harness.AssertMatch], [WaitMatch], [AssertNotMatch], and [RequireMatch].
func AssertMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) bool {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if !re.MatchString(out) {
		cfg.Errorf(tb, "expected output to match %q\noutput:\n%s", pattern, out)
		return false
	}
	return true
}

// RequireMatch fails the test immediately unless output matches the regular
// expression pattern.
//
// See also [Harness.RequireMatch], [AssertMatch], [RequireNotMatch], and [WaitMatch].
func RequireMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) {
	tb.Helper()

	if !AssertMatch(tb, view, pattern, opts...) {
		tb.FailNow()
	}
}

// AssertNotMatch reports an error if output matches the regular expression
// pattern. pattern is compiled with [regexp.Compile]; a compile error fails
// the test immediately.
// It returns whether the output matched and allows the test to continue.
//
// See also [Harness.AssertNotMatch], [AssertMatch], [WaitNotMatch], and
// [RequireNotMatch].
func AssertNotMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) bool {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		tb.Fatalf("invalid regexp: %v", err)
	}
	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	if re.MatchString(out) {
		cfg.Errorf(tb, "expected output not to match %q\noutput:\n%s", pattern, out)
		return false
	}
	return true
}

// RequireNotMatch fails the test immediately if output matches the regular
// expression pattern.
//
// See also [Harness.RequireNotMatch], [AssertNotMatch], and [RequireMatch].
func RequireNotMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) {
	tb.Helper()

	if !AssertNotMatch(tb, view, pattern, opts...) {
		tb.FailNow()
	}
}

// AssertHeight reports an error unless output has n rows. Note that this behaves
// differently to [charm.land/lipgloss/v2.Height] which always assumes a minimum
// height of 1.
// It returns whether the output matched and allows the test to continue.
//
// See also [Harness.AssertHeight], [AssertDimensions], [AssertWidth], [Dimensions],
// and [RequireHeight].
func AssertHeight(tb testing.TB, view Viewable, n int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	_, goth := Dimensions(out)
	if goth != n {
		cfg.Errorf(tb, "expected output height %d, got %d", n, goth)
		return false
	}
	return true
}

// RequireHeight fails the test immediately unless output has n rows.
//
// See also [Harness.RequireHeight], [AssertHeight], and [RequireDimensions].
func RequireHeight(tb testing.TB, view Viewable, n int, opts ...Option) {
	tb.Helper()

	if !AssertHeight(tb, view, n, opts...) {
		tb.FailNow()
	}
}

// AssertWidth reports an error unless output has n columns. It returns whether
// the output matched and allows the test to continue.
//
// See also [Harness.AssertWidth], [AssertDimensions], [Dimensions], [AssertHeight],
// and [RequireWidth].
func AssertWidth(tb testing.TB, view Viewable, n int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	gotw, _ := Dimensions(out)
	if gotw != n {
		cfg.Errorf(tb, "expected output width %d, got %d", n, gotw)
		return false
	}
	return true
}

// RequireWidth fails the test immediately unless output has n columns.
//
// See also [Harness.RequireWidth], [AssertWidth], and [RequireDimensions].
func RequireWidth(tb testing.TB, view Viewable, n int, opts ...Option) {
	tb.Helper()

	if !AssertWidth(tb, view, n, opts...) {
		tb.FailNow()
	}
}

// AssertDimensions reports an error unless output has specified dimensions.
//
// See also [Harness.AssertDimensions], [AssertWidth], [AssertHeight], [Dimensions],
// [RequireDimensions], and [WithStripANSI].
func AssertDimensions(tb testing.TB, view Viewable, width, height int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(opts...)
	out := view()
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	gotw, goth := Dimensions(out)
	if gotw != width || goth != height {
		cfg.Errorf(tb, "expected output dimensions %dx%d, got %dx%d", width, height, gotw, goth)
		return false
	}
	return true
}

// RequireDimensions fails the test immediately unless output has specified dimensions.
//
// See also [Harness.RequireDimensions], [AssertDimensions], [RequireHeight],
// [RequireWidth], and [Harness.AssertViewSnapshot].
func RequireDimensions(tb testing.TB, view Viewable, width, height int, opts ...Option) {
	tb.Helper()

	if !AssertDimensions(tb, view, width, height, opts...) {
		tb.FailNow()
	}
}

// Dimensions returns the width and height of the output.
//
// See also [AssertDimensions], [AssertWidth], [AssertHeight], and
// [Harness.AssertDimensions].
func Dimensions(out string, opts ...Option) (w, h int) {
	cfg := collectOptions(opts...)
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}

	if out == "" {
		return 0, 0
	}

	var width int
	var height int
	for line := range strings.SplitSeq(out, "\n") {
		width = max(width, ansi.StringWidth(line))
		height++
	}
	return width, height
}

// MessageCollector exposes messages observed by a test harness. [Harness] implements
// this interface.
type MessageCollector interface {
	MessageHistory() iter.Seq[uv.Event]
	Messages(ctx context.Context) iter.Seq[uv.Event]
	LiveMessages(ctx context.Context) iter.Seq[uv.Event]
}

// AssertHasMessage asserts that at least one message with the same concrete type
// as T has been observed.
func AssertHasMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) bool {
	tb.Helper()
	cfg := collectOptions(opts...)

	for msg := range log.MessageHistory() {
		if _, ok := msg.(T); ok {
			return true
		}
	}

	var zero T
	cfg.Errorf(tb, "no message with type %T found", zero)
	return false
}

// RequireHasMessage requires that at least one message with the same concrete type
// as T has been observed.
func RequireHasMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) {
	tb.Helper()

	if !AssertHasMessage[T](tb, log, opts...) {
		tb.FailNow()
	}
}

// WaitLiveMessage waits until a NEW message (only messages received since this
// function was invoked) with the same concrete type as T has been observed,
// then returns the first match.
func WaitLiveMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) T {
	tb.Helper()

	var match T
	var ok bool

	opts = append(opts, withReason("WaitLiveMessage[%T]", match))

	WaitLiveMessageWhere(tb, log, func(msg uv.Event) bool {
		match, ok = msg.(T)
		return ok
	}, opts...)
	return match
}

// WaitMessage waits until at least one message with the same concrete type
// as T has been observed, then returns the first match.
func WaitMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) T {
	tb.Helper()

	var match T
	var ok bool

	opts = append(opts, withReason("WaitMessage[%T]", match))

	WaitMessageWhere(tb, log, func(msg uv.Event) bool {
		match, ok = msg.(T)
		return ok
	}, opts...)
	return match
}

// WaitLiveMessageWhere waits until "fn" returns true for a NEW message (only messages
// received since this function was invoked).
//
// See also [WaitMessageWhere].
func WaitLiveMessageWhere(tb testing.TB, log MessageCollector, fn func(uv.Event) (ok bool), opts ...Option) uv.Event {
	tb.Helper()

	cfg := collectOptions(opts...)
	ctx, cancel := context.WithTimeout(tb.Context(), cfg.timeout)
	defer cancel()

	observedMessages := newTypeObserver[uv.Event]()

	for msg := range log.LiveMessages(ctx) {
		observedMessages.observe(msg)
		if !fn(msg) {
			continue
		}
		return msg
	}

	cfg.Fatalf(tb,
		"error waiting for messages: %v\n\n%s",
		ctx.Err(),
		observedMessages,
	)
	return nil
}

// WaitMessageWhere waits until "fn" returns true for a message (across all
// received messages).
//
// See also [WaitLiveMessageWhere].
func WaitMessageWhere(tb testing.TB, log MessageCollector, fn func(uv.Event) (ok bool), opts ...Option) uv.Event {
	tb.Helper()

	cfg := collectOptions(opts...)
	ctx, cancel := context.WithTimeout(tb.Context(), cfg.timeout)
	defer cancel()

	observedMessages := newTypeObserver[uv.Event]()

	for msg := range log.Messages(ctx) {
		observedMessages.observe(msg)
		if !fn(msg) {
			continue
		}
		return msg
	}

	cfg.Fatalf(tb,
		"error waiting for messages: %v\n\n%s",
		ctx.Err(),
		observedMessages,
	)
	return nil
}

// WaitSettleMessages waits until no messages have been observed for the
// configured settle timeout.
//
// See also [Harness.WaitSettleMessages], [WaitSettle], [WithSettleTimeout],
// [WithCheckInterval], and [WithTimeout].
func WaitSettleMessages(tb testing.TB, log MessageCollector, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(opts...)
	ctx, cancel := context.WithTimeout(tb.Context(), cfg.timeout)
	defer cancel()

	observedMessages := newTypeObserver[uv.Event]()
	done := make(chan struct{})

	var last atomic.Pointer[time.Time]
	last.Store(new(time.Now()))

	go func() {
		defer close(done)
		for msg := range log.LiveMessages(ctx) {
			if len(cfg.settleIgnore) > 0 && slices.Contains(cfg.settleIgnore, reflect.TypeOf(msg)) {
				continue
			}

			observedMessages.observe(msg)
			last.Store(new(time.Now()))
		}
	}()

	for {
		if time.Since(*last.Load()) >= cfg.settleTimeout {
			return
		}

		select {
		case <-ctx.Done():
			tb.Fatalf("wait for messages to settle canceled: %v\n\n%s", ctx.Err(), observedMessages)
		case <-time.After(min(cfg.checkInterval, cfg.checkInterval-time.Since(*last.Load())) + 5*time.Millisecond):
		}
	}
}

// IgnoreMessagesReflect filters an iterator of messages to exclude messages
// with the same concrete type as any of the provided reflect.Types.
func IgnoreMessagesReflect(tb testing.TB, messages iter.Seq[uv.Event], ignore ...reflect.Type) iter.Seq[uv.Event] {
	tb.Helper()
	return func(yield func(uv.Event) bool) {
		tb.Helper()
		for msg := range messages {
			if len(ignore) > 0 && slices.Contains(ignore, reflect.TypeOf(msg)) {
				continue
			}
			if !yield(msg) {
				return
			}
		}
	}
}

// FilterMessagesType filters an iterator of messages to only include messages
// with the same concrete type as T.
func FilterMessagesType[T uv.Event](tb testing.TB, messages iter.Seq[uv.Event]) iter.Seq[T] {
	tb.Helper()
	var zero T
	target := reflect.TypeOf(zero)
	return func(yield func(T) bool) {
		tb.Helper()
		for msg := range messages {
			if reflect.TypeOf(msg) != target {
				continue
			}
			typed, ok := msg.(T)
			if ok && !yield(typed) {
				return
			}
		}
	}
}

// FilterMessagesFunc filters an iterator of messages to only include messages
// that return true from the provided "fn" function.
func FilterMessagesFunc[T uv.Event](tb testing.TB, messages iter.Seq[uv.Event], fn func(T) bool) iter.Seq[T] {
	tb.Helper()
	return func(yield func(T) bool) {
		tb.Helper()
		for msg := range messages {
			typed, ok := msg.(T)
			if ok && fn(typed) && !yield(typed) {
				return
			}
		}
	}
}
