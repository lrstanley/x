// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"context"
	"log/slog"
	"sync"
)

var _ slog.Handler = (*Historical)(nil) // Ensure we implement the [log/slog.Handler] interface.

// Historical stores the last X log entries in memory while wrapping another handler.
type Historical struct {
	handler     slog.Handler
	maxEntries  int
	minLevel    slog.Level
	mu          sync.RWMutex
	entries     []slog.Record
	onAddedHook func()
}

// NewHistorical creates a new [log/slog.Handler] that stores the last maxEntries log
// entries in memory. Entries below minLevel are still passed to the wrapped handler
// but are not stored in memory. Returns a pointer to allow calling [Historical.GetEntries].
func NewHistorical(maxEntries int, minLevel slog.Level, handler slog.Handler) *Historical {
	return &Historical{
		handler:    handler,
		maxEntries: maxEntries,
		minLevel:   minLevel,
		entries:    make([]slog.Record, 0, maxEntries),
	}
}

// WithOnAddedHook sets a hook that will be called when a new log entry is added.
// This is useful for things like sending the log entries to a remote server.
// The hook will be called in a new goroutine.
func (h *Historical) WithOnAddedHook(hook func()) *Historical {
	h.mu.Lock()
	h.onAddedHook = hook
	h.mu.Unlock()
	return h
}

// Enabled checks if the wrapped handler is enabled for the given level.
func (h *Historical) Enabled(ctx context.Context, l slog.Level) bool {
	return h.handler.Enabled(ctx, l)
}

// Handle stores the log record in memory (if level >= minLevel) and passes it to
// the wrapped handler. Maintains the maxEntries limit by removing oldest entries.
func (h *Historical) Handle(ctx context.Context, r slog.Record) error {
	// Store in memory if level is at or above minLevel.
	if r.Level >= h.minLevel {
		cloned := r.Clone()
		h.mu.Lock()
		h.entries = append(h.entries, cloned)
		// Trim from front if we exceed maxEntries.
		if len(h.entries) > h.maxEntries {
			h.entries = h.entries[len(h.entries)-h.maxEntries:]
		}
		h.mu.Unlock()

		h.mu.RLock()
		fn := h.onAddedHook
		h.mu.RUnlock()
		if fn != nil {
			go fn()
		}
	}

	// Always pass to wrapped handler, regardless of level.
	return h.handler.Handle(ctx, r)
}

// WithAttrs creates a new handler with additional attributes added to the wrapped handler.
func (h *Historical) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewHistorical(h.maxEntries, h.minLevel, h.handler.WithAttrs(attrs))
}

// WithGroup creates a new handler with a group name applied to the wrapped handler.
func (h *Historical) WithGroup(name string) slog.Handler {
	return NewHistorical(h.maxEntries, h.minLevel, h.handler.WithGroup(name))
}

// GetEntries returns all stored log entries in chronological order (oldest first).
// If you need to modify the entries, you should make a copy of the specified entries.
func (h *Historical) GetEntries() []slog.Record {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.entries
}

func (h *Historical) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}
