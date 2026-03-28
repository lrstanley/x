// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpclog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfigValidate_NilReceiver(t *testing.T) {
	t.Parallel()
	err := (*Config)(nil).Validate()
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestConfigValidate_Defaults(t *testing.T) {
	t.Parallel()
	c := &Config{}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Level == nil || *c.Level != slog.LevelDebug {
		t.Errorf("Level = %v, want debug", c.Level)
	}
	if c.Logger == nil {
		t.Error("Logger should default to slog.Default")
	}
	if c.BaseTransport == nil {
		t.Error("BaseTransport should default to http.DefaultTransport")
	}
	if len(c.Headers) == 0 {
		t.Error("Headers should get default allowlist when empty")
	}
}

func TestConfigValidate_HTTPTraceEnv(t *testing.T) {
	t.Setenv("HTTP_TRACE", "true")

	c := &Config{}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !c.Trace {
		t.Error("expected Trace true when HTTP_TRACE is set")
	}
}

func TestConfigValidate_DisableEnvTrace(t *testing.T) {
	t.Setenv("HTTP_TRACE", "true")

	c := &Config{DisableEnvTrace: true}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Trace {
		t.Error("expected Trace false when DisableEnvTrace is set")
	}
}

func TestConfigValidate_StarHeadersMeansAll(t *testing.T) {
	t.Parallel()
	c := &Config{Headers: []string{"*"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Headers != nil {
		t.Errorf("Headers after * should be nil (log all), got %#v", c.Headers)
	}
}

func TestConfigValidate_CustomHeadersCanonicalized(t *testing.T) {
	t.Parallel()
	c := &Config{Headers: []string{"x-foo"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(c.Headers) != 1 || c.Headers[0] != "X-Foo" {
		t.Errorf("Headers = %#v, want canonical X-Foo", c.Headers)
	}
}

func newTestLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(h), &buf
}

func TestRoundTrip_LogsRequestAndResponse(t *testing.T) {
	t.Parallel()
	logger, buf := newTestLogger(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(&Config{
		Logger:        logger,
		BaseTransport: http.DefaultTransport,
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "httpclog-test/1")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	out := buf.String()
	if !strings.Contains(out, `"msg":"http request"`) {
		t.Errorf("log should contain request line; got %q", out)
	}
	if !strings.Contains(out, `"msg":"http response"`) {
		t.Errorf("log should contain response line; got %q", out)
	}
	if !strings.Contains(out, `"method":"GET"`) {
		t.Errorf("log should contain method; got %q", out)
	}
	if !strings.Contains(out, `"status":200`) {
		t.Errorf("log should contain status 200; got %q", out)
	}
}

func TestRoundTrip_LogsError(t *testing.T) {
	t.Parallel()
	logger, buf := newTestLogger(t)

	tr := NewTransport(&Config{
		Logger: logger,
		BaseTransport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("roundtrip failed")
		}),
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.invalid/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tr.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error from base transport")
	}

	out := buf.String()
	if !strings.Contains(out, `"msg":"http request failed"`) {
		t.Errorf("log should contain failure line; got %q", out)
	}
	if !strings.Contains(out, `"error":"roundtrip failed"`) {
		t.Errorf("log should contain error text; got %q", out)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRoundTrip_TraceRequestIncludesDump(t *testing.T) {
	t.Parallel()
	logger, buf := newTestLogger(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(&Config{
		Logger:        logger,
		BaseTransport: http.DefaultTransport,
		TraceRequest:  true,
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	out := buf.String()
	if !strings.Contains(out, `"request":"GET `) {
		t.Errorf("trace should include dumped request; got %q", out)
	}
}

func TestRoundTrip_TraceResponseIncludesDump(t *testing.T) {
	t.Parallel()
	logger, buf := newTestLogger(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("tea"))
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(&Config{
		Logger:        logger,
		BaseTransport: http.DefaultTransport,
		TraceResponse: true,
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	out := buf.String()
	if !strings.Contains(out, "HTTP/1.1 418") || !strings.Contains(out, "I'm a teapot") {
		t.Errorf("trace should include dumped response; got %q", out)
	}
}

func TestRoundTrip_HeaderFilter(t *testing.T) {
	t.Parallel()
	logger, buf := newTestLogger(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Secret", "nope")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(&Config{
		Logger:        logger,
		BaseTransport: http.DefaultTransport,
		Headers:       []string{"Content-Type"},
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	out := buf.String()
	if !strings.Contains(out, `"Content-Type":"text/plain"`) {
		t.Errorf("expected Content-Type in headers group; got %q", out)
	}
	if strings.Contains(out, "X-Secret") {
		t.Errorf("did not expect X-Secret when filtered; got %q", out)
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()
	logger, _ := newTestLogger(t)

	c := NewClient(&Config{Logger: logger})
	if c == nil {
		t.Fatal("client is nil")
	}
	if c.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", c.Timeout)
	}
	if c.Transport == nil {
		t.Fatal("Transport is nil")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
