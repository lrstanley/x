// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"context"
	"log/slog"
)

var _ slog.Handler = (*levelOverride)(nil) // Ensure we implement the [log/slog.Handler] interface.

// levelOverride overrides the log level of another handler.
type levelOverride struct {
	override slog.Level
	handler  slog.Handler
}

// NewLevelOverride creates a new [log/slog.Handler] that overrides the log level
// of another handler.
func NewLevelOverride(level slog.Level, handler slog.Handler) slog.Handler {
	return &levelOverride{override: level, handler: handler}
}

func (h *levelOverride) Enabled(_ context.Context, l slog.Level) bool {
	return l < h.override
}

func (h *levelOverride) Handle(ctx context.Context, r slog.Record) error {
	return h.handler.Handle(ctx, r)
}

func (h *levelOverride) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.handler.WithAttrs(attrs)
}

func (h *levelOverride) WithGroup(name string) slog.Handler {
	return h.handler.WithGroup(name)
}
