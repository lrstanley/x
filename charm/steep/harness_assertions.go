// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
)

// WaitFinished waits for the [tea.Program] to finish.
//
// See also [Harness.FinalModel] and [Harness.WaitSettleMessages].
func (h *Harness) WaitFinished(opts ...Option) {
	h.tb.Helper()
	defer h.hook(HookClose, nil)()
	h.waitFinished(opts...)
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

// WaitSettle waits until the rendered view string from [Harness.View] has not
// changed for the configured settle timeout. It polls this harness's
// [Harness.View] and compares each result to the previous sample.
//
// See also [Harness.WaitSettleMessages], [WaitSettle], [WithSettle],
// [WithCheck], and [WithTimeout].
func (h *Harness) WaitSettle(opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "settle"})()
	WaitSettle(h.tb, h.View, h.mergedOpts(opts...)...)
	return h
}

// WaitFunc waits until condition returns true for the latest view output.
// T is the view type passed to condition: string or []byte.
//
// See also [WaitViewFunc], [Harness.Wait], [Harness.WaitNot], and [Harness.WaitMatch].
func (h *Harness) WaitFunc[T ~string | ~[]byte](condition func(view T) bool, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "func"})()
	WaitViewFunc(h.tb, h.View, condition, h.mergedOpts(opts...)...)
	return h
}

// Wait waits until view output contains contents. contents is a string, a
// []byte sequence, or every string in a []string.
//
// See also [Wait], [Harness.WaitFunc], [Harness.WaitNot], and [Harness.WaitMatch].
func (h *Harness) Wait[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "wait"})()
	Wait(h.tb, h.View, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNot waits until view output contains none of contents. contents is a
// string, a []byte sequence, or any string in a []string.
//
// See also [WaitNot], [Harness.Wait], and [Harness.WaitNotMatch].
func (h *Harness) WaitNot[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "not"})()
	WaitNot(h.tb, h.View, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitMatch waits until the latest view output matches the regular expression pattern.
//
// See also [WaitMatch], [Harness.WaitNotMatch], [Harness.AssertMatch], and
// [Harness.RequireMatch].
func (h *Harness) WaitMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookSettle, SettleMeta{Kind: "match"})()
	WaitMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
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
	WaitNotMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// Assert reports an error unless content appears in view output. content is a
// string or every string in a []string. It allows the test to continue.
//
// See also [Assert], [Harness.Require], and [Harness.Wait].
func (h *Harness) Assert[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assert"})()
	Assert(h.tb, h.View, content, h.mergedOpts(opts...)...)
	return h
}

// Require fails the test immediately unless content appears in view output.
// content is a string or every string in a []string.
//
// See also [Require], [Harness.Assert], and [Harness.Wait].
func (h *Harness) Require[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "require"})()
	if !Assert(h.tb, h.View, content, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertNot reports an error if content appears in view output. content is a
// string or any string in a []string. It allows the test to continue.
//
// See also [AssertNot], [Harness.Assert], and [Harness.WaitNot].
func (h *Harness) AssertNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "assert_not"})()
	AssertNot(h.tb, h.View, content, h.mergedOpts(opts...)...)
	return h
}

// RequireNot fails the test immediately if content appears in view output.
// content is a string or any string in a []string.
//
// See also [RequireNot], [Harness.Require], and [Harness.WaitNot].
func (h *Harness) RequireNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "require_not"})()
	if !AssertNot(h.tb, h.View, content, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
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
	AssertMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
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
	if !AssertMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
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
	AssertNotMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
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
	if !AssertNotMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
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
	AssertHeight(h.tb, h.View, height, h.mergedOpts(opts...)...)
	return h
}

// RequireHeight fails the test immediately unless view output has height rows.
//
// See also [RequireHeight], [Harness.AssertHeight], [Harness.RequireDimensions],
// and [AssertDimensions].
func (h *Harness) RequireHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireHeight"})()
	if !AssertHeight(h.tb, h.View, height, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
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
	AssertWidth(h.tb, h.View, width, h.mergedOpts(opts...)...)
	return h
}

// RequireWidth fails the test immediately unless view output has width columns.
//
// See also [RequireWidth], [Harness.AssertWidth], [Harness.RequireDimensions],
// and [AssertDimensions].
func (h *Harness) RequireWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: "requireWidth"})()
	if !AssertWidth(h.tb, h.View, width, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
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
	AssertDimensions(h.tb, h.View, width, height, h.mergedOpts(opts...)...)
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
	if !AssertDimensions(h.tb, h.View, width, height, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
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
	WaitSettleMessages(h.tb, h, h.mergedOpts(opts...)...)
	return h
}
