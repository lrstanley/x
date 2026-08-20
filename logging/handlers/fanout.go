// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"log/slog"
)

// NewFanout creates a new fanout handler that distributes records to multiple
// [log/slog.Handler] instances.
//
// Deprecated: use [log/slog.NewMultiHandler] instead.
//go:fix inline
func NewFanout(handlers ...slog.Handler) slog.Handler {
	return slog.NewMultiHandler(handlers...)
}
