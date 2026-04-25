// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	defaultWidth         = 80
	defaultHeight        = 24
	defaultTimeout       = 3 * time.Second
	defaultCheckInterval = 10 * time.Millisecond
	defaultCommandLimit  = 100
)

type options struct {
	width         int
	height        int
	timeout       time.Duration
	checkInterval time.Duration
	programOpts   []tea.ProgramOption
	commandLimit  int
}

func defaultOptions() options {
	return options{
		width:         defaultWidth,
		height:        defaultHeight,
		timeout:       defaultTimeout,
		checkInterval: defaultCheckInterval,
		commandLimit:  defaultCommandLimit,
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
		cfg.checkInterval = defaultCheckInterval
	}
	if cfg.timeout <= 0 {
		cfg.timeout = defaultTimeout
	}
	if cfg.commandLimit <= 0 {
		cfg.commandLimit = defaultCommandLimit
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

// WithFinalTimeout configures how long final waits may run.
func WithFinalTimeout(timeout time.Duration) Option {
	return WithTimeout(timeout)
}

// WithCheckInterval configures how often waits and expectations are checked.
func WithCheckInterval(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithCommandLimit configures the maximum number of command messages processed
// by generic view models from a single Send call.
func WithCommandLimit(limit int) Option {
	return func(cfg *options) {
		cfg.commandLimit = limit
	}
}
