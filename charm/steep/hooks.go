// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

const defaultSlowExport = 200 * time.Millisecond

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
	h.runHooks(event, HookStageBefore, metadata)
	return func() {
		if d := h.cfg.slow; d > 0 {
			time.Sleep(d)
		}
		h.runHooks(event, HookStageAfter, metadata)
	}
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
	enc *json.Encoder
}

func newHookLog(w io.Writer) *hookLog {
	if w == nil {
		w = io.Discard
	}
	return &hookLog{
		w:   w,
		enc: json.NewEncoder(w),
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
	_ = l.enc.Encode(hookLogEntry{
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
