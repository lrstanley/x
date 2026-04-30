// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
)

// WaitFinished waits for the Bubble Tea program to finish.
//
// See also [Harness.FinalOutput], [Harness.FinalView], [Harness.FinalModel], and
// [Harness.WaitSettleMessages].
func (h *Harness) WaitFinished(opts ...Option) {
	h.tb.Helper()

	cfg := collectOptions(h.mergedOpts(opts...)...)
	h.resultMu.RLock()
	if result := h.result; result != nil {
		h.resultMu.RUnlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			h.tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		return
	}
	h.resultMu.RUnlock()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case result := <-h.done:
		h.resultMu.Lock()
		h.result = &result
		h.resultMu.Unlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			h.tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		if wrapper, ok := result.model.(*observer); ok {
			h.observer.replace(wrapper.currentModel())
		}
	case <-timer.C:
		h.tb.Fatalf("timeout waiting for bubble tea program to finish after %s", cfg.timeout)
	}
}

// WaitSettleMessages waits until no messages have been observed for the
// configured settle timeout.
//
// See also [Harness.WaitSettleView], [WaitSettleView], [WithSettleTimeout],
// [WithCheckInterval], and [WithTimeout].
func (h *Harness) WaitSettleMessages(opts ...Option) *Harness {
	h.tb.Helper()

	cfg := collectOptions(h.mergedOpts(opts...)...)
	h.observer.setSettleIgnore(cfg.settleIgnore)
	defer h.observer.setSettleIgnore(nil)

	deadline := time.Now().Add(cfg.timeout)
	ctx := h.tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		h.observer.mu.RLock()
		updateCount, lastReceivedMessage := len(h.observer.observedMsgs), h.observer.lastReceivedMessage
		h.observer.mu.RUnlock()
		now := time.Now()
		quietFor := now.Sub(lastReceivedMessage)

		if quietFor >= cfg.settleTimeout {
			return h
		}

		remainingTimeout := deadline.Sub(now)
		if remainingTimeout <= 0 {
			h.tb.Fatalf(
				"timeout waiting for Update() to settle after %s; last update was %s ago after %d update(s)",
				cfg.timeout,
				quietFor,
				updateCount,
			)
			return h
		}

		timer.Reset(min(cfg.checkInterval, cfg.settleTimeout-quietFor, remainingTimeout))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			h.tb.Fatalf("wait for Update() to settle canceled: %v", ctx.Err())
		}
	}
}

// WaitSettleView waits until the rendered view string has not changed for the
// configured settle timeout. It polls this harness's [Harness.View] and compares
// each result to the previous sample.
//
// See also [Harness.WaitSettleMessages], [WaitSettleView], [WithSettleTimeout],
// [WithCheckInterval], and [WithTimeout].
func (h *Harness) WaitSettleView(opts ...Option) *Harness {
	h.tb.Helper()
	WaitSettleView(h.tb, h, h.mergedOpts(opts...)...)
	return h
}

// WaitBytesFunc waits until condition returns true for the latest view output.
// It wraps [WaitViewFunc] with a byte-slice predicate.
//
// See also [Harness.WaitStringFunc], [WaitBytes], [WaitNotBytes], and [WaitStrings].
func (h *Harness) WaitBytesFunc(condition func(view []byte) bool, opts ...Option) *Harness {
	h.tb.Helper()
	WaitViewFunc(h.tb, h, condition, h.mergedOpts(opts...)...)
	return h
}

// WaitStringFunc waits until condition returns true for the latest view output.
// It wraps [WaitViewFunc] with a string predicate.
//
// See also [Harness.WaitBytesFunc], [WaitString], [WaitNotString], and [WaitStrings].
func (h *Harness) WaitStringFunc(condition func(view string) bool, opts ...Option) *Harness {
	h.tb.Helper()
	WaitViewFunc(h.tb, h, condition, h.mergedOpts(opts...)...)
	return h
}

// WaitBytes waits until view output contains contents.
//
// See also [WaitBytes], [Harness.WaitString], [Harness.WaitStrings], and
// [Harness.WaitBytesFunc].
func (h *Harness) WaitBytes(contents []byte, opts ...Option) *Harness {
	h.tb.Helper()
	WaitBytes(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitString waits until view output contains contents.
//
// See also [WaitString], [Harness.WaitStrings], [Harness.WaitNotString],
// [Harness.WaitMatch], and [Harness.WaitStringFunc].
func (h *Harness) WaitString(contents string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitString(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitStrings waits until view output contains all contents.
//
// See also [WaitStrings], [Harness.WaitString], and [Harness.WaitNotStrings].
func (h *Harness) WaitStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNotBytes waits until view output contains none of the contents.
//
// See also [WaitNotBytes], [Harness.WaitBytes], [Harness.WaitNotStrings], and
// [Harness.WaitBytesFunc].
func (h *Harness) WaitNotBytes(contents []byte, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotBytes(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNotString waits until view output contains none of the contents.
//
// See also [WaitNotString], [Harness.WaitString], [Harness.WaitStrings], and
// [Harness.WaitNotStrings].
func (h *Harness) WaitNotString(contents string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotString(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNotStrings waits until view output contains none of the contents.
//
// See also [WaitNotStrings], [Harness.WaitStrings], and [Harness.WaitNotString].
func (h *Harness) WaitNotStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitMatch waits until the latest view output matches the regular expression pattern.
//
// See also [WaitMatch], [Harness.WaitNotMatch], [Harness.AssertMatch], and
// [Harness.RequireMatch].
func (h *Harness) WaitMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern.
//
// See also [WaitNotMatch], [Harness.WaitMatch], [Harness.AssertNotMatch], and
// [Harness.RequireNotMatch].
func (h *Harness) WaitNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// AssertString reports an error unless content appears in view output. It
// allows the test to continue.
//
// See also [AssertString], [Harness.RequireString], [Harness.AssertStrings], and
// [Harness.WaitString].
func (h *Harness) AssertString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertString(h.tb, h, content, h.mergedOpts(opts...)...)
	return h
}

// RequireString fails the test immediately unless content appears in view output.
//
// See also [RequireString], [Harness.AssertString], [Harness.RequireStrings], and
// [WaitString].
func (h *Harness) RequireString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertString(h.tb, h, content, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertStrings reports an error unless every substring in contents
// appears in view output. It allows the test to continue.
//
// See also [AssertStrings], [Harness.AssertString], [Harness.RequireStrings], and
// [WaitStrings].
func (h *Harness) AssertStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// RequireStrings fails the test immediately unless every substring in
// contents appears in view output.
//
// See also [RequireStrings], [Harness.RequireString], [AssertStrings], and
// [WaitStrings].
func (h *Harness) RequireStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertStrings(h.tb, h, contents, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertNotString reports an error if content appears in view output. It
// allows the test to continue.
//
// See also [AssertNotString], [Harness.AssertString], [Harness.AssertNotStrings],
// and [Harness.WaitNotString].
func (h *Harness) AssertNotString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertNotString(h.tb, h, content, h.mergedOpts(opts...)...)
	return h
}

// RequireNotString fails the test immediately if content appears in view output.
//
// See also [RequireNotString], [Harness.RequireString], [AssertNotString],
// [Harness.RequireNotStrings], and [Harness.WaitNotStrings].
func (h *Harness) RequireNotString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertNotString(h.tb, h, content, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertNotStrings reports an error if any substring in contents
// appears in view output. It allows the test to continue.
//
// See also [AssertNotStrings], [Harness.AssertStrings], [AssertNotString], and
// [WaitNotStrings].
func (h *Harness) AssertNotStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertNotStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// RequireNotStrings fails the test immediately if any substring in
// contents appears in view output.
//
// See also [RequireNotStrings], [Harness.RequireStrings], [AssertNotStrings],
// [Harness.RequireNotString], and [WaitStrings].
func (h *Harness) RequireNotStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertNotStrings(h.tb, h, contents, h.mergedOpts(opts...)...) {
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
	AssertMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// RequireMatch fails the test immediately unless view output matches the regular
// expression pattern.
//
// See also [RequireMatch], [Harness.AssertMatch], [Harness.RequireNotMatch], and
// [Harness.WaitMatch].
func (h *Harness) RequireMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertMatch(h.tb, h, pattern, h.mergedOpts(opts...)...) {
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
	AssertNotMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// RequireNotMatch fails the test immediately if view output matches the regular
// expression pattern.
//
// See also [RequireNotMatch], [Harness.AssertNotMatch], [Harness.AssertMatch],
// [Harness.RequireMatch], and [Harness.WaitNotMatch].
func (h *Harness) RequireNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertNotMatch(h.tb, h, pattern, h.mergedOpts(opts...)...) {
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
	AssertHeight(h.tb, h, height, h.mergedOpts(opts...)...)
	return h
}

// RequireHeight fails the test immediately unless view output has height rows.
//
// See also [RequireHeight], [Harness.AssertHeight], [Harness.RequireDimensions],
// and [AssertDimensions].
func (h *Harness) RequireHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertHeight(h.tb, h, height, h.mergedOpts(opts...)...) {
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
	AssertWidth(h.tb, h, width, h.mergedOpts(opts...)...)
	return h
}

// RequireWidth fails the test immediately unless view output has width columns.
//
// See also [RequireWidth], [Harness.AssertWidth], [Harness.RequireDimensions],
// and [AssertDimensions].
func (h *Harness) RequireWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertWidth(h.tb, h, width, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertDimensions reports an error unless view output has width columns and height
// rows. It allows the test to continue.
//
// See also [AssertDimensions], [Harness.RequireDimensions], [Harness.AssertWidth],
// [Harness.AssertHeight], [Harness.RequireViewSnapshot], and [Dimensions].
func (h *Harness) AssertDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	AssertDimensions(h.tb, h, width, height, h.mergedOpts(opts...)...)
	return h
}

// RequireDimensions fails the test immediately unless view output has width
// columns and height rows.
//
// See also [RequireDimensions], [Harness.AssertDimensions], [Harness.RequireHeight],
// [Harness.RequireWidth], and [Harness.RequireViewSnapshot].
func (h *Harness) RequireDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertDimensions(h.tb, h, width, height, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}
