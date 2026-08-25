// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpclog

import "context"

type contextKey string

const (
	logKey           contextKey = "log"
	traceKey         contextKey = "trace"
	traceRequestKey  contextKey = "traceRequest"
	traceResponseKey contextKey = "traceResponse"
)

// WithLog returns a copy of ctx that controls whether request and response logging
// is performed for the associated request. When log is false, all logging (including
// errors) is suppressed. Requests without an explicit value default to logging.
// Child contexts override parent values.
func WithLog(ctx context.Context, log bool) context.Context {
	return context.WithValue(ctx, logKey, log)
}

// WithTrace returns a copy of ctx that controls full request and response tracing
// (bodies and full headers) for the associated request. When trace is true, tracing
// is enabled; when false, tracing is disabled for both request and response unless
// overridden by [WithTraceRequest] or [WithTraceResponse]. Requests without an
// explicit value defer to transport configuration. Child contexts override parent values.
func WithTrace(ctx context.Context, trace bool) context.Context {
	return context.WithValue(ctx, traceKey, trace)
}

// WithTraceRequest returns a copy of ctx that controls request tracing for the
// associated request. When trace is true, the full request is included in logs;
// when false, request tracing is disabled unless [WithTrace] enables it. Requests
// without an explicit value defer to transport configuration and [WithTrace].
// Child contexts override parent values.
func WithTraceRequest(ctx context.Context, trace bool) context.Context {
	return context.WithValue(ctx, traceRequestKey, trace)
}

// WithTraceResponse returns a copy of ctx that controls response tracing for the
// associated request. When trace is true, the full response is included in logs;
// when false, response tracing is disabled unless [WithTrace] enables it. Requests
// without an explicit value defer to transport configuration and [WithTrace].
// Child contexts override parent values.
func WithTraceResponse(ctx context.Context, trace bool) context.Context {
	return context.WithValue(ctx, traceResponseKey, trace)
}

func logFromContext(ctx context.Context) bool {
	log, ok := ctx.Value(logKey).(bool)
	if !ok {
		return true
	}
	return log
}

func boolFromContext(ctx context.Context, key contextKey) (bool, bool) {
	v, ok := ctx.Value(key).(bool)
	return v, ok
}
