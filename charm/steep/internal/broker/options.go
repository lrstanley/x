// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package broker

const (
	// DefaultMaxHistory is the default number of past events retained by a broker.
	DefaultMaxHistory = 1024
	// DefaultSubBufferSize is the default capacity of each subscriber queue.
	DefaultSubBufferSize = 32
)

type options struct {
	maxHistory int
	subBuffer  int
}

type Option func(*options)

// WithMaxHistory caps how many past events the broker retains. When n is 0 or
// less, history grows without a cap (bounded only by available memory).
// Defaults to [DefaultMaxHistory] when unspecified.
func WithMaxHistory(n int) Option {
	return func(o *options) {
		o.maxHistory = n
	}
}

// WithSubscriberBuffer sets the capacity of each subscriber queue. When full,
// newer events for that subscriber are dropped so Publish stays non-blocking.
// Defaults to [DefaultSubBufferSize] when unspecified or less than 1.
func WithSubscriberBuffer(size int) Option {
	return func(o *options) {
		o.subBuffer = size
	}
}

// resolveOptions applies opts over broker defaults and normalizes subBuffer.
func resolveOptions(opts []Option) options {
	o := options{
		maxHistory: DefaultMaxHistory,
		subBuffer:  DefaultSubBufferSize,
	}
	for _, fn := range opts {
		fn(&o)
	}
	if o.subBuffer < 1 {
		o.subBuffer = DefaultSubBufferSize
	}
	if o.maxHistory < 0 {
		o.maxHistory = 0
	}
	return o
}
