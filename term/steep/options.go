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
	// DefaultSettleTimeout is the default settle timeout used by [Harness] settle
	// waits. This must be less than 80% of the timeout set by [WithTimeout].
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

	// harness is the harness whose timeline waits and assertions record
	// what they found in, or nil to record nothing (see [withHarness]).
	harness *Harness
	// required is whether an assertion is a requirement, for its record
	// (see [withRequired]).
	required bool

	// envVars are extra environment variables to set for the [tea.Program].
	envVars []string
	// color is the color support advertised to the [tea.Program], or 0 to
	// leave envVars as they are.
	color ColorProfile

	// ctx is the parent context for blocking [Wait]-style helpers.
	ctx           context.Context
	wasContextSet bool

	// recording is what an export writes, from [WithRecording], with the
	// environment's patch applied.
	recording reel.Profile
	// artifacts is the directory relative output paths resolve under, or
	// empty for [testing.TB.ArtifactDir].
	artifacts string
	// policy is when the harness exports.
	policy exportPolicy
	// stills are the kinds of still written at each hook event's marks.
	stills map[HookEvent]StillKind

	// recordMessages records each message the model receives in the
	// timeline.
	recordMessages bool

	hooks       []Hook
	ignoreEnv   bool
	slow        time.Duration
	snapshotDir string
}

func defaultOptions() options {
	return options{
		width:         DefaultTermWidth,
		height:        DefaultTermHeight,
		timeout:       DefaultTimeout,
		checkInterval: DefaultCheckInterval,
		settleTimeout: DefaultSettleTimeout,
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

// withHarness has the waits and assertions the options are passed to record
// how they ended, and what they found, in h's timeline. [NewHarness] adds it
// to the options it merges into each call's, so the package's waits and
// assertions record for a harness's methods, and record nothing otherwise.
func withHarness(h *Harness) Option {
	return func(cfg *options) {
		cfg.harness = h
	}
}

// withRequired records the assertion the options are passed to as a
// requirement, which stops the test when it fails, as [Require] does.
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

// WithContext sets the parent context for blocking waits ([Wait],
// [WaitMessageWhere], [Harness.WaitMessageWhere], and other [Harness] wait
// helpers) and for the running [tea.Program]. Defaults to a notify context
// through [signal.NotifyContext] when invoked through [Harness], else
// [context.Background].
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
// [NewComponentHarness]. The harness always sets environment, context (see
// [WithContext]), input and output (via the internal vt emulator),
// [tea.WithoutSignals], and [tea.WithWindowSize]; those cannot be replaced by
// options passed here. It has no effect elsewhere.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(cfg *options) {
		cfg.programOpts = append(cfg.programOpts, opts...)
	}
}

// WithTimeout configures the maximum time blocking waits (assertions, startup,
// settle, mutation) may run before failing the test. The settle timeout is
// capped at 80% of this value; see [WithSettle].
//
// Environment: STEEP_TIMEOUT (overrides this option unless [WithIgnoreEnv] is set).
//
// See also [DefaultTimeout].
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.timeout = timeout
	}
}

// WithCheck configures how often polling waits re-check their condition. While
// an export policy is on, it is also how long the program must stay quiet
// after a paint before an action's mark is placed (see [WithExportAlways]), so
// a very large value slows those actions.
//
// Environment: STEEP_CHECK (overrides this option unless [WithIgnoreEnv] is set).
//
// See also [DefaultCheckInterval].
func WithCheck(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithSettle configures the quiet period the [tea.Program] must stay idle before
// settle waits succeed. See [Harness.WaitSettleMessages], [Harness.WaitSettle]
// and [WaitSettle] for more details. It is capped at 80% of [WithTimeout]. While
// an export policy is on, it also bounds how long an action waits for the
// program to paint (with a 50ms floor) before placing its mark anyway.
//
// Environment: STEEP_SETTLE (overrides this option unless [WithIgnoreEnv] is set).
//
// See also [DefaultSettleTimeout].
func WithSettle(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.settleTimeout = timeout
	}
}

// WithSettleIgnoreMsgs marks message types that do not reset the quiet period
// used by [Harness.WaitSettleMessages]. Pass a value of each type to ignore (for
// example a zero value of your periodic tick type). The dynamic type of each
// observed message is compared with [reflect.TypeOf] on those samples; nil
// samples are skipped. Calls accumulate; there is no default ignore list.
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

// WithANSI determines whether ANSI sequences should be kept. ANSI is kept by
// default. When false, ANSI sequences are stripped, which will often make it
// easier to visually and programmatically compare snapshots, output, etc. This
// applies to string/regex comparisons in [Assert], [AssertMatch], [Wait], and so
// on, and to [Harness.AssertSnapshot]. It does not change what images and
// recordings show.
func WithANSI(enable bool) Option {
	return func(cfg *options) {
		cfg.stripANSI = !enable
	}
}

// WithHooks appends user hooks, which run for each event and stage (see
// [Hook]). A hook that returns an error fails the test immediately. Calls
// accumulate.
func WithHooks(hooks ...Hook) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, hooks...)
	}
}

// WithIgnoreEnv disables STEEP_* environment overrides (defaults to applying
// them). Environment values are applied after every other option, regardless of
// option order, so by default a STEEP_* variable overrides the matching option
// set in code. Use this to pin a test's configuration.
func WithIgnoreEnv(ignore bool) Option {
	return func(cfg *options) {
		cfg.ignoreEnv = ignore
	}
}

// WithSlow makes the harness sleep for d after every hooked action (input,
// terminal, program, wait, ready, close) finishes, before its After hooks
// run. This paces the test so the program has real time to run between actions,
// which is useful for animations, ticks, and timers, or for watching a test
// play out.
//
// It is not required for complete recordings: every paint is in the
// [reel.Timeline], and recordings play it back as it happened. Playback speed
// is [reel.Profile.Speed] (see [WithRecording]). The sleep applies to every
// hooked action, so it slows the whole test, whether or not it exports.
//
// Environment: STEEP_SLOW.
func WithSlow(d time.Duration) Option {
	return func(cfg *options) {
		cfg.slow = d
	}
}

// WithLog is a shorthand for [WithHooks] & [LoggerHook] with a nil logger,
// logging every hooked harness action (before and after) to tb through
// [testing.TB.Log]. Log output only shows for failing tests or with go test -v.
//
// Environment: STEEP_LOG (true or 1) adds the same hook without code changes
// unless [WithIgnoreEnv] is set.
func WithLog(tb testing.TB) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, LoggerHook(tb, nil))
	}
}

// WithRecordMessages records each message the model receives in the
// session's [reel.Timeline], as a "steep.message" annotation of its type
// and value, as fmt's %+v prints it, cut to 4 KiB (see the package
// documentation for what else is recorded). It is off by default, since a
// program that ticks or animates receives many messages, each of which
// grows the timeline and the ".cast" file of a recording.
//
// Environment: STEEP_RECORD_MESSAGES (true or 1, or false or 0) overrides
// this option unless [WithIgnoreEnv] is set.
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
// Using this option replaces those defaults with the test process environment
// plus vars, so pass TTY_FORCE and TERM again if the program needs them.
// [WithColor] replaces the color variables after this option. It panics if an
// entry is not in "key=value" form.
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
