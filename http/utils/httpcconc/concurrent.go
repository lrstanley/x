// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpcconc

import "net/http"

type transport struct {
	base      http.RoundTripper // The underlying [net/http.RoundTripper] to delegate requests to.
	semaphore chan struct{}     // The semaphore to limit concurrent requests.
}

// NewTransport returns a [net/http.RoundTripper] that limits the number of
// concurrent requests. It wraps another [net/http.RoundTripper] and ensures that
// only a maximum number of requests can be processed simultaneously, while
// allowing unlimited goroutines to queue up.
func NewTransport(maxConcurrent int, baseTransport http.RoundTripper) http.RoundTripper {
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}

	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	return &transport{
		base:      baseTransport,
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

// RoundTrip implements [net/http.RoundTripper] interface. It acquires a semaphore slot
// before making the request and releases it after the request completes.
func (cl *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	cl.semaphore <- struct{}{}
	defer func() {
		<-cl.semaphore
	}()
	return cl.base.RoundTrip(req)
}
