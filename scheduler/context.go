// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"context"
	"log/slog"
)

type contextKey string

const (
	contextKeyLogger contextKey = "logger"
)

// LoggerFromContext returns the logger from the context. If no logger is found,
// the default logger is returned, which is [slog.Default]. A logger will only
// be available if invoked through [Run].
func LoggerFromContext(ctx context.Context) *slog.Logger {
	l, ok := ctx.Value(contextKeyLogger).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return l
}

func withLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger, l)
}
