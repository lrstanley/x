// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpcconc

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// funcRoundTripper adapts a function to [http.RoundTripper] for tests.
type funcRoundTripper func(*http.Request) (*http.Response, error)

func (f funcRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewTransport_nilBaseUsesDefaultTransport(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(2, nil)
	req, err := http.NewRequest(http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestNewTransport_maxConcurrentClamped(t *testing.T) {
	t.Parallel()

	var maxSeen atomic.Int32
	var inFlight atomic.Int32
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for {
			old := maxSeen.Load()
			if n <= old || maxSeen.CompareAndSwap(old, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// maxConcurrent 0 must clamp to 1 concurrent request.
	tr := NewTransport(0, http.DefaultTransport)
	client := &http.Client{Transport: tr}

	const nReq = 5
	var wg sync.WaitGroup
	wg.Add(nReq)
	for range nReq {
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodGet, srv.URL, http.NoBody)
			if err != nil {
				t.Error(err)
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = resp.Body.Close()
		}()
	}

	deadline := time.After(5 * time.Second)
	for {
		if maxSeen.Load() >= 1 && inFlight.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for requests; maxSeen=%d inFlight=%d", maxSeen.Load(), inFlight.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if max := maxSeen.Load(); max != 1 {
		t.Fatalf("max concurrent in handler = %d, want 1", max)
	}
	close(release)
	wg.Wait()
}

func TestNewTransport_limitsConcurrentRequests(t *testing.T) {
	t.Parallel()

	const maxConcurrent = 3

	var maxSeen atomic.Int32
	var inFlight atomic.Int32
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for {
			old := maxSeen.Load()
			if n <= old || maxSeen.CompareAndSwap(old, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(maxConcurrent, http.DefaultTransport)
	client := &http.Client{Transport: tr}

	const nReq = 10
	var wg sync.WaitGroup
	wg.Add(nReq)
	for range nReq {
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodGet, srv.URL, http.NoBody)
			if err != nil {
				t.Error(err)
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = resp.Body.Close()
		}()
	}

	deadline := time.After(5 * time.Second)
	for maxSeen.Load() < maxConcurrent {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for saturation; maxSeen=%d inFlight=%d", maxSeen.Load(), inFlight.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if max := maxSeen.Load(); max != int32(maxConcurrent) {
		t.Fatalf("max concurrent in handler = %d, want %d", max, maxConcurrent)
	}
	close(release)
	wg.Wait()
}

func TestRoundTrip_releasesSemaphoreOnError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	base := funcRoundTripper(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("boom")
	})

	tr := NewTransport(1, base)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)

	_, err := tr.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error from base transport")
	}
	_, err = tr.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error from base transport")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("base RoundTrip calls = %d, want 2 (second call must not block)", got)
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(4, nil)
	if c.Timeout != 60*time.Second {
		t.Fatalf("Timeout = %v, want 60s", c.Timeout)
	}
	if c.Transport == nil {
		t.Fatal("Transport is nil")
	}

	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}
