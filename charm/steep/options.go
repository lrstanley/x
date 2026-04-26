// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// DefaultTermWidth is the default terminal width used by program harnesses.
	DefaultTermWidth = 80
	// DefaultTermHeight is the default terminal height used by program harnesses.
	DefaultTermHeight = 24

	// DefaultTimeout is the default timeout used by program harnesses.
	DefaultTimeout = 1 * time.Second
	// DefaultCheckInterval is the default check interval used by program harnesses.
	DefaultCheckInterval = 20 * time.Millisecond
	// DefaultSettleTimeout is the default settle timeout used by program harnesses.
	// This must be less than 80% of the timeout set by [WithTimeout].
	DefaultSettleTimeout = 200 * time.Millisecond
)

type options struct {
	width         int
	height        int
	timeout       time.Duration
	checkInterval time.Duration
	settleTimeout time.Duration
	programOpts   []tea.ProgramOption
}

func defaultOptions() options {
	return options{
		width:         DefaultTermWidth,
		height:        DefaultTermHeight,
		timeout:       DefaultTimeout,
		checkInterval: DefaultCheckInterval,
		settleTimeout: DefaultSettleTimeout,
	}
}

func collectOptions(opts ...Option) options {
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.checkInterval <= 0 {
		cfg.checkInterval = DefaultCheckInterval
	}
	if cfg.timeout <= 0 {
		cfg.timeout = DefaultTimeout
	}
	if cfg.settleTimeout <= 0 {
		cfg.settleTimeout = DefaultSettleTimeout
	}
	if maxSettleTimeout := cfg.timeout * 8 / 10; cfg.settleTimeout > maxSettleTimeout {
		cfg.settleTimeout = maxSettleTimeout
	}
	return cfg
}

// Option configures a test model or assertion.
type Option func(*options)

// WithInitialTermSize configures the starting terminal size.
func WithInitialTermSize(width, height int) Option {
	return func(cfg *options) {
		cfg.width = width
		cfg.height = height
	}
}

// WithProgramOptions appends Bubble Tea program options to the test program.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(cfg *options) {
		cfg.programOpts = append(cfg.programOpts, opts...)
	}
}

// WithTimeout configures how long waits and expectations may run.
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.timeout = timeout
	}
}

// WithCheckInterval configures how often waits and expectations are checked.
func WithCheckInterval(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithSettleTimeout configures how long to wait for the program to settle.
func WithSettleTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.settleTimeout = timeout
	}
}
