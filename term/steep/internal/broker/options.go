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

// WithMaxHistory caps how many past events the broker retains, or removes
// the cap when n is 0 or less. Defaults to [DefaultMaxHistory].
func WithMaxHistory(n int) Option {
	return func(o *options) {
		o.maxHistory = n
	}
}

// WithSubscriberBuffer sets the capacity of each subscriber queue, past which
// it drops events. Defaults to [DefaultSubBufferSize] when less than 1.
func WithSubscriberBuffer(size int) Option {
	return func(o *options) {
		o.subBuffer = size
	}
}

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
