// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package xhttp

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"runtime"
	"slices"
	"strings"
	"time"
)

// LoggerConfig is the configuration for the logger transport.
type LoggerConfig struct {
	// Level is the log level to use. Defaults to [log/slog.LevelDebug], which
	// means that the logger will only be invoked if the provided [LoggerConfig.Logger]
	// is enabled for the [log/slog.LevelDebug] level. Note that response errors will
	// always be logged at the [log/slog.LevelError] level, regardless of this setting.
	Level *slog.Level

	// Logger is the logger to use. Defaults to [slog.Default].
	Logger *slog.Logger

	// BaseTransport is the base transport to use (will be chained). Defaults to
	// [net/http.DefaultTransport], which allows for connection reuse, HTTP proxy
	// support, etc.
	BaseTransport http.RoundTripper

	// Headers is a list of request or response headers to log. If not "*". some
	// sane defaults are used that are non-sensitive. When tracing is enabled, all
	// headers would be logged as part of that slog attribute, regardless of this
	// setting.
	Headers []string

	// Trace will enable full request/response tracing (e.g. request/response bodies,
	// full headers, etc). This overrides all other trace settings. If you only want
	// to trace things when a user explicitly desires it, and not based on logging
	// level, use something like:
	//
	//	&xhttp.LoggerConfig{Trace: strconv.ParseBool(os.Getenv("HTTP_TRACE")), [...]}
	Trace bool

	// TraceRequest will enable request tracing. This overrides
	// [LoggerConfig.TraceRequestFunc].
	TraceRequest bool

	// TraceRequestFunc is a function that determines whether to trace the request.
	TraceRequestFunc func(req *http.Request) bool

	// TraceResponse will enable response tracing. This overrides
	// [LoggerConfig.TraceResponseFunc].
	TraceResponse bool

	// TraceResponseFunc is a function that determines whether to trace the response.
	TraceResponseFunc func(resp *http.Response) bool
}

// Validate validates the logger configuration. Use this to validate the configuration,
// before passing it to [NewLoggerTransport] or [NewLoggerClient], as they will panic
// if the configuration is invalid.
func (c *LoggerConfig) Validate() error {
	if c == nil {
		return errors.New("config cannot be nil")
	}

	if c.Level == nil {
		level := slog.LevelDebug
		c.Level = &level
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	if c.BaseTransport == nil {
		c.BaseTransport = http.DefaultTransport
	}

	if len(c.Headers) == 1 && c.Headers[0] == "*" {
		c.Headers = nil
	} else if len(c.Headers) == 0 {
		// Some sane headers to log, for both requests and responses.
		c.Headers = []string{
			"Content-Type",
			"Content-Length",
			"User-Agent",
			"Referer",
			"Origin",
			"Host",
			"Accept",
			"Accept-Encoding",
			"Connection",
			"Upgrade-Insecure-Requests",
			"Sec-Fetch-Dest",
			"Sec-Fetch-Mode",
			"Sec-Fetch-Site",
			"Sec-Fetch-User",
			"Sec-Fetch-Header",
		}
	}

	for i := range c.Headers {
		c.Headers[i] = http.CanonicalHeaderKey(c.Headers[i])
	}

	return nil
}

type logger struct {
	config *LoggerConfig
}

// NewLoggerTransport creates a new [net/http.RoundTripper] that logs requests and
// responses. See also [NewLoggerClient]. This will panic if the configuration is
// invalid, which can be avoided by using [LoggerConfig.Validate] first.
func NewLoggerTransport(config *LoggerConfig) http.RoundTripper {
	if config == nil {
		config = &LoggerConfig{}
	}
	err := config.Validate()
	if err != nil {
		panic(err)
	}
	return &logger{config: config}
}

// NewLoggerClient creates a new [http.Client] that logs requests and responses.
// See also [NewLoggerTransport]. The default timeout is 60 seconds. This will panic
// if the configuration is invalid, which can be avoided by using [LoggerConfig.Validate]
// first.
func NewLoggerClient(config *LoggerConfig) *http.Client {
	if config == nil {
		config = &LoggerConfig{}
	}
	err := config.Validate()
	if err != nil {
		panic(err)
	}
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: NewLoggerTransport(config),
	}
}

func (l *logger) shouldTraceRequest(req *http.Request) bool {
	if l.config.Trace || l.config.TraceRequest {
		return true
	}
	if l.config.TraceRequestFunc != nil {
		return l.config.TraceRequestFunc(req)
	}
	return false
}

func (l *logger) shouldTraceResponse(resp *http.Response) bool {
	if l.config.Trace || l.config.TraceResponse {
		return true
	}
	if l.config.TraceResponseFunc != nil {
		return l.config.TraceResponseFunc(resp)
	}
	return false
}

func (rt *logger) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	handler := rt.config.Logger.Handler()

	var r slog.Record

	var pcs [1]uintptr
	_ = runtime.Callers(6, pcs[:]) // Skip this, and all of the net/http/client functions.

	if handler.Enabled(ctx, *rt.config.Level) {
		r = slog.NewRecord(time.Now(), *rt.config.Level, "http request", pcs[0])

		r.AddAttrs(
			slog.String("method", req.Method),
			slog.String("url", req.URL.String()),
			slog.String("user-agent", req.UserAgent()),
			slog.Int64("content-length", req.ContentLength),
			slog.GroupAttrs("headers", rt.headersAsAttrs(req.Header)...),
		)

		if rt.shouldTraceRequest(req) {
			b, err := httputil.DumpRequest(req, true)
			if err == nil {
				r.AddAttrs(slog.String("request", string(b)))
			}
		}

		_ = handler.Handle(ctx, r)
	}

	started := time.Now()
	resp, err := rt.config.BaseTransport.RoundTrip(req)
	duration := time.Since(started)

	if err != nil {
		if handler.Enabled(ctx, slog.LevelError) {
			r = slog.NewRecord(time.Now(), slog.LevelError, "http request failed", pcs[0])
			r.AddAttrs(
				slog.String("url", req.URL.String()),
				slog.String("error", err.Error()),
				slog.Duration("duration", duration),
			)

			if resp != nil && rt.shouldTraceResponse(resp) {
				var b []byte
				b, err = httputil.DumpResponse(resp, true)
				if err == nil {
					r.AddAttrs(slog.String("response", string(b)))
				}
			}

			_ = handler.Handle(ctx, r)
		}
		return nil, err
	}

	if handler.Enabled(ctx, *rt.config.Level) {
		r = slog.NewRecord(time.Now(), *rt.config.Level, "http response", pcs[0])
		r.AddAttrs(
			slog.String("url", req.URL.String()),
			slog.Int("status", resp.StatusCode),
			slog.Duration("duration", duration),
			slog.Int64("content-length", resp.ContentLength),
			slog.GroupAttrs("headers", rt.headersAsAttrs(resp.Header)...),
		)

		if rt.shouldTraceResponse(resp) {
			var b []byte
			b, err = httputil.DumpResponse(resp, true)
			if err == nil {
				r.AddAttrs(slog.String("response", string(b)))
			}
		}

		_ = handler.Handle(ctx, r)
	}

	return resp, nil
}

func (l *logger) headersAsAttrs(headers http.Header) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(headers))
	for k, v := range headers {
		if len(l.config.Headers) > 0 && !slices.Contains(l.config.Headers, k) {
			continue
		}
		attrs = append(attrs, slog.String(k, strings.Join(v, ", ")))
	}
	return attrs
}
