// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

const defaultSlowExport = 100 * time.Millisecond

// InputMeta describes keyboard, paste, and mouse harness actions.
type InputMeta struct {
	Kind string `json:"kind,omitempty"`
	Key  string `json:"key,omitempty"`
	Text string `json:"text,omitempty"`
}

func (m InputMeta) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("kind", m.Kind),
		slog.String("key", m.Key),
		slog.String("text", m.Text),
	)
}

// SettleMeta describes blocking settle and wait helpers.
type SettleMeta struct {
	Kind string `json:"kind,omitempty"`
}

func (m SettleMeta) LogValue() slog.Value {
	return slog.StringValue(m.Kind)
}

// AssertMeta describes immediate assertion helpers.
type AssertMeta struct {
	Kind string `json:"kind,omitempty"`
}

func (m AssertMeta) LogValue() slog.Value {
	return slog.StringValue(m.Kind)
}

// CaptureMeta describes snapshot, JSON, and image capture helpers.
type CaptureMeta struct {
	Kind string `json:"kind,omitempty"`
}

func (m CaptureMeta) LogValue() slog.Value {
	return slog.StringValue(m.Kind)
}

// HookEvent identifies a harness action for hooks and export capture.
type HookEvent int

const (
	HookInput HookEvent = iota
	HookTerminal
	HookProgram
	HookSettle
	HookReady
	HookClose
	HookAssert
	HookCapture
)

// String returns the stable event name used in artifact filenames.
func (e HookEvent) String() string {
	switch e {
	case HookInput:
		return "input"
	case HookTerminal:
		return "terminal"
	case HookProgram:
		return "program"
	case HookSettle:
		return "settle"
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

// Hook observes harness actions. Built-in export hooks run before user hooks
// from [WithHooks]. Errors fail the test immediately.
type Hook func(event HookEvent, stage HookStage, metadata any) error

func (h *Harness) hook(event HookEvent, metadata any) func() {
	h.tb.Helper()
	var seq uint64
	if h.awaitsPaint(event) {
		seq = h.emulator.term.OutputSeq()
	}
	h.runHooks(event, HookStageBefore, metadata)
	return func() {
		if h.awaitsPaint(event) {
			h.waitForPaint(seq)
		}
		if d := h.cfg.slow; d > 0 {
			time.Sleep(d)
		}
		h.runHooks(event, HookStageAfter, metadata)
	}
}

// awaitsPaint reports whether After hooks for event must wait until the
// program has committed the action's terminal output. Export reads the
// screen in After; without the wait that read is the previous frame.
func (h *Harness) awaitsPaint(event HookEvent) bool {
	if h.export == nil || h.export.mode == ExportOff {
		return false
	}
	visual := h.export.types.has(ExportTypeGIF) ||
		h.export.types.has(ExportTypePNG) ||
		h.export.types.has(ExportTypeSnapshot)
	if !visual {
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
		seq := h.emulator.term.OutputSeq()
		held := h.emulator.term.Held()
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
// After hooks sample the screen. A paint that never comes must not stall for
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
	if h.export != nil {
		if err := h.export.hook(event, stage, metadata); err != nil {
			h.tb.Fatalf("export hook %s %s: %v", event, stage.String(), err)
		}
	}
	for _, hook := range h.hooks {
		if hook == nil {
			continue
		}
		if err := hook(event, stage, metadata); err != nil {
			h.tb.Fatalf("hook %s %s: %v", event, stage.String(), err)
		}
	}
	if h.logHook != nil {
		h.logHook.Record(event, stage, metadata, time.Now())
	}
}

type hookLog struct {
	mu  sync.Mutex
	w   io.Writer
	enc *jsontext.Encoder
}

func newHookLog(w io.Writer) *hookLog {
	if w == nil {
		w = io.Discard
	}
	return &hookLog{
		w:   w,
		enc: jsontext.NewEncoder(w, json.Deterministic(true)),
	}
}

type hookLogEntry struct {
	Time     time.Time `json:"time"`
	Event    string    `json:"event"`
	Stage    string    `json:"stage"`
	Metadata any       `json:"metadata,omitempty"`
}

func (l *hookLog) Record(event HookEvent, stage HookStage, metadata any, at time.Time) {
	if l == nil || l.enc == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = json.MarshalEncode(l.enc, hookLogEntry{
		Time:     at,
		Event:    event.String(),
		Stage:    stage.String(),
		Metadata: metadata,
	})
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

func openHookLogFile(path string) (*hookLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return newHookLog(f), nil
}
