// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package handlers

import (
	"log/slog"
)

// NewDiscard creates a new discard handler.
//
// Deprecated: Use [log/slog.DiscardHandler] instead.
//
//go:fix inline
func NewDiscard() slog.Handler {
	return slog.DiscardHandler
}
