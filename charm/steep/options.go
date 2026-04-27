// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// DefaultTermWidth is the default terminal width used by program harnesses.
	DefaultTermWidth = 80
	// DefaultTermHeight is the default terminal height used by program harnesses.
	DefaultTermHeight = 24

	// DefaultTimeout is the default timeout used by program harnesses.
	DefaultTimeout = 2 * time.Second
	// DefaultCheckInterval is the default check interval used by program harnesses.
	DefaultCheckInterval = 20 * time.Millisecond
	// DefaultSettleTimeout is the default settle timeout used by program harnesses.
	// This must be less than 80% of the timeout set by [WithTimeout].
	DefaultSettleTimeout = 100 * time.Millisecond
)

type options struct {
	width         int
	height        int
	timeout       time.Duration
	checkInterval time.Duration
	programOpts   []tea.ProgramOption

	settleTimeout time.Duration
	settleIgnore  []reflect.Type

	stripANSI bool
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

// Option configures a [Harness] or assertion helper. [NewHarness] and
// [NewComponentHarness] store these options and merge them (before per-call
// options) on any [Harness] method that accepts more options, so a harness can
// be configured with defaults once and overridden per call.
type Option func(*options)

// WithInitialTermSize configures the starting terminal size. See also
// [DefaultTermWidth] and [DefaultTermHeight].
func WithInitialTermSize(width, height int) Option {
	return func(cfg *options) {
		cfg.width = width
		cfg.height = height
	}
}

// WithProgramOptions appends BubbleTea program options to the test program. Note
// that the following options are always set (and cannot be overridden):
// - tea.WithInput(nil)
// - tea.WithOutput(buf)
// - tea.WithoutSignals()
// - tea.WithWindowSize(cfg.width, cfg.height)
//
// This is ignored for all but [NewHarness] and [NewComponentHarness].
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(cfg *options) {
		cfg.programOpts = append(cfg.programOpts, opts...)
	}
}

// WithTimeout configures how long waits may run.
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.timeout = timeout
	}
}

// WithCheckInterval configures how often waits are checked.
func WithCheckInterval(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithSettleTimeout configures how long to wait for the program to settle. See
// [Harness.WaitSettleMessages], [Harness.WaitSettleView] and [WaitSettleView] for
// more details.
func WithSettleTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.settleTimeout = timeout
	}
}

// WithSettleIgnoreMsgs marks message types that do not reset the quiet period
// used by [Harness.WaitSettleMessages]. Pass a value of each type to ignore (for
// example a zero value of your periodic tick type). The dynamic type of each
// observed message is compared with [reflect.TypeOf] on those samples; nil
// samples are skipped.
func WithSettleIgnoreMsgs(types ...any) Option {
	return func(cfg *options) {
		for _, s := range types {
			if s == nil {
				continue
			}
			cfg.settleIgnore = append(cfg.settleIgnore, reflect.TypeOf(s))
		}
	}
}

// WithStripANSI strips ANSI escape and control sequences from view text before
// string/regex [WaitView] and [AssertString] (and related) comparisons,
// and from [Dimensions] for layout assertions. When set on a [NewHarness] or
// [NewComponentHarness], it is also applied to [Harness.AssertSnapshot] and
// [Harness.RequireSnapshot] on that harness.
func WithStripANSI() Option {
	return func(cfg *options) {
		cfg.stripANSI = true
	}
}
