// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"errors"
	"fmt"
	"iter"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// WaitFinished waits for the [tea.Program] to finish.
//
// See also [Harness.FinalModel] and [Harness.WaitSettleMessages].
func (h *Harness) WaitFinished(opts ...Option) {
	h.tb.Helper()
	defer h.hook(HookClose, nil)()
	w := h.startWait(waitNote{Wait: "finished"}, h.mergedOpts(opts...))
	defer w.finish()
	h.waitFinished(opts...)
	w.satisfied(nil, "")
}

func (h *Harness) waitFinished(opts ...Option) {
	h.tb.Helper()

	cfg := collectOptions(h.tb, h.mergedOpts(opts...)...)

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-h.done:
		h.resultMu.RLock()
		result := h.result
		h.resultMu.RUnlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			h.tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		if wrapper, ok := result.model.(*observer); ok {
			h.observer.replace(wrapper.currentModel())
		}
	case <-timer.C:
		h.tb.Fatalf("timeout waiting for bubble tea program to finish after %s", cfg.timeout)
	case <-cfg.ctx.Done():
		h.tb.Fatalf("test context canceled waiting for bubble tea program to finish: %v", cfg.ctx.Err())
	}
}

// Sleep lets the program run for d without waiting on anything in particular,
// such as to hold on a screen while a recording plays it, or to let an
// animation run. It returns early, failing the test, if the context set with
// [WithContext] is canceled.
//
// Prefer a wait for the screen the test expects, such as [Harness.Wait], when
// there is one: a wait does not depend on how fast the program runs.
func (h *Harness) Sleep(d time.Duration, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "sleep"})()

	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "sleep", DurationMS: d.Milliseconds()}, opts)
	defer w.finish()
	cfg := collectOptions(h.tb, opts...)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		w.satisfied(nil, "")
	case <-cfg.ctx.Done():
		h.tb.Fatalf("sleep canceled: %v", cfg.ctx.Err())
	}
	return h
}

// WaitSettle waits until the rendered view string from [Harness.View] has not
// changed for the configured settle timeout. It polls this harness's
// [Harness.View] and compares each result to the previous sample.
//
// See also [Harness.WaitSettleMessages], [WaitSettle], [WithSettle],
// [WithCheck], and [WithTimeout].
func (h *Harness) WaitSettle(opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "settle"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "settle"}, opts)
	defer w.finish()
	WaitSettle(h.tb, h.View, opts...)
	w.satisfied(nil, "")
	return h
}

// WaitFunc waits until condition returns true for the latest view output.
// T is the view type passed to condition: string or []byte.
//
// See also [WaitViewFunc], [Harness.Wait], [Harness.WaitNot], and [Harness.WaitMatch].
func (h *Harness) WaitFunc[T ~string | ~[]byte](condition func(view T) bool, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "func"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "func"}, opts)
	defer w.finish()
	WaitViewFunc(h.tb, h.View, condition, opts...)
	w.satisfied(nil, "")
	return h
}

// Wait waits until view output contains contents. contents is a string, a
// []byte sequence, or every string in a []string.
//
// See also [Wait], [Harness.WaitFunc], [Harness.WaitNot], and [Harness.WaitMatch].
func (h *Harness) Wait[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "wait"})()
	opts = h.mergedOpts(opts...)
	text := texts(contents)
	w := h.startWait(waitNote{Wait: "text", Text: text}, opts)
	defer w.finish()
	view := Wait(h.tb, h.View, contents, opts...)
	w.satisfied(textMatches(view, text), "")
	return h
}

// WaitNot waits until view output contains none of contents. contents is a
// string, a []byte sequence, or any string in a []string.
//
// See also [WaitNot], [Harness.Wait], and [Harness.WaitNotMatch].
func (h *Harness) WaitNot[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "not"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "not_text", Text: texts(contents)}, opts)
	defer w.finish()
	WaitNot(h.tb, h.View, contents, opts...)
	w.satisfied(nil, "")
	return h
}

// WaitMatch waits until the latest view output matches the regular expression pattern.
//
// See also [WaitMatch], [Harness.WaitNotMatch], [Harness.AssertMatch], and
// [Harness.RequireMatch].
func (h *Harness) WaitMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "match"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "match", Pattern: pattern}, opts)
	defer w.finish()
	view := WaitMatch(h.tb, h.View, pattern, opts...)
	w.satisfied(patternMatches(view, pattern), "")
	return h
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern.
//
// See also [WaitNotMatch], [Harness.WaitMatch], [Harness.AssertNotMatch], and
// [Harness.RequireNotMatch].
func (h *Harness) WaitNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "not_match"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "not_match", Pattern: pattern}, opts)
	defer w.finish()
	WaitNotMatch(h.tb, h.View, pattern, opts...)
	w.satisfied(nil, "")
	return h
}

// Assert reports an error unless content appears in view output. content is a
// string or every string in a []string. It allows the test to continue.
//
// See also [Assert], [Harness.Require], and [Harness.Wait].
func (h *Harness) Assert[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assert"})()
	assertText(h, false, true, content, opts)
	return h
}

// Require fails the test immediately unless content appears in view output.
// content is a string or every string in a []string.
//
// See also [Require], [Harness.Assert], and [Harness.Wait].
func (h *Harness) Require[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "require"})()
	assertText(h, true, true, content, opts)
	return h
}

// AssertNot reports an error if content appears in view output. content is a
// string or any string in a []string. It allows the test to continue.
//
// See also [AssertNot], [Harness.Assert], and [Harness.WaitNot].
func (h *Harness) AssertNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assert_not"})()
	assertText(h, false, false, content, opts)
	return h
}

// RequireNot fails the test immediately if content appears in view output.
// content is a string or any string in a []string.
//
// See also [RequireNot], [Harness.Require], and [Harness.WaitNot].
func (h *Harness) RequireNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "require_not"})()
	assertText(h, true, false, content, opts)
	return h
}

// AssertMatch reports an error unless view output matches the regular expression
// pattern.
//
// See also [AssertMatch], [Harness.RequireMatch], [Harness.WaitMatch],
// [Harness.AssertNotMatch], [Harness.RequireNotMatch], and [Harness.WaitNotMatch].
func (h *Harness) AssertMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assertMatch"})()
	assertPattern(h, false, true, pattern, opts)
	return h
}

// RequireMatch fails the test immediately unless view output matches the regular
// expression pattern.
//
// See also [RequireMatch], [Harness.AssertMatch], [Harness.RequireNotMatch], and
// [Harness.WaitMatch].
func (h *Harness) RequireMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireMatch"})()
	assertPattern(h, true, true, pattern, opts)
	return h
}

// AssertNotMatch reports an error if view output matches the regular expression
// pattern.
//
// See also [AssertNotMatch], [Harness.RequireNotMatch], [Harness.AssertMatch],
// [Harness.RequireMatch], and [Harness.WaitNotMatch].
func (h *Harness) AssertNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assertNotMatch"})()
	assertPattern(h, false, false, pattern, opts)
	return h
}

// RequireNotMatch fails the test immediately if view output matches the regular
// expression pattern.
//
// See also [RequireNotMatch], [Harness.AssertNotMatch], [Harness.AssertMatch],
// [Harness.RequireMatch], and [Harness.WaitNotMatch].
func (h *Harness) RequireNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireNotMatch"})()
	assertPattern(h, true, false, pattern, opts)
	return h
}

// AssertHeight reports an error unless view output has height rows. It allows
// the test to continue.
//
// See also [AssertHeight], [Harness.RequireHeight], [Harness.AssertDimensions],
// [Harness.AssertWidth], [AssertDimensions], and [Dimensions].
func (h *Harness) AssertHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assertHeight"})()
	assertDims(h, false, nil, &height, opts)
	return h
}

// RequireHeight fails the test immediately unless view output has height rows.
//
// See also [RequireHeight], [Harness.AssertHeight], [Harness.RequireDimensions],
// and [AssertDimensions].
func (h *Harness) RequireHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireHeight"})()
	assertDims(h, true, nil, &height, opts)
	return h
}

// AssertWidth reports an error unless view output has width columns. It allows
// the test to continue.
//
// See also [AssertWidth], [Harness.RequireWidth], [Harness.AssertDimensions],
// [Harness.AssertHeight], [AssertDimensions], and [Dimensions].
func (h *Harness) AssertWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assertWidth"})()
	assertDims(h, false, &width, nil, opts)
	return h
}

// RequireWidth fails the test immediately unless view output has width columns.
//
// See also [RequireWidth], [Harness.AssertWidth], [Harness.RequireDimensions],
// and [AssertDimensions].
func (h *Harness) RequireWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireWidth"})()
	assertDims(h, true, &width, nil, opts)
	return h
}

// AssertDimensions reports an error unless view output has width columns and height
// rows. It allows the test to continue.
//
// See also [AssertDimensions], [Harness.RequireDimensions], [Harness.AssertWidth],
// [Harness.AssertHeight], [Harness.RequireSnapshot], and [Dimensions].
func (h *Harness) AssertDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assertDimensions"})()
	assertDims(h, false, &width, &height, opts)
	return h
}

// RequireDimensions fails the test immediately unless view output has width
// columns and height rows.
//
// See also [RequireDimensions], [Harness.AssertDimensions], [Harness.RequireHeight],
// [Harness.RequireWidth], and [Harness.RequireSnapshot].
func (h *Harness) RequireDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireDimensions"})()
	assertDims(h, true, &width, &height, opts)
	return h
}

// WaitSettleMessages waits until no messages have been observed for the
// configured settle timeout.
//
// See also [WaitSettleMessages], [Harness.WaitSettle], [WaitSettle],
// [WithSettle], [WithCheck], and [WithTimeout].
func (h *Harness) WaitSettleMessages(opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "messages"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "settle_messages"}, opts)
	defer w.finish()
	WaitSettleMessages(h.tb, h, opts...)
	w.satisfied(nil, "")
	return h
}

// WaitMessage waits until at least one message with concrete type T has been
// observed, then returns the first match.
//
// See also [WaitMessage], [Harness.WaitMessageWhere], and [Harness.WaitLiveMessage].
func (h *Harness) WaitMessage[T uv.Event](opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "message"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "message", Type: typeName[T]()}, opts)
	defer w.finish()
	msg := WaitMessage[T](h.tb, h, opts...)
	w.satisfied(nil, fmt.Sprintf("%T", msg))
	return msg
}

// WaitLiveMessage waits until a new message with concrete type T arrives after
// this call, then returns the first match.
//
// See also [WaitLiveMessage], [Harness.WaitMessage], and [Harness.WaitLiveMessageWhere].
func (h *Harness) WaitLiveMessage[T uv.Event](opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "live_message"})()
	opts = h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "message", Type: typeName[T](), Live: true}, opts)
	defer w.finish()
	msg := WaitLiveMessage[T](h.tb, h, opts...)
	w.satisfied(nil, fmt.Sprintf("%T", msg))
	return msg
}

// WaitMessageWhere waits until fn returns true for a message of type T across
// history and later messages. T is inferred from fn.
//
// See also [WaitMessageWhere] and [Harness.WaitLiveMessageWhere].
func (h *Harness) WaitMessageWhere[T uv.Event](fn func(T) bool, opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "message_where"})()
	return waitMessageWhere[T](h, false, fn, opts...)
}

// WaitLiveMessageWhere waits until fn returns true for a message of type T
// received after this call. T is inferred from fn.
//
// See also [WaitLiveMessageWhere] and [Harness.WaitMessageWhere].
func (h *Harness) WaitLiveMessageWhere[T uv.Event](fn func(T) bool, opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "live_message_where"})()
	return waitMessageWhere[T](h, true, fn, opts...)
}

// waitMessageWhere waits until fn returns true for a message of type T, and
// records the wait. With live, only messages after the call count.
func waitMessageWhere[T uv.Event](h *Harness, live bool, fn func(T) bool, opts ...Option) T {
	h.tb.Helper()
	var match T
	pred := func(msg uv.Event) bool {
		typed, ok := msg.(T)
		if !ok || !fn(typed) {
			return false
		}
		match = typed
		return true
	}
	merged := h.mergedOpts(opts...)
	w := h.startWait(waitNote{Wait: "message", Type: typeName[T](), Live: live}, merged)
	defer w.finish()
	var msg uv.Event
	if live {
		msg = WaitLiveMessageWhere(h.tb, h, pred, merged...)
	} else {
		msg = WaitMessageWhere(h.tb, h, pred, merged...)
	}
	w.satisfied(nil, fmt.Sprintf("%T", msg))
	return match
}

// AssertHasMessage reports an error unless history contains a message of type T.
//
// See also [AssertHasMessage] and [Harness.RequireHasMessage].
func (h *Harness) AssertHasMessage[T uv.Event](opts ...Option) bool {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "hasMessage"})()
	pass := AssertHasMessage[T](h.tb, h, h.mergedOpts(opts...)...)
	h.finishAssert(assertNote{Assert: "has_message", Pass: pass, Type: typeName[T]()})
	return pass
}

// RequireHasMessage fails the test immediately unless history contains a message
// of type T.
//
// See also [RequireHasMessage] and [Harness.AssertHasMessage].
func (h *Harness) RequireHasMessage[T uv.Event](opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireHasMessage"})()
	pass := AssertHasMessage[T](h.tb, h, h.mergedOpts(opts...)...)
	h.finishAssert(assertNote{Assert: "has_message", Required: true, Pass: pass, Type: typeName[T]()})
	return h
}

// FilterMessagesType yields history messages whose concrete type is T.
//
// See also [FilterMessagesType] and [Harness.FilterMessagesFunc].
func (h *Harness) FilterMessagesType[T uv.Event]() iter.Seq[T] {
	h.tb.Helper()
	return FilterMessagesType[T](h.tb, h.MessageHistory())
}

// FilterMessagesFunc yields history messages of type T for which fn returns true.
//
// See also [FilterMessagesFunc] and [Harness.FilterMessagesType].
func (h *Harness) FilterMessagesFunc[T uv.Event](fn func(T) bool) iter.Seq[T] {
	h.tb.Helper()
	return FilterMessagesFunc[T](h.tb, h.MessageHistory(), fn)
}

// assertText asserts that view output contains content, or with present
// false, that it does not, and records it. With required, a failure stops
// the test.
func assertText[T string | []string](h *Harness, required, present bool, content T, opts []Option) {
	h.tb.Helper()
	opts = h.mergedOpts(opts...)
	view := h.sampleView(opts)
	sample := func() string { return view }
	n := assertNote{Assert: "contains", Required: required, Text: texts(content)}
	if present {
		n.Pass = Assert(h.tb, sample, content, opts...)
		if n.Pass {
			n.Matches = textMatches(view, n.Text)
		}
	} else {
		n.Assert = "not_contains"
		n.Pass = AssertNot(h.tb, sample, content, opts...)
	}
	h.finishAssert(n)
}

// assertPattern asserts that view output matches pattern, or with present
// false, that it does not, and records it. With required, a failure stops
// the test.
func assertPattern(h *Harness, required, present bool, pattern string, opts []Option) {
	h.tb.Helper()
	opts = h.mergedOpts(opts...)
	view := h.sampleView(opts)
	sample := func() string { return view }
	n := assertNote{Assert: "match", Required: required, Pattern: pattern}
	if present {
		n.Pass = AssertMatch(h.tb, sample, pattern, opts...)
		if n.Pass {
			n.Matches = patternMatches(view, pattern)
		}
	} else {
		n.Assert = "not_match"
		n.Pass = AssertNotMatch(h.tb, sample, pattern, opts...)
	}
	h.finishAssert(n)
}

// assertDims asserts that view output is width columns wide and height rows
// high, either of which may be nil to not check it, and records it. With
// required, a failure stops the test.
func assertDims(h *Harness, required bool, width, height *int, opts []Option) {
	h.tb.Helper()
	opts = h.mergedOpts(opts...)
	view := h.sampleView(opts)
	sample := func() string { return view }
	n := assertNote{Required: required, Expected: &dims{Width: width, Height: height}}
	switch {
	case width == nil:
		n.Assert = "height"
		n.Pass = AssertHeight(h.tb, sample, *height, opts...)
	case height == nil:
		n.Assert = "width"
		n.Pass = AssertWidth(h.tb, sample, *width, opts...)
	default:
		n.Assert = "dimensions"
		n.Pass = AssertDimensions(h.tb, sample, *width, *height, opts...)
	}
	w, ht := Dimensions(h.tb, view, opts...)
	n.Actual = &dims{Width: &w, Height: &ht}
	h.finishAssert(n)
}
