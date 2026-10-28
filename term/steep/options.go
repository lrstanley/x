// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/x/term/reel"
)

const (
	// DefaultTermWidth is the default terminal width (see [WithWindowSize]).
	DefaultTermWidth = 80
	// DefaultTermHeight is the default terminal height (see [WithWindowSize]).
	DefaultTermHeight = 24

	// DefaultTimeout is the default timeout used by [Harness] blocking waits.
	DefaultTimeout = 2 * time.Second
	// DefaultCheckInterval is the default polling interval used by [Harness] waits.
	DefaultCheckInterval = 10 * time.Millisecond
	// DefaultSettleTimeout is the default settle timeout used by [Harness] settle
	// waits. Settle timeouts are capped at 80% of the [WithTimeout] timeout.
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

	// scrollOnInput is [WithScrollOnInput].
	scrollOnInput bool

	stripANSI bool

	// reason prefixes failure messages.
	reason string

	// harness is the harness whose timeline waits and assertions record
	// into, or nil to record nothing.
	harness *Harness
	// required marks an assertion as a requirement in its record.
	required bool

	envVars []string
	// color is 0 to leave envVars as they are.
	color ColorProfile

	ctx           context.Context
	wasContextSet bool

	// recording includes the environment's patch.
	recording reel.Profile
	// artifacts is empty for [testing.TB.ArtifactDir].
	artifacts string
	policy    exportPolicy
	stills    map[HookEvent]StillKind

	recordMessages bool

	hooks       []Hook
	ignoreEnv   bool
	slow        time.Duration
	typingDelay time.Duration
	snapshotDir string
}

func defaultOptions() options {
	return options{
		width:         DefaultTermWidth,
		height:        DefaultTermHeight,
		timeout:       DefaultTimeout,
		checkInterval: DefaultCheckInterval,
		settleTimeout: DefaultSettleTimeout,
		scrollOnInput: true,
		envVars: []string{
			// Force TTY mode, since we're not connecting to a real TTY/PTY.
			"TTY_FORCE=1",
			term256,
		},
		ctx: context.Background(),
	}
}

func collectOptions(tb testing.TB, opts ...Option) options {
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	applyEnv(tb, &cfg)
	return cfg.validate()
}

func (o options) validate() options {
	if o.checkInterval <= 0 {
		o.checkInterval = DefaultCheckInterval
	}
	if o.timeout <= 0 {
		o.timeout = DefaultTimeout
	}
	o.typingDelay = max(o.typingDelay, 0)
	if o.settleTimeout <= 0 {
		o.settleTimeout = DefaultSettleTimeout
	}
	if maxSettleTimeout := o.timeout * 8 / 10; o.settleTimeout > maxSettleTimeout {
		o.settleTimeout = maxSettleTimeout
	}

	return o
}

func (o options) Errorf(tb testing.TB, format string, args ...any) {
	tb.Helper()
	if o.reason != "" {
		format = o.reason + ": " + format
	}
	tb.Errorf(format, args...)
}

func (o options) Fatalf(tb testing.TB, format string, args ...any) {
	tb.Helper()
	if o.reason != "" {
		format = o.reason + ": " + format
	}
	tb.Fatalf(format, args...)
}

// Option configures a [Harness] or assertion helper. Options given to
// [NewHarness] are defaults for every [Harness] method that accepts options,
// and per-call options override them.
type Option func(*options)

// withReason sets a prefix for failure messages.
func withReason(format string, args ...any) Option {
	return func(cfg *options) {
		if len(args) > 0 {
			cfg.reason = fmt.Sprintf(format, args...)
		} else {
			cfg.reason = format
		}
	}
}

// withHarness makes waits and assertions record their outcome in h's
// timeline. [NewHarness] adds it to every call's options.
func withHarness(h *Harness) Option {
	return func(cfg *options) {
		cfg.harness = h
	}
}

// withRequired records an assertion as a requirement.
func withRequired() Option {
	return func(cfg *options) {
		cfg.required = true
	}
}

// WithWindowSize configures the starting terminal size.
//
// See also [DefaultTermWidth] and [DefaultTermHeight].
func WithWindowSize(width, height int) Option {
	return func(cfg *options) {
		cfg.width = width
		cfg.height = height
	}
}

// WithContext sets the parent context for blocking waits and the running
// [tea.Program]. A [Harness] defaults to the test's context, canceled on
// interrupt; other helpers default to [context.Background].
func WithContext(ctx context.Context) Option {
	if ctx == nil {
		ctx = context.Background()
	}
	return func(cfg *options) {
		cfg.ctx = ctx
		cfg.wasContextSet = true
	}
}

// WithProgramOptions appends [tea.ProgramOption] values for [NewHarness] and
// [NewComponentHarness]. Environment, context, input, output,
// [tea.WithoutSignals], and [tea.WithWindowSize] are always set by the
// harness and cannot be replaced here.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(cfg *options) {
		cfg.programOpts = append(cfg.programOpts, opts...)
	}
}

// WithTimeout configures the maximum time blocking waits (startup, waits,
// settle, mutation) may run before failing the test. Defaults to
// [DefaultTimeout].
//
// Environment: STEEP_TIMEOUT (overrides this option unless [WithIgnoreEnv] is set).
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.timeout = timeout
	}
}

// WithCheck configures how often polling waits re-check their condition.
// Defaults to [DefaultCheckInterval]. While exporting, it also paces action
// marks (see [Harness.Mark]).
//
// Environment: STEEP_CHECK (overrides this option unless [WithIgnoreEnv] is set).
func WithCheck(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithSettle configures the quiet period settle waits require. Defaults to
// [DefaultSettleTimeout], and is capped at 80% of [WithTimeout]. While
// exporting, it also paces action marks (see [Harness.Mark]).
//
// Environment: STEEP_SETTLE (overrides this option unless [WithIgnoreEnv] is set).
func WithSettle(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.settleTimeout = timeout
	}
}

// WithSettleIgnoreMsgs marks message types that do not reset the quiet period
// used by [Harness.WaitSettleMessages]. Pass a value of each type to ignore,
// such as a zero value of a periodic tick type; types must match exactly.
// Calls accumulate.
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

// WithScrollOnInput determines whether [Harness.Type], [Harness.Key] (and the
// helpers built on it), and [Harness.Paste] first scroll a viewport scrolled
// with [Harness.ScrollUp] and friends back to the bottom, as a real terminal
// does on a key press. Enabled by default. Mouse events and
// [Harness.SendProgram] never scroll.
func WithScrollOnInput(enable bool) Option {
	return func(cfg *options) {
		cfg.scrollOnInput = enable
	}
}

// WithANSI determines whether ANSI sequences are kept (the default) in the
// output that waits, assertions, and [Harness.AssertSnapshot] compare. It does
// not change what images and recordings show.
func WithANSI(enable bool) Option {
	return func(cfg *options) {
		cfg.stripANSI = !enable
	}
}

// WithHooks appends hooks (see [Hook]). Calls accumulate.
func WithHooks(hooks ...Hook) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, hooks...)
	}
}

// WithIgnoreEnv disables STEEP_* environment overrides. By default they are
// applied after every other option, so they override options set in code.
func WithIgnoreEnv(ignore bool) Option {
	return func(cfg *options) {
		cfg.ignoreEnv = ignore
	}
}

// WithSlow makes the harness sleep for d after every hooked action, before
// its After hooks run, giving the program real time between actions for
// animations, ticks, and timers. Recordings do not need it: they play back
// the timeline as it happened, at [reel.Profile.Speed].
//
// Environment: STEEP_SLOW (overrides this option unless [WithIgnoreEnv] is set).
func WithSlow(d time.Duration) Option {
	return func(cfg *options) {
		cfg.slow = d
	}
}

// WithTypingDelay makes [Harness.Type] and [Harness.Key] pause for d after
// each key press (see [reel.WithTypingDelay]), so recordings show text being
// typed. Unlike [WithSlow], it only slows input. Passed to Type or Key, it
// sets the pause for that call.
//
// Environment: STEEP_TYPING_DELAY.
func WithTypingDelay(d time.Duration) Option {
	return func(cfg *options) {
		cfg.typingDelay = d
	}
}

// WithLog logs every hooked action to tb, as [WithHooks] with a
// [LoggerHook] of a nil logger.
//
// Environment: STEEP_LOG (true or 1) adds the same hook unless
// [WithIgnoreEnv] is set.
func WithLog(tb testing.TB) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, LoggerHook(tb, nil))
	}
}

// WithRecordMessages records each message the model receives as a
// "steep.message" annotation of its type and %+v value. It is off by
// default, since programs that tick or animate receive many messages, each of
// which grows the timeline and ".cast" file.
//
// Environment: STEEP_RECORD_MESSAGES (a boolean) overrides this option
// unless [WithIgnoreEnv] is set.
func WithRecordMessages(enable bool) Option {
	return func(cfg *options) {
		cfg.recordMessages = enable
	}
}

// WithEnvVars sets environment variables for the [tea.Program], in the form of
// "key=value". Without this option the program sees only the defaults:
//
//   - TTY_FORCE=1
//   - TERM=xterm-256color (see [WithColor])
//
// This option replaces those defaults with the test process environment plus
// vars, so pass TTY_FORCE and TERM again if the program needs them. It panics
// if an entry is not in "key=value" form.
func WithEnvVars(vars ...string) Option {
	return func(cfg *options) {
		current := map[string]string{}
		for _, entry := range os.Environ() {
			key, value, _ := strings.Cut(entry, "=")
			current[key] = value
		}
		for _, v := range vars {
			key, value, ok := strings.Cut(v, "=")
			if !ok {
				panic(fmt.Errorf("invalid environment variable: %s", v))
			}
			current[key] = value
		}
		cfg.envVars = make([]string, 0, len(current))
		for key, value := range current {
			cfg.envVars = append(cfg.envVars, key+"="+value)
		}
	}
}
