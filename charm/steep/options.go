// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// DefaultTermWidth is the default terminal width for [NewHarness] and
	// [NewComponentHarness] (the [tea.Program] window and vt emulator).
	DefaultTermWidth = 80
	// DefaultTermHeight is the default terminal height for [NewHarness] and
	// [NewComponentHarness] (the [tea.Program] window and vt emulator).
	DefaultTermHeight = 24

	// DefaultTimeout is the default timeout used by [Harness] blocking waits.
	DefaultTimeout = 2 * time.Second
	// DefaultCheckInterval is the default polling interval used by [Harness] waits.
	DefaultCheckInterval = 10 * time.Millisecond
	// DefaultSettleTimeout is the default settle timeout used by [Harness] settle waits.
	// This must be less than 80% of the timeout set by [WithTimeout].
	DefaultSettleTimeout = 100 * time.Millisecond
)

type options struct {
	// width is the initial terminal width in cells for the [vt.Emulator] and
	// [tea.Program].
	width int
	// height is the initial terminal height in cells for the [vt.Emulator] and
	// [tea.Program].
	height int
	// timeout caps how long blocking waits (assertions, startup, settle,
	// mutation) may run.
	timeout time.Duration
	// checkInterval is how often polling waits re-check their condition until
	// timeout.
	checkInterval time.Duration
	// programOpts are extra [tea.ProgramOption] values merged when constructing
	// the [tea.Program].
	programOpts []tea.ProgramOption

	// settleTimeout is the minimum quiet period with no relevant activity before
	// settle waits succeed.
	settleTimeout time.Duration
	// settleIgnore lists message types that do not reset the quiet period for
	// [Harness.WaitSettleMessages].
	settleIgnore []reflect.Type

	// stripANSI removes ANSI sequences from view text and dimensions used in
	// comparisons and snapshots.
	stripANSI bool

	// reason is an optional, propagated reason for an error or failure.
	reason string
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

func (o *options) Errorf(tb testing.TB, format string, args ...any) {
	tb.Helper()
	if o.reason != "" {
		format = o.reason + ": " + format
	}
	tb.Errorf(format, args...)
}

func (o *options) Fatalf(tb testing.TB, format string, args ...any) {
	tb.Helper()
	if o.reason != "" {
		format = o.reason + ": " + format
	}
	tb.Fatalf(format, args...)
}

// Option configures a [Harness] or assertion helper. [NewHarness] and
// [NewComponentHarness] store these options and merge them (before per-call
// options) on any [Harness] method that accepts more options, so a harness can
// be configured with defaults once and overridden per call.
type Option func(*options)

// withReason sets an optional, propagated reason for an error or failure.
func withReason(format string, args ...any) Option {
	return func(cfg *options) {
		if len(args) > 0 {
			cfg.reason = fmt.Sprintf(format, args...)
		} else {
			cfg.reason = format
		}
	}
}

// WithWindowSize configures the starting terminal size. See also
// [DefaultTermWidth] and [DefaultTermHeight].
func WithWindowSize(width, height int) Option {
	return func(cfg *options) {
		cfg.width = width
		cfg.height = height
	}
}

// WithProgramOptions appends [tea.ProgramOption] values for [NewHarness] and
// [NewComponentHarness]. The harness always sets environment, context, input and
// output (via the internal vt emulator), [tea.WithoutSignals], and [tea.WithWindowSize];
// those cannot be replaced by options passed here. It has no effect elsewhere.
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

// WithSettleTimeout configures how long to wait for the [tea.Program] to satisfy
// settle waits. See [Harness.WaitSettleMessages], [Harness.WaitSettle] and
// [WaitSettle] for more details.
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
// string/regex [WaitViewFunc] and [AssertString] (and related) comparisons,
// and from [Dimensions] for layout assertions. When set on a [NewHarness] or
// [NewComponentHarness], it is also applied to [Harness.AssertViewSnapshot]
// and [Harness.RequireViewSnapshot] on that harness.
func WithStripANSI() Option {
	return func(cfg *options) {
		cfg.stripANSI = true
	}
}
