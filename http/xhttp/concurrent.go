// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package xhttp

import "net/http"

// ConcurrentLimiter is a [net/http.RoundTripper] that limits the number of concurrent
// requests. It wraps another [net/http.RoundTripper] and ensures that only a maximum
// number of requests can be processed simultaneously, while allowing unlimited
// goroutines to queue up.
type ConcurrentLimiter struct {
	// The underlying RoundTripper to delegate requests to.
	Transport http.RoundTripper
	// Semaphore to limit concurrent requests.
	semaphore chan struct{}
}

// NewConcurrentLimiter creates a new [ConcurrentLimiter] with the specified maximum
// number of concurrent requests. If transport is nil, [net/http.DefaultTransport]
// is used.
func NewConcurrentLimiter(maxConcurrent int, transport http.RoundTripper) *ConcurrentLimiter {
	if transport == nil {
		transport = http.DefaultTransport
	}

	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	return &ConcurrentLimiter{
		Transport: transport,
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

// RoundTrip implements [net/http.RoundTripper] interface. It acquires a semaphore slot
// before making the request and releases it after the request completes.
func (cl *ConcurrentLimiter) RoundTrip(req *http.Request) (*http.Response, error) {
	// Acquire a semaphore slot to limit concurrent requests.
	cl.semaphore <- struct{}{}
	defer func() {
		// Release the semaphore slot when the request completes.
		<-cl.semaphore
	}()

	// Delegate the actual request to the underlying transport.
	return cl.Transport.RoundTrip(req)
}
