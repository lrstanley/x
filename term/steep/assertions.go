// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
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
	"github.com/lrstanley/x/term/steep/internal/xansi"
)

// Viewable returns the output the package-level waits and assertions
// check, such as [Harness.View].
type Viewable func() string

// WaitViewFunc waits until condition returns true for the latest view output.
func WaitViewFunc[T ~string | ~[]byte](
	tb testing.TB,
	view Viewable,
	condition func(view T) bool,
	opts ...Option,
) T {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindFunc}})
	defer w.finish()
	return waitView(tb, view, condition, nil, cfg, w)
}

// waitView waits until condition returns true for the latest view output,
// setting the outcome on w. find, if not nil, locates what satisfied the wait.
func waitView[T ~string | ~[]byte](
	tb testing.TB,
	view Viewable,
	condition func(view T) bool,
	find func(view string) []match,
	cfg options,
	w *waitRecord,
) T {
	tb.Helper()

	deadline := time.Now().Add(cfg.timeout)
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		raw := renderedView(view, cfg.stripANSI)
		out := T(raw)
		if condition(out) {
			w.foundOn(raw, find)
			return out
		}
		remainingTimeout := time.Until(deadline)
		if remainingTimeout <= 0 {
			w.ended(outcomeTimeout)
			cfg.Fatalf(tb, "timeout waiting for condition\nlast output:\n%s", out)
		}

		timer.Reset(min(cfg.checkInterval, remainingTimeout))
		select {
		case <-timer.C:
		case <-cfg.ctx.Done():
			timer.Stop()
			w.ended(outcomeCanceled)
			cfg.Fatalf(tb, "wait for condition canceled: %v", cfg.ctx.Err())
		}
	}
}

func texts[T string | []byte | []string](contents T) []string {
	switch c := any(contents).(type) {
	case string:
		return []string{c}
	case []byte:
		return []string{string(c)}
	case []string:
		return append([]string{}, c...)
	default:
		panic("unreachable")
	}
}

// textMatch reports whether every one of parts is present in view, or, when
// present is false, absent from it.
func textMatch(view string, parts []string, present bool) bool {
	for _, part := range parts {
		if strings.Contains(view, part) != present {
			return false
		}
	}
	return true
}

// Wait waits until output contains contents. contents is a string, a []byte
// sequence, or every string in a []string. It returns the matching view.
func Wait[T string | []byte | []string](tb testing.TB, view Viewable, contents T, opts ...Option) string {
	tb.Helper()
	cfg := collectOptions(tb, append(opts, withReason("Wait(%q)", contents))...)
	parts := texts(contents)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindText}, Text: parts})
	defer w.finish()
	return waitView(tb, view, func(str string) bool {
		return textMatch(str, parts, true)
	}, func(str string) []match {
		return textMatches(str, parts)
	}, cfg, w)
}

// WaitNot waits until output contains none of contents. contents is a string,
// a []byte sequence, or any string in a []string. It returns the matching view.
func WaitNot[T string | []byte | []string](tb testing.TB, view Viewable, contents T, opts ...Option) string {
	tb.Helper()
	cfg := collectOptions(tb, append(opts, withReason("WaitNot(%q)", contents))...)
	parts := texts(contents)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindNotText}, Text: parts})
	defer w.finish()
	return waitView(tb, view, func(str string) bool {
		return textMatch(str, parts, false)
	}, nil, cfg, w)
}

// compilePattern compiles pattern, failing the test (and w) when it is
// invalid.
func compilePattern(tb testing.TB, pattern string, w *waitRecord) *regexp.Regexp {
	tb.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		w.ended(outcomeFailed)
		tb.Fatalf("invalid regexp: %v", err)
	}
	return re
}

// WaitMatch waits until the latest view output matches the regular expression
// pattern. pattern is compiled with [regexp.Compile]; a compile error fails the
// test immediately.
func WaitMatch(tb testing.TB, model Viewable, pattern string, opts ...Option) string {
	tb.Helper()
	cfg := collectOptions(tb, append(opts, withReason("WaitMatch(%q)", pattern))...)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindMatch}, Pattern: pattern})
	defer w.finish()
	re := compilePattern(tb, pattern, w)
	return waitView(tb, model, re.MatchString, func(str string) []match {
		return patternMatches(re, str)
	}, cfg, w)
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern. pattern is compiled with [regexp.Compile]; a compile
// error fails the test immediately.
func WaitNotMatch(tb testing.TB, model Viewable, pattern string, opts ...Option) string {
	tb.Helper()
	cfg := collectOptions(tb, append(opts, withReason("WaitNotMatch(%q)", pattern))...)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindNotMatch}, Pattern: pattern})
	defer w.finish()
	re := compilePattern(tb, pattern, w)
	return waitView(tb, model, func(str string) bool {
		return !re.MatchString(str)
	}, nil, cfg, w)
}

// WaitSettle waits until view's output has not changed for the settle
// timeout ([WithSettle]), checking every [WithCheck] interval.
//
// See also [WaitSettleMessages].
func WaitSettle(tb testing.TB, view Viewable, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindSettle}, SettleMS: cfg.settleTimeout.Milliseconds()})
	defer w.finish()
	deadline := time.Now().Add(cfg.timeout)
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	prev := renderedView(view, cfg.stripANSI)
	lastChange := time.Now()

	for {
		v := renderedView(view, cfg.stripANSI)
		now := time.Now()
		if v != prev {
			prev = v
			lastChange = now
		}
		quietFor := now.Sub(lastChange)

		if quietFor >= cfg.settleTimeout {
			w.ended(outcomeSatisfied)
			return
		}

		remainingTimeout := deadline.Sub(now)
		if remainingTimeout <= 0 {
			w.ended(outcomeTimeout)
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
		case <-cfg.ctx.Done():
			timer.Stop()
			w.ended(outcomeCanceled)
			tb.Fatalf("wait for view to settle canceled: %v", cfg.ctx.Err())
		}
	}
}

func renderedView(view Viewable, stripANSI bool) string {
	out := view()
	if stripANSI {
		out = xansi.StripANSI(out)
	}
	return out
}

// Assert reports an error unless content appears in output. content is a
// string or every string in a []string. It returns whether the output matched.
// Each missing substring is reported separately.
func Assert[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	out := renderedView(view, cfg.stripANSI)
	parts := texts(content)
	matched := true
	for _, sub := range parts {
		if !strings.Contains(out, sub) {
			cfg.Errorf(tb, "expected output to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	cfg.noteAssert(AssertKindContains, assertNote{Text: parts, Pass: matched}, func() []match {
		return textMatches(out, parts)
	})
	return matched
}

// Require fails the test immediately unless content appears in output.
// content is a string or every string in a []string.
func Require[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) {
	tb.Helper()

	if !Assert(tb, view, content, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// AssertNot reports an error if content appears in output. content is a string
// or any string in a []string. It returns whether none appeared and allows the
// test to continue. Each present substring is reported separately.
func AssertNot[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	out := renderedView(view, cfg.stripANSI)
	parts := texts(content)
	matched := true
	for _, sub := range parts {
		if strings.Contains(out, sub) {
			cfg.Errorf(tb, "expected output not to contain %q\noutput:\n%s", sub, out)
			matched = false
		}
	}
	cfg.noteAssert(AssertKindNotContains, assertNote{Text: parts, Pass: matched}, nil)
	return matched
}

// RequireNot fails the test immediately if content appears in output.
// content is a string or any string in a []string.
func RequireNot[T string | []string](tb testing.TB, view Viewable, content T, opts ...Option) {
	tb.Helper()

	if !AssertNot(tb, view, content, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// AssertMatch reports an error unless output matches the regular expression
// pattern. It returns whether the output matched and allows the test to
// continue. An invalid pattern fails the test immediately.
func AssertMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) bool {
	tb.Helper()
	re := compilePattern(tb, pattern, nil)
	cfg := collectOptions(tb, opts...)
	out := renderedView(view, cfg.stripANSI)
	matched := re.MatchString(out)
	if !matched {
		cfg.Errorf(tb, "expected output to match %q\noutput:\n%s", pattern, out)
	}
	cfg.noteAssert(AssertKindMatch, assertNote{Pattern: pattern, Pass: matched}, func() []match {
		return patternMatches(re, out)
	})
	return matched
}

// RequireMatch fails the test immediately unless output matches the regular
// expression pattern.
func RequireMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) {
	tb.Helper()

	if !AssertMatch(tb, view, pattern, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// AssertNotMatch reports an error if output matches the regular expression
// pattern. It returns whether the output did not match and allows the test to
// continue. An invalid pattern fails the test immediately.
func AssertNotMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) bool {
	tb.Helper()
	re := compilePattern(tb, pattern, nil)
	cfg := collectOptions(tb, opts...)
	out := renderedView(view, cfg.stripANSI)
	matched := !re.MatchString(out)
	if !matched {
		cfg.Errorf(tb, "expected output not to match %q\noutput:\n%s", pattern, out)
	}
	cfg.noteAssert(AssertKindNotMatch, assertNote{Pattern: pattern, Pass: matched}, nil)
	return matched
}

// RequireNotMatch fails the test immediately if output matches the regular
// expression pattern.
func RequireNotMatch(tb testing.TB, view Viewable, pattern string, opts ...Option) {
	tb.Helper()

	if !AssertNotMatch(tb, view, pattern, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// AssertHeight reports an error unless output has n rows. Unlike
// [charm.land/lipgloss/v2.Height], empty output has a height of 0. It returns
// whether the output matched.
func AssertHeight(tb testing.TB, view Viewable, n int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	gotw, goth := xansi.Dimensions(renderedView(view, cfg.stripANSI))
	matched := goth == n
	if !matched {
		cfg.Errorf(tb, "expected output height %d, got %d", n, goth)
	}
	cfg.noteAssert(AssertKindHeight, assertNote{
		Pass:     matched,
		Expected: &dims{Height: &n}, Actual: &dims{Width: &gotw, Height: &goth},
	}, nil)
	return matched
}

// RequireHeight fails the test immediately unless output has n rows.
func RequireHeight(tb testing.TB, view Viewable, n int, opts ...Option) {
	tb.Helper()

	if !AssertHeight(tb, view, n, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// AssertWidth reports an error unless output has n columns. It returns whether
// the output matched.
func AssertWidth(tb testing.TB, view Viewable, n int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	gotw, goth := xansi.Dimensions(renderedView(view, cfg.stripANSI))
	matched := gotw == n
	if !matched {
		cfg.Errorf(tb, "expected output width %d, got %d", n, gotw)
	}
	cfg.noteAssert(AssertKindWidth, assertNote{
		Pass:     matched,
		Expected: &dims{Width: &n}, Actual: &dims{Width: &gotw, Height: &goth},
	}, nil)
	return matched
}

// RequireWidth fails the test immediately unless output has n columns.
func RequireWidth(tb testing.TB, view Viewable, n int, opts ...Option) {
	tb.Helper()

	if !AssertWidth(tb, view, n, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// AssertDimensions reports an error unless output has specified dimensions.
// It returns whether the output matched.
func AssertDimensions(tb testing.TB, view Viewable, width, height int, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	gotw, goth := xansi.Dimensions(renderedView(view, cfg.stripANSI))
	matched := gotw == width && goth == height
	if !matched {
		cfg.Errorf(tb, "expected output dimensions %dx%d, got %dx%d", width, height, gotw, goth)
	}
	cfg.noteAssert(AssertKindDimensions, assertNote{
		Pass:     matched,
		Expected: &dims{Width: &width, Height: &height}, Actual: &dims{Width: &gotw, Height: &goth},
	}, nil)
	return matched
}

// RequireDimensions fails the test immediately unless output has specified dimensions.
func RequireDimensions(tb testing.TB, view Viewable, width, height int, opts ...Option) {
	tb.Helper()

	if !AssertDimensions(tb, view, width, height, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// Dimensions returns the width and height of the output.
func Dimensions(tb testing.TB, out string, opts ...Option) (w, h int) {
	cfg := collectOptions(tb, opts...)
	if cfg.stripANSI {
		out = xansi.StripANSI(out)
	}
	return xansi.Dimensions(out)
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
	cfg := collectOptions(tb, opts...)

	found := false
	for msg := range log.MessageHistory() {
		if _, found = msg.(T); found {
			break
		}
	}
	if !found {
		var zero T
		cfg.Errorf(tb, "no message with type %T found", zero)
	}
	cfg.noteAssert(AssertKindHasMessage, assertNote{Type: typeName[T](), Pass: found}, nil)
	return found
}

// RequireHasMessage requires that at least one message with the same concrete type
// as T has been observed.
func RequireHasMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) {
	tb.Helper()

	if !AssertHasMessage[T](tb, log, append(opts, withRequired())...) {
		tb.FailNow()
	}
}

// WaitLiveMessage waits until a message with the same concrete type as T is
// received after the call, then returns it.
func WaitLiveMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) T {
	tb.Helper()
	var zero T
	return waitMessage[T](tb, log, true, nil, append(opts, withReason("WaitLiveMessage[%T]", zero))...)
}

// WaitMessage waits until at least one message with the same concrete type
// as T has been observed, then returns the first match.
func WaitMessage[T uv.Event](tb testing.TB, log MessageCollector, opts ...Option) T {
	tb.Helper()
	var zero T
	return waitMessage[T](tb, log, false, nil, append(opts, withReason("WaitMessage[%T]", zero))...)
}

// WaitLiveMessageWhere waits until fn returns true for a message received
// after the call, then returns it.
func WaitLiveMessageWhere(tb testing.TB, log MessageCollector, fn func(uv.Event) (ok bool), opts ...Option) uv.Event {
	tb.Helper()
	return waitMessage(tb, log, true, fn, opts...)
}

// WaitMessageWhere waits until fn returns true for any received message,
// past or future, then returns it.
func WaitMessageWhere(tb testing.TB, log MessageCollector, fn func(uv.Event) (ok bool), opts ...Option) uv.Event {
	tb.Helper()
	return waitMessage(tb, log, false, fn, opts...)
}

// waitMessage waits for a message of type T for which fn, if not nil,
// returns true. With live, only messages received after the call count.
func waitMessage[T uv.Event](tb testing.TB, log MessageCollector, live bool, fn func(T) bool, opts ...Option) T {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindMessage, Live: live}, Type: typeName[T]()})
	defer w.finish()
	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
	defer cancel()

	messages := log.Messages
	if live {
		messages = log.LiveMessages
	}
	observedMessages := newTypeObserver[uv.Event]()

	for msg := range messages(ctx) {
		observedMessages.observe(msg)
		if typed, ok := msg.(T); ok && (fn == nil || fn(typed)) {
			w.received(msg)
			return typed
		}
	}

	w.ended(doneOutcome(ctx))
	cfg.Fatalf(tb,
		"error waiting for messages: %v\n\n%s",
		ctx.Err(),
		observedMessages,
	)
	var zero T
	return zero
}

// WaitSettleMessages waits until no messages have been observed for the
// settle timeout ([WithSettle]). Types from [WithSettleIgnoreMsgs] do not
// count.
//
// See also [WaitSettle].
func WaitSettleMessages(tb testing.TB, log MessageCollector, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	w := cfg.startWait(waitNote{WaitMeta: WaitMeta{Kind: WaitKindSettleMessages}, SettleMS: cfg.settleTimeout.Milliseconds()})
	defer w.finish()

	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
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
			w.ended(outcomeSatisfied)
			return
		}

		delay := cfg.checkInterval - time.Since(*last.Load())
		if delay < 0 {
			// A negative delay would spin the loop and can starve the program
			// under -race.
			delay = cfg.checkInterval
		}

		select {
		case <-ctx.Done():
			w.ended(doneOutcome(ctx))
			tb.Fatalf("wait for messages to settle canceled: %v\n\n%s", ctx.Err(), observedMessages)
		case <-time.After(delay + 5*time.Millisecond):
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
// of type T for which fn returns true.
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
