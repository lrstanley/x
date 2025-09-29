// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"context"
	"log/slog"
)

// Discard discards all log records.
type Discard struct{}

// NewDiscard creates a new discard handler.
func NewDiscard() slog.Handler {
	return &Discard{}
}

// Enabled implements the [log/slog.Handler] interface.
func (h *Discard) Enabled(_ context.Context, _ slog.Level) bool {
	return false
}

// Handle implements the [log/slog.Handler] interface.
func (h *Discard) Handle(_ context.Context, _ slog.Record) error {
	return nil
}

// WithAttrs implements the [log/slog.Handler] interface.
func (h *Discard) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

// WithGroup implements the [log/slog.Handler] interface.
func (h *Discard) WithGroup(_ string) slog.Handler {
	return h
}
