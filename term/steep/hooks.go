// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"log/slog"
	"testing"
	"time"
)

// InputKind is the kind of an input action: the "action" of its
// "steep.input" annotation.
type InputKind string

const (
	// InputKindType is text typed as key presses ([Harness.Type]).
	InputKindType InputKind = "type"
	// InputKindKey is a key press ([Harness.Key]).
	InputKindKey InputKind = "key"
	// InputKindPaste is a paste ([Harness.Paste]).
	InputKindPaste InputKind = "paste"
	// InputKindMouse is a single mouse event ([Harness.Mouse]).
	InputKindMouse InputKind = "mouse"
	// InputKindClick is a press and release of a mouse button
	// ([Harness.MouseClick]).
	InputKindClick InputKind = "click"
	// InputKindDrag is a press of a mouse button, motion, and its release
	// ([Harness.MouseDrag]).
	InputKindDrag InputKind = "drag"
)

// InputMeta is the metadata of [HookInput] events: keyboard, paste, and
// mouse actions.
type InputMeta struct {
	Kind InputKind `json:"action"`
	// Text is the text of [InputKindType] and [InputKindPaste].
	Text string `json:"text,omitzero"`
	// Key is the key of [InputKindKey], such as "enter" or "ctrl+c".
	Key string `json:"key,omitzero"`
	// Mouse is the mouse event of [InputKindMouse]: "press", "release", or
	// "motion".
	Mouse string `json:"mouse,omitzero"`
	// Button is the mouse button of [InputKindMouse], [InputKindClick], and
	// [InputKindDrag]: "left", "right", "middle", or "none" for motion
	// without one.
	Button string `json:"button,omitzero"`
}

func (m InputMeta) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("kind", string(m.Kind))}
	for _, a := range []slog.Attr{
		slog.String("text", m.Text),
		slog.String("key", m.Key),
		slog.String("mouse", m.Mouse),
		slog.String("button", m.Button),
	} {
		if a.Value.String() != "" {
			attrs = append(attrs, a)
		}
	}
	return slog.GroupValue(attrs...)
}

// WaitKind is what a wait waits for: the "wait" of its "steep.wait.start"
// and "steep.wait.end" annotations.
type WaitKind string

const (
	// WaitKindText waits for text to be on the screen ([Harness.Wait]).
	WaitKindText WaitKind = "text"
	// WaitKindNotText waits for text not to be on the screen
	// ([Harness.WaitNot]).
	WaitKindNotText WaitKind = "not_text"
	// WaitKindMatch waits for a regular expression to match the screen
	// ([Harness.WaitMatch]).
	WaitKindMatch WaitKind = "match"
	// WaitKindNotMatch waits for a regular expression not to match the
	// screen ([Harness.WaitNotMatch]).
	WaitKindNotMatch WaitKind = "not_match"
	// WaitKindFunc waits for a condition of the test's own on the screen
	// ([Harness.WaitFunc]).
	WaitKindFunc WaitKind = "func"
	// WaitKindSettle waits for the screen to stay unchanged
	// ([Harness.WaitSettle]).
	WaitKindSettle WaitKind = "settle"
	// WaitKindSettleMessages waits for the model to receive no messages
	// ([Harness.WaitSettleMessages]).
	WaitKindSettleMessages WaitKind = "settle_messages"
	// WaitKindMessage waits for the model to receive a message of a type
	// ([Harness.WaitMessage] and the like).
	WaitKindMessage WaitKind = "message"
	// WaitKindFinished waits for the program to end
	// ([Harness.WaitFinished]).
	WaitKindFinished WaitKind = "finished"
	// WaitKindSleep waits for time to pass ([Harness.Sleep]).
	WaitKindSleep WaitKind = "sleep"
)

// WaitMeta is the metadata of [HookWait] events, and of the [HookClose]
// event of [Harness.WaitFinished].
type WaitMeta struct {
	Kind WaitKind `json:"wait"`
	// Live is true for a [WaitKindMessage] wait that only counts messages
	// received after it starts, such as [Harness.WaitLiveMessage].
	Live bool `json:"live,omitzero"`
}

func (m WaitMeta) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("kind", string(m.Kind))}
	if m.Live {
		attrs = append(attrs, slog.Bool("live", true))
	}
	return slog.GroupValue(attrs...)
}

// AssertKind is what an assertion asserts: the "assert" of its
// "steep.assert" annotation.
type AssertKind string

const (
	// AssertKindContains asserts that text is on the screen
	// ([Harness.Assert] and [Harness.Require]).
	AssertKindContains AssertKind = "contains"
	// AssertKindNotContains asserts that text is not on the screen
	// ([Harness.AssertNot] and [Harness.RequireNot]).
	AssertKindNotContains AssertKind = "not_contains"
	// AssertKindMatch asserts that a regular expression matches the screen
	// ([Harness.AssertMatch] and [Harness.RequireMatch]).
	AssertKindMatch AssertKind = "match"
	// AssertKindNotMatch asserts that a regular expression does not match
	// the screen ([Harness.AssertNotMatch] and [Harness.RequireNotMatch]).
	AssertKindNotMatch AssertKind = "not_match"
	// AssertKindWidth asserts the width of the screen's text
	// ([Harness.AssertWidth] and [Harness.RequireWidth]).
	AssertKindWidth AssertKind = "width"
	// AssertKindHeight asserts the height of the screen's text
	// ([Harness.AssertHeight] and [Harness.RequireHeight]).
	AssertKindHeight AssertKind = "height"
	// AssertKindDimensions asserts the width and height of the screen's
	// text ([Harness.AssertDimensions] and [Harness.RequireDimensions]).
	AssertKindDimensions AssertKind = "dimensions"
	// AssertKindHasMessage asserts that the model received a message of a
	// type ([Harness.AssertHasMessage] and [Harness.RequireHasMessage]).
	AssertKindHasMessage AssertKind = "has_message"
)

// AssertMeta is the metadata of [HookAssert] events.
type AssertMeta struct {
	Kind AssertKind `json:"assert"`
	// Required is true for a Require method, whose failure stops the test,
	// and false for an Assert method, which lets it go on.
	Required bool `json:"required"`
}

func (m AssertMeta) LogValue() slog.Value {
	return slog.GroupValue(slog.String("kind", string(m.Kind)), slog.Bool("required", m.Required))
}

// CaptureKind is what a capture captures. For a snapshot comparison, it is
// the "format" of its "steep.snapshot" annotation.
type CaptureKind string

const (
	// CaptureKindText compares the screen's text with a ".snap" file
	// ([Harness.AssertSnapshot] and [Harness.RequireSnapshot]).
	CaptureKindText CaptureKind = "text"
	// CaptureKindJSON compares the screen's cells and the terminal's state
	// with a ".json" file ([Harness.AssertJSON] and [Harness.RequireJSON]).
	CaptureKindJSON CaptureKind = "json"
	// CaptureKindImage renders the screen as an image ([Harness.Image] and
	// [Harness.ImageInto]). It records no annotation.
	CaptureKindImage CaptureKind = "image"
)

// CaptureMeta is the metadata of [HookCapture] events.
type CaptureMeta struct {
	Kind CaptureKind `json:"format"`
	// Required is true for a Require method, whose mismatch stops the test.
	Required bool `json:"required"`
}

func (m CaptureMeta) LogValue() slog.Value {
	return slog.GroupValue(slog.String("kind", string(m.Kind)), slog.Bool("required", m.Required))
}

// HookEvent identifies a harness action for hooks, and labels the action's
// mark in the timeline (see [Harness.Mark]).
type HookEvent int

const (
	// HookInput is a keyboard, paste, or mouse action, with [InputMeta].
	HookInput HookEvent = iota
	// HookTerminal is a change to the terminal, such as a resize.
	HookTerminal
	// HookProgram is an action on the program, such as a message sent to it.
	HookProgram
	// HookWait is a wait, or a sleep, with [WaitMeta].
	HookWait
	// HookReady is the program's start.
	HookReady
	// HookClose is the program's end, and the harness's. The wait of
	// [Harness.WaitFinished] has [WaitMeta].
	HookClose
	// HookAssert is an assertion, with [AssertMeta].
	HookAssert
	// HookCapture is a snapshot comparison or image capture, with
	// [CaptureMeta].
	HookCapture
)

// String returns the stable event name used in mark labels and still file
// names.
func (e HookEvent) String() string {
	switch e {
	case HookInput:
		return "input"
	case HookTerminal:
		return "terminal"
	case HookProgram:
		return "program"
	case HookWait:
		return "wait"
	case HookReady:
		return "ready"
	case HookClose:
		return "close"
	case HookAssert:
		return "assert"
	case HookCapture:
		return "capture"
	default:
		return "unknown"
	}
}

// HookStage is whether a hook runs before or after the harness action body.
type HookStage int

const (
	HookStageBefore HookStage = iota
	HookStageAfter
)

func (s HookStage) String() string {
	switch s {
	case HookStageBefore:
		return "before"
	case HookStageAfter:
		return "after"
	}
	return "unknown"
}

// Hook observes harness actions (see [WithHooks]). Errors fail the test
// immediately.
//
// metadata is nil, or the action's [InputMeta], [WaitMeta], [AssertMeta], or
// [CaptureMeta]. Each uses the vocabulary of the [reel.Annotation] the
// harness records for the same action (see reel's annotation.schema.json):
// it marshals to JSON as the fields of that annotation it holds, with the
// same names and values.
type Hook func(event HookEvent, stage HookStage, metadata any) error

func (h *Harness) hook(event HookEvent, metadata any) func() {
	h.tb.Helper()
	var seq uint64
	if h.awaitsPaint(event) {
		seq = h.session.Terminal().OutputSeq()
	}
	h.runHooks(event, HookStageBefore, metadata)
	return func() {
		if h.awaitsPaint(event) {
			h.waitForPaint(seq)
		}
		if d := h.cfg.slow; d > 0 {
			time.Sleep(d)
		}
		// Close places its own mark as it starts.
		if event != HookClose || !h.closed.Load() {
			h.markEvent(event)
		}
		h.runHooks(event, HookStageAfter, metadata)
	}
}

// awaitsPaint reports whether the mark and After hooks for event must wait
// until the program has painted the action's terminal output. Without the
// wait, a still at the mark shows the screen from before the action. It only
// waits while an export policy is on, so tests that write nothing do not pay
// for it.
func (h *Harness) awaitsPaint(event HookEvent) bool {
	if h.cfg.policy == exportOff {
		return false
	}
	return event == HookInput || event == HookProgram || event == HookTerminal
}

// waitForPaint blocks until the terminal has accepted program output newer
// than start and then stayed idle for one check interval, or until the paint
// wait elapses. Synchronized-output holds do not count as idle: the visible
// grid is still the previous frame.
func (h *Harness) waitForPaint(start uint64) {
	h.tb.Helper()
	quiet := h.cfg.checkInterval
	if quiet <= 0 {
		quiet = time.Millisecond
	}
	deadline := time.Now().Add(h.paintWait())
	var idleAt time.Time
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	for {
		seq := h.session.Terminal().OutputSeq()
		held := h.session.Terminal().Held()
		if seq != start && !held {
			start = seq
			idleAt = time.Now().Add(quiet)
		}
		now := time.Now()
		if !idleAt.IsZero() && !held && seq == start && !now.Before(idleAt) {
			return
		}
		if idleAt.IsZero() && !now.Before(deadline) {
			return
		}
		select {
		case <-h.cfg.ctx.Done():
			return
		case <-timer.C:
			timer.Reset(time.Millisecond)
		}
	}
}

// paintWait is how long an exporting action waits for a program write before
// it is marked. A paint that never comes must not stall for
// the full test timeout. The floor covers a harness whose settle interval is
// shorter than one scheduler delay.
func (h *Harness) paintWait() time.Duration {
	wait := h.cfg.settleTimeout
	if wait < 50*time.Millisecond {
		wait = 50 * time.Millisecond
	}
	return wait
}

func (h *Harness) runHooks(event HookEvent, stage HookStage, metadata any) {
	h.tb.Helper()
	for _, hook := range h.hooks {
		if hook == nil {
			continue
		}
		if err := hook(event, stage, metadata); err != nil {
			h.tb.Fatalf("hook %s %s: %v", event, stage.String(), err)
		}
	}
}

type testLogWriter struct{ tb testing.TB }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.tb.Helper()
	w.tb.Log(string(p))
	return len(p), nil
}

// LoggerHook returns a hook that mirrors events to logger. When logger is nil,
// events are written with [testing.TB.Log].
func LoggerHook(tb testing.TB, logger *slog.Logger) Hook {
	tb.Helper()
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(testLogWriter{tb: tb}, nil))
	}
	return func(event HookEvent, stage HookStage, metadata any) error {
		args := []slog.Attr{
			slog.String("event", event.String()),
			slog.String("stage", stage.String()),
		}
		if metadata != nil {
			args = append(args, slog.Any("metadata", metadata))
		}
		logger.LogAttrs(tb.Context(), slog.LevelInfo, "steep", args...)
		return nil
	}
}
