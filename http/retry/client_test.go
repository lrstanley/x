// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package retry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func ExampleNewClient() { //nolint:testableexamples
	client := NewClient(&Config{
		// All of these settings are optional.
		MaxRetries:           3,
		MaxBackoff:           2 * time.Minute,
		MaxRateLimitDuration: 5 * time.Minute,
		RetryCallback: func(_ context.Context, retries int, backoff time.Duration, req *http.Request, _ *http.Response, _ error) {
			// Log the retry attempt.
			fmt.Printf("retrying request %s: attempt %d, backoff %s\n", req.URL, retries, backoff)
		},
	})

	// Make a request.
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", http.NoBody)
	if err != nil {
		panic(err)
	}

	// Make a request with the client, or pass the client to some other library.
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	// Do things with the response here.
	// [...]
	fmt.Printf("response status: %s\n", resp.Status)
}

func TestParseRetryAfterHeader(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		backoff time.Duration
		ok      bool
	}{
		{name: "empty-header", headers: nil, backoff: 0, ok: false},
		{name: "empty-header-2", headers: []string{""}, backoff: 0, ok: false},
		{name: "invalid-header", headers: []string{"invalid"}, backoff: 0, ok: false},
		{name: "valid-seconds", headers: []string{"10"}, backoff: 10 * time.Second, ok: true},
		{name: "valid-seconds-2", headers: []string{"60"}, backoff: 60 * time.Second, ok: true},
		{name: "negative-seconds", headers: []string{"-10"}, backoff: 0, ok: false},
		{name: "valid-rfc1123", headers: []string{time.Now().Add(30 * time.Second).Format(time.RFC1123)}, backoff: 30 * time.Second, ok: true},
		{name: "negative-rfc1123", headers: []string{time.Now().Add(-30 * time.Second).Format(time.RFC1123)}, backoff: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff, ok := parseRetryAfterHeader(tt.headers)

			// Compare backoff and tt.backoff with a tolerance of 1 second.
			if tt.backoff != 0 && backoff != 0 {
				if backoff < tt.backoff-1*time.Second || backoff > tt.backoff+1*time.Second {
					t.Errorf("parseRetryAfterHeader() backoff = %v, want %v", backoff, tt.backoff)
				}
			} else if tt.backoff != 0 && backoff == 0 {
				t.Errorf("parseRetryAfterHeader() backoff = %v, want %v", backoff, tt.backoff)
			} else if backoff != 0 && tt.backoff == 0 {
				t.Errorf("parseRetryAfterHeader() backoff = %v, want %v", backoff, tt.backoff)
			}

			if ok != tt.ok {
				t.Errorf("parseRetryAfterHeader() ok = %v, want %v", ok, tt.ok)
			}
		})
	}
}

func hstatus(t *testing.T, code int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}
}

func hempty(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			panic("response writier does not support hijacking")
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			panic(fmt.Sprintf("failed to hijack connection: %v", err))
		}
		_ = bufrw.Flush()
		_ = conn.Close()
	}
}

func hratelimit(t *testing.T, wait time.Duration) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
		w.WriteHeader(http.StatusTooManyRequests)
	}
}

// mockServer creates a mock HTTP server that calls the provided handlers in order as requests are received.
// If overflow is true, it will not panic if there are more requests than handlers, though all subsequent
// requests will return a 500 status code.
func mockServer(t *testing.T, handlers []http.HandlerFunc, overflow bool) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	var handlerIndex int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count := handlerIndex
		mu.Unlock()

		if count >= len(handlers) {
			if !overflow {
				panic("too many requests to mock server")
			}
			t.Log("overflowed mock server")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		t.Logf("calling handler @ index %d", count)

		handlers[count](w, r)

		mu.Lock()
		handlerIndex++
		mu.Unlock()
	}))

	t.Cleanup(func() {
		srv.Close()

		mu.Lock()
		count := handlerIndex
		mu.Unlock()
		if count < len(handlers) {
			t.Errorf("expected %d handlers to be called, got %d", len(handlers), count)
		}
	})
	return srv
}

func TestNewTransport(t *testing.T) {
	t.Parallel()
	// TODO: change this to use testing/synctest in the future once it's released (experimental in Go 1.24)
	// to make these tests WAY faster.
	tests := []struct {
		name     string
		handlers []http.HandlerFunc
		overflow bool
		config   *Config
		err      bool
		min      time.Duration
		max      time.Duration
	}{
		{
			name:     "success-0",
			handlers: []http.HandlerFunc{hstatus(t, http.StatusOK)},
			err:      false,
			max:      500 * time.Millisecond,
		},
		{
			name:     "success-after-1-empty",
			handlers: []http.HandlerFunc{hempty(t), hstatus(t, http.StatusOK)},
			err:      false,
			min:      1 * time.Second,
			max:      2 * time.Second,
		},
		{
			name:     "success-after-2-mixed",
			handlers: []http.HandlerFunc{hempty(t), hstatus(t, http.StatusInternalServerError), hstatus(t, http.StatusOK)},
			err:      false,
			min:      2 * time.Second,
			max:      5 * time.Second,
		},
		{
			name:     "success-after-2-500",
			handlers: []http.HandlerFunc{hstatus(t, http.StatusInternalServerError), hstatus(t, http.StatusInternalServerError), hstatus(t, http.StatusOK)},
			err:      false,
			min:      2 * time.Second,
			max:      5 * time.Second,
		},
		{
			name: "success-after-4-500",
			handlers: []http.HandlerFunc{
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusOK),
			},
			err: false,
			min: 14 * time.Second,
			max: 16 * time.Second,
		},
		{
			name: "success-after-4-500-502",
			handlers: []http.HandlerFunc{
				hstatus(t, http.StatusBadGateway),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusBadGateway),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusOK),
			},
			err: false,
			min: 14 * time.Second,
			max: 16 * time.Second,
		},
		{
			name: "success-after-1-429",
			handlers: []http.HandlerFunc{
				hratelimit(t, 5*time.Second),
				hstatus(t, http.StatusOK),
			},
			err: false,
			min: 5 * time.Second,
			max: 6 * time.Second,
		},
		{
			name: "success-after-4-429",
			handlers: []http.HandlerFunc{
				hratelimit(t, 1*time.Second),
				hratelimit(t, 1*time.Second),
				hratelimit(t, 1*time.Second),
				hratelimit(t, 1*time.Second),
				hstatus(t, http.StatusOK),
			},
			err: false,
			min: 4 * time.Second,
			max: 5 * time.Second,
		},
		{
			name: "fail-after-5-500",
			handlers: []http.HandlerFunc{
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
				hstatus(t, http.StatusInternalServerError),
			},
			err: true,
			min: 14 * time.Second,
			max: 16 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.config == nil {
				tt.config = &Config{}
			}
			err := tt.config.Validate()
			if err != nil {
				t.Fatalf("failed to validate config: %v", err)
			}

			tt.config.RetryCallback = func(_ context.Context, retries int, backoff time.Duration, _ *http.Request, resp *http.Response, err error) {
				if resp != nil {
					t.Logf("got response status: %d", resp.StatusCode)
				} else {
					t.Logf("got response error: %v", err)
				}
				t.Logf("retry attempt %d @ backoff %s", retries, backoff)
			}

			srv := mockServer(t, tt.handlers, tt.overflow)

			client := &http.Client{
				Transport: NewTransport(tt.config),
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			start := time.Now()
			resp, err := client.Do(req)
			took := time.Since(start)
			if resp != nil {
				t.Logf("got response status: %d", resp.StatusCode)
				if err == nil && resp.StatusCode >= 400 {
					err = fmt.Errorf("response status code %d", resp.StatusCode)
				}
			}
			if (err != nil) != tt.err {
				t.Errorf("expected error: %v, got: %v", tt.err, err)
				return
			}
			if resp != nil {
				resp.Body.Close()
			}

			if tt.min != 0 && took < tt.min {
				t.Errorf("expected request to take at least %s time, actually took %s", tt.min, took)
			}
			if tt.max != 0 && took > tt.max {
				t.Errorf("expected request to take at most %s time, actually took %s", tt.max, took)
			}
		})
	}
}
