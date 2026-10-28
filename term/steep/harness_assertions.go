// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"errors"
	"iter"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// WaitFinished waits for the [tea.Program] to finish. It fails the test if
// the program returned an error other than [tea.ErrProgramKilled].
//
// See also [Harness.FinalModel].
func (h *Harness) WaitFinished(opts ...Option) {
	h.tb.Helper()
	meta := WaitMeta{Kind: WaitKindFinished}
	defer h.hook(HookClose, meta)()
	cfg := collectOptions(h.tb, h.mergedOpts(opts...)...)
	w := cfg.startWait(waitNote{WaitMeta: meta})
	defer w.finish()
	h.waitFinished(cfg, w)
}

// waitFinished waits for the program to finish, setting the outcome on w.
func (h *Harness) waitFinished(cfg options, w *waitRecord) {
	h.tb.Helper()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case <-h.done:
		h.resultMu.RLock()
		result := h.result
		h.resultMu.RUnlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			w.ended(outcomeFailed)
			h.tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		w.ended(outcomeSatisfied)
		if wrapper, ok := result.model.(*observer); ok {
			h.observer.replace(wrapper.currentModel())
		}
	case <-timer.C:
		w.ended(outcomeTimeout)
		h.tb.Fatalf("timeout waiting for bubble tea program to finish after %s", cfg.timeout)
	case <-cfg.ctx.Done():
		w.ended(outcomeCanceled)
		h.tb.Fatalf("test context canceled waiting for bubble tea program to finish: %v", cfg.ctx.Err())
	}
}

// Sleep lets the program run for d, such as to hold on a screen in a
// recording or let an animation run. It fails the test if the context is
// canceled first.
//
// Prefer waiting for the expected screen, such as with [Harness.Wait], which
// does not depend on how fast the program runs.
func (h *Harness) Sleep(d time.Duration, opts ...Option) *Harness {
	h.tb.Helper()
	meta := WaitMeta{Kind: WaitKindSleep}
	defer h.hook(HookWait, meta)()

	cfg := collectOptions(h.tb, h.mergedOpts(opts...)...)
	w := h.startWait(waitNote{WaitMeta: meta, DurationMS: d.Milliseconds()})
	defer w.finish()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		w.ended(outcomeSatisfied)
	case <-cfg.ctx.Done():
		w.ended(outcomeCanceled)
		h.tb.Fatalf("sleep canceled: %v", cfg.ctx.Err())
	}
	return h
}

// WaitSettle waits until [Harness.View] has not changed for the settle
// timeout ([WithSettle]).
//
// See also [Harness.WaitSettleMessages].
func (h *Harness) WaitSettle(opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindSettle})()
	WaitSettle(h.tb, h.View, h.mergedOpts(opts...)...)
	return h
}

// WaitFunc waits until condition returns true for the latest view output.
// T is the view type passed to condition: string or []byte.
func (h *Harness) WaitFunc[T ~string | ~[]byte](condition func(view T) bool, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindFunc})()
	WaitViewFunc(h.tb, h.View, condition, h.mergedOpts(opts...)...)
	return h
}

// Wait waits until view output contains contents. contents is a string, a
// []byte sequence, or every string in a []string.
func (h *Harness) Wait[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindText})()
	Wait(h.tb, h.View, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNot waits until view output contains none of contents. contents is a
// string, a []byte sequence, or any string in a []string.
func (h *Harness) WaitNot[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindNotText})()
	WaitNot(h.tb, h.View, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitMatch waits until the latest view output matches the regular expression
// pattern. An invalid pattern fails the test immediately.
func (h *Harness) WaitMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindMatch})()
	WaitMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern. An invalid pattern fails the test immediately.
func (h *Harness) WaitNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindNotMatch})()
	WaitNotMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// Assert reports an error unless content appears in view output. content is a
// string or every string in a []string.
func (h *Harness) Assert[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindContains})()
	Assert(h.tb, h.View, content, h.mergedOpts(opts...)...)
	return h
}

// Require fails the test immediately unless content appears in view output.
// content is a string or every string in a []string.
func (h *Harness) Require[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindContains, Required: true})()
	Require(h.tb, h.View, content, h.mergedOpts(opts...)...)
	return h
}

// AssertNot reports an error if content appears in view output. content is a
// string or any string in a []string.
func (h *Harness) AssertNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindNotContains})()
	AssertNot(h.tb, h.View, content, h.mergedOpts(opts...)...)
	return h
}

// RequireNot fails the test immediately if content appears in view output.
// content is a string or any string in a []string.
func (h *Harness) RequireNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindNotContains, Required: true})()
	RequireNot(h.tb, h.View, content, h.mergedOpts(opts...)...)
	return h
}

// AssertMatch reports an error unless view output matches the regular expression
// pattern.
func (h *Harness) AssertMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindMatch})()
	AssertMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// RequireMatch fails the test immediately unless view output matches the regular
// expression pattern.
func (h *Harness) RequireMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindMatch, Required: true})()
	RequireMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// AssertNotMatch reports an error if view output matches the regular expression
// pattern.
func (h *Harness) AssertNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindNotMatch})()
	AssertNotMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// RequireNotMatch fails the test immediately if view output matches the regular
// expression pattern.
func (h *Harness) RequireNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindNotMatch, Required: true})()
	RequireNotMatch(h.tb, h.View, pattern, h.mergedOpts(opts...)...)
	return h
}

// AssertHeight reports an error unless view output has height rows. It allows
// the test to continue.
func (h *Harness) AssertHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindHeight})()
	AssertHeight(h.tb, h.View, height, h.mergedOpts(opts...)...)
	return h
}

// RequireHeight fails the test immediately unless view output has height rows.
func (h *Harness) RequireHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindHeight, Required: true})()
	RequireHeight(h.tb, h.View, height, h.mergedOpts(opts...)...)
	return h
}

// AssertWidth reports an error unless view output has width columns. It allows
// the test to continue.
func (h *Harness) AssertWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindWidth})()
	AssertWidth(h.tb, h.View, width, h.mergedOpts(opts...)...)
	return h
}

// RequireWidth fails the test immediately unless view output has width columns.
func (h *Harness) RequireWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindWidth, Required: true})()
	RequireWidth(h.tb, h.View, width, h.mergedOpts(opts...)...)
	return h
}

// AssertDimensions reports an error unless view output has width columns and height
// rows.
//
// See also [Harness.Dimensions].
func (h *Harness) AssertDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindDimensions})()
	AssertDimensions(h.tb, h.View, width, height, h.mergedOpts(opts...)...)
	return h
}

// RequireDimensions fails the test immediately unless view output has width
// columns and height rows.
func (h *Harness) RequireDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindDimensions, Required: true})()
	RequireDimensions(h.tb, h.View, width, height, h.mergedOpts(opts...)...)
	return h
}

// WaitSettleMessages waits until the model has received no messages for the
// settle timeout ([WithSettle]), ignoring types from [WithSettleIgnoreMsgs].
//
// See also [Harness.WaitSettle].
func (h *Harness) WaitSettleMessages(opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindSettleMessages})()
	WaitSettleMessages(h.tb, h, h.mergedOpts(opts...)...)
	return h
}

// WaitMessage waits until at least one message with concrete type T has been
// observed, then returns the first match.
func (h *Harness) WaitMessage[T uv.Event](opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindMessage})()
	return WaitMessage[T](h.tb, h, h.mergedOpts(opts...)...)
}

// WaitLiveMessage waits until a new message with concrete type T arrives after
// this call, then returns the first match.
func (h *Harness) WaitLiveMessage[T uv.Event](opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindMessage, Live: true})()
	return WaitLiveMessage[T](h.tb, h, h.mergedOpts(opts...)...)
}

// WaitMessageWhere waits until fn returns true for a message of type T across
// history and later messages. T is inferred from fn. fn should be quick: a
// wait that falls 32 messages behind misses the newer ones (see
// [Harness.Messages]).
func (h *Harness) WaitMessageWhere[T uv.Event](fn func(T) bool, opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindMessage})()
	return waitMessage(h.tb, h, false, fn, h.mergedOpts(opts...)...)
}

// WaitLiveMessageWhere waits until fn returns true for a message of type T
// received after this call. T is inferred from fn. fn should be quick, as for
// [Harness.WaitMessageWhere].
func (h *Harness) WaitLiveMessageWhere[T uv.Event](fn func(T) bool, opts ...Option) T {
	h.tb.Helper()
	defer h.hook(HookWait, WaitMeta{Kind: WaitKindMessage, Live: true})()
	return waitMessage(h.tb, h, true, fn, h.mergedOpts(opts...)...)
}

// AssertHasMessage reports an error unless history contains a message of type T.
func (h *Harness) AssertHasMessage[T uv.Event](opts ...Option) bool {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindHasMessage})()
	return AssertHasMessage[T](h.tb, h, h.mergedOpts(opts...)...)
}

// RequireHasMessage fails the test immediately unless history contains a message
// of type T.
func (h *Harness) RequireHasMessage[T uv.Event](opts ...Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookAssert, AssertMeta{Kind: AssertKindHasMessage, Required: true})()
	RequireHasMessage[T](h.tb, h, h.mergedOpts(opts...)...)
	return h
}

// FilterMessagesType yields history messages whose concrete type is T.
func (h *Harness) FilterMessagesType[T uv.Event]() iter.Seq[T] {
	h.tb.Helper()
	return FilterMessagesType[T](h.tb, h.MessageHistory())
}

// FilterMessagesFunc yields history messages of type T for which fn returns true.
func (h *Harness) FilterMessagesFunc[T uv.Event](fn func(T) bool) iter.Seq[T] {
	h.tb.Helper()
	return FilterMessagesFunc[T](h.tb, h.MessageHistory(), fn)
}
