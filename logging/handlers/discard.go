// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"context"
	"log/slog"
)

// discard discards all log records.
type discard struct{}

// NewDiscard creates a new discard handler.
//
// Deprecated: Use [log/slog.DiscardHandler] instead.
func NewDiscard() slog.Handler {
	return &discard{}
}

// Enabled implements the [log/slog.Handler] interface.
func (h *discard) Enabled(_ context.Context, _ slog.Level) bool {
	return false
}

// Handle implements the [log/slog.Handler] interface.
func (h *discard) Handle(_ context.Context, _ slog.Record) error {
	return nil
}

// WithAttrs implements the [log/slog.Handler] interface.
func (h *discard) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

// WithGroup implements the [log/slog.Handler] interface.
func (h *discard) WithGroup(_ string) slog.Handler {
	return h
}
