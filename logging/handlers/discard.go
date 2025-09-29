// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"context"
	"log/slog"
)

// DiscardHandler discards all log records.
type DiscardHandler struct{}

// Enabled implements the [log/slog.Handler] interface.
func (h *DiscardHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return false
}

// Handle implements the [log/slog.Handler] interface.
func (h *DiscardHandler) Handle(_ context.Context, _ slog.Record) error {
	return nil
}

// WithAttrs implements the [log/slog.Handler] interface.
func (h *DiscardHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

// WithGroup implements the [log/slog.Handler] interface.
func (h *DiscardHandler) WithGroup(_ string) slog.Handler {
	return h
}
