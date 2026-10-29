// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"errors"
	"iter"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel"
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

	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
	defer cancel()
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
	case <-ctx.Done():
		w.ended(doneOutcome(ctx))
		h.tb.Fatalf("waiting for the bubble tea program to finish: %v", context.Cause(ctx))
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
// timeout ([WithSettle]) (see [reel.Session.WaitSettle]).
//
// See also [Harness.WaitSettleMessages].
func (h *Harness) WaitSettle(opts ...Option) *Harness {
	h.tb.Helper()
	meta := WaitMeta{Kind: WaitKindSettle}
	defer h.hook(HookWait, meta)()
	cfg := collectOptions(h.tb, h.mergedOpts(opts...)...)
	w := cfg.startWait(waitNote{WaitMeta: meta, SettleMS: cfg.settleTimeout.Milliseconds()})
	defer w.finish()
	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
	defer cancel()
	if _, err := h.session.WaitSettle(ctx, cfg.settleTimeout); err != nil {
		w.ended(doneOutcome(ctx))
		cfg.Fatalf(h.tb, "%v (for %s)\nlast output:\n%s", err, cfg.settleTimeout, waitLast(err))
	}
	w.ended(outcomeSatisfied)
	return h
}

// WaitFor waits until cond holds for the screen (see [reel.Session.Wait]),
// such as [reel.Matches] for a regular expression, [reel.ScreenFunc] for a
// condition of the test's own on the screen's text, or [reel.OnCursorLine]
// for one on the cursor's line. cond is checked whenever the screen
// changes.
func (h *Harness) WaitFor(cond reel.Condition, opts ...Option) *Harness {
	h.tb.Helper()
	note := waitNote{WaitMeta: WaitMeta{Kind: WaitKindCondition}, Condition: cond.String()}
	defer h.hook(HookWait, note.WaitMeta)()
	h.waitScreen(cond, note, opts)
	return h
}

// Wait waits until view output contains contents. contents is a string, a
// []byte sequence, or every string in a []string.
func (h *Harness) Wait[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	note := waitNote{WaitMeta: WaitMeta{Kind: WaitKindText}, Text: texts(contents)}
	defer h.hook(HookWait, note.WaitMeta)()
	h.waitScreen(reel.Contains(note.Text...), note, append(opts, withReason("Wait(%q)", contents)))
	return h
}

// WaitNot waits until view output contains none of contents. contents is a
// string, a []byte sequence, or any string in a []string.
func (h *Harness) WaitNot[T string | []byte | []string](contents T, opts ...Option) *Harness {
	h.tb.Helper()
	note := waitNote{WaitMeta: WaitMeta{Kind: WaitKindNotText}, Text: texts(contents)}
	defer h.hook(HookWait, note.WaitMeta)()
	h.waitScreen(reel.NotContains(note.Text...), note, append(opts, withReason("WaitNot(%q)", contents)))
	return h
}

// waitScreen records note and waits for cond with [reel.Session.Wait],
// recording its end, and fails the test if the timeout ([WithTimeout])
// passes first.
func (h *Harness) waitScreen(cond reel.Condition, note waitNote, opts []Option) {
	h.tb.Helper()
	cfg := collectOptions(h.tb, h.mergedOpts(opts...)...)
	w := cfg.startWait(note)
	defer w.finish()
	ctx, cancel := context.WithTimeoutCause(cfg.ctx, cfg.timeout, errWaitTimeout)
	defer cancel()
	res, err := h.session.Wait(ctx, cond)
	if err != nil {
		w.ended(doneOutcome(ctx))
		cfg.Fatalf(h.tb, "%v\nlast output:\n%s", err, waitLast(err))
	}
	w.satisfied(res.Matches)
}

// waitLast returns the last screen a failed [reel.Session] wait saw.
func waitLast(err error) string {
	var werr *reel.WaitError
	if errors.As(err, &werr) {
		return werr.Last
	}
	return ""
}

// AssertFor reports an error unless cond holds for the screen, such as
// [reel.Matches] for a regular expression or [reel.OnCursorLine] for a
// condition on the cursor's line.
func (h *Harness) AssertFor(cond reel.Condition, opts ...Option) *Harness {
	h.tb.Helper()
	n := conditionNote(cond, false)
	defer h.hook(HookAssert, n.AssertMeta)()
	h.assertScreen(cond, n, opts)
	return h
}

// RequireFor is [Harness.AssertFor], failing the test immediately unless
// cond holds.
func (h *Harness) RequireFor(cond reel.Condition, opts ...Option) *Harness {
	h.tb.Helper()
	n := conditionNote(cond, true)
	defer h.hook(HookAssert, n.AssertMeta)()
	h.assertScreen(cond, n, opts)
	return h
}

// Assert reports an error unless content appears in view output. content is a
// string or every string in a []string.
func (h *Harness) Assert[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	n := textNote(AssertKindContains, texts(content), false)
	defer h.hook(HookAssert, n.AssertMeta)()
	h.assertScreen(reel.Contains(n.Text...), n, opts)
	return h
}

// Require fails the test immediately unless content appears in view output.
// content is a string or every string in a []string.
func (h *Harness) Require[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	n := textNote(AssertKindContains, texts(content), true)
	defer h.hook(HookAssert, n.AssertMeta)()
	h.assertScreen(reel.Contains(n.Text...), n, opts)
	return h
}

// AssertNot reports an error if content appears in view output. content is a
// string or any string in a []string.
func (h *Harness) AssertNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	n := textNote(AssertKindNotContains, texts(content), false)
	defer h.hook(HookAssert, n.AssertMeta)()
	h.assertScreen(reel.NotContains(n.Text...), n, opts)
	return h
}

// RequireNot fails the test immediately if content appears in view output.
// content is a string or any string in a []string.
func (h *Harness) RequireNot[T string | []string](content T, opts ...Option) *Harness {
	h.tb.Helper()
	n := textNote(AssertKindNotContains, texts(content), true)
	defer h.hook(HookAssert, n.AssertMeta)()
	h.assertScreen(reel.NotContains(n.Text...), n, opts)
	return h
}

// assertScreen is [checkAssert] of the screen, cursor included.
func (h *Harness) assertScreen(cond reel.Condition, n assertNote, opts []Option) {
	h.tb.Helper()
	screen, err := h.session.ScreenText()
	if err != nil {
		h.check(err)
		return
	}
	checkAssert(h.tb, collectOptions(h.tb, h.mergedOpts(opts...)...), screen, cond, n)
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
