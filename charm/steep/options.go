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
	"github.com/lrstanley/x/charm/still"
	"github.com/lrstanley/x/charm/still/export"
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

	// envVars are extra environment variables to set for the [tea.Program].
	envVars []string

	// ctx is the parent context for blocking [Wait]-style helpers.
	ctx           context.Context
	wasContextSet bool

	// imageRendererOpts configures the harness-owned [still.Renderer].
	imageRendererOpts []still.Option

	exportMode   ExportMode
	exportTypes  ExportType
	exportDir    string
	exportName   string
	exportOpts   []export.Option
	hooks        []Hook
	ignoreEnv    bool
	slow         time.Duration
	slowExport   time.Duration
	gifMaxFrames int
	snapshotDir  string
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
			"TERM=xterm-256color",
			// TODO: remove this default env var once Bubble Tea either flushes
			// startup DECRQM mode probes before input shutdown, or x/vt makes query
			// responses non-blocking. Without this, WT_SESSION/kitty-like envs make
			// Bubble Tea emit synchronized-output/unicode-core queries that x/vt can
			// answer by blocking on its input pipe during fast test cleanup.
			"TERM_PROGRAM=Apple_Terminal",
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
// [WaitMessageWhere], [Harness] wait helpers, etc.) and for the running
// [tea.Program]. Defaults to a notify context through [signal.NotifyContext]
// when invoked through [Harness], else [context.Background].
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

// WithTimeout configures how long waits may run.
//
// Environment: STEEP_TIMEOUT (unless [WithIgnoreEnv] is set).
//
// See also [DefaultTimeout].
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *options) {
		cfg.timeout = timeout
	}
}

// WithCheck configures how often waits are checked.
//
// Environment: STEEP_CHECK.
//
// See also [DefaultCheckInterval].
func WithCheck(interval time.Duration) Option {
	return func(cfg *options) {
		cfg.checkInterval = interval
	}
}

// WithSettle configures how long to wait for the [tea.Program] to satisfy
// settle waits. See [Harness.WaitSettleMessages], [Harness.WaitSettle] and
// [WaitSettle] for more details.
//
// Environment: STEEP_SETTLE.
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
// samples are skipped.
//
// See also [DefaultSettleIgnoreMsgs].
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

// WithANSI determines whether ANSI sequences should be kept. When false, ANSI
// sequences are stripped, which will often make it easier to visually and
// programmatically compare snapshots, output, etc. This would apply to strings/regex
// comparisons in [Assert], [AssertMatch], [Wait], and so on.
func WithANSI(enable bool) Option {
	return func(cfg *options) {
		cfg.stripANSI = !enable
	}
}

// WithRenderer configures the harness-owned [still.Renderer] used by
// [Harness.Image] and [Harness.ImageInto]. Options apply only at harness
// construction; per-draw emulator state is synchronized automatically.
//
// Environment: STEEP_RENDERER_FONT_SIZE maps to [still.WithFontSizePt] unless
// [WithIgnoreEnv] is set.
func WithRenderer(opts ...still.Option) Option {
	return func(cfg *options) {
		cfg.imageRendererOpts = append(cfg.imageRendererOpts, opts...)
	}
}

// WithHooks appends user hooks after built-in export hooks.
func WithHooks(hooks ...Hook) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, hooks...)
	}
}

// WithIgnoreEnv disables STEEP_* environment overrides (defaults to applying them).
func WithIgnoreEnv(ignore bool) Option {
	return func(cfg *options) {
		cfg.ignoreEnv = ignore
	}
}

// WithExport enables artifact export hooks for the given mode and types.
//
// Environment: STEEP_EXPORT_MODE, STEEP_EXPORT_TYPE, STEEP_EXPORT_OPTIMIZE,
// STEEP_EXPORT_MAX_FRAMES, STEEP_EXPORT_DIR.
func WithExport(mode ExportMode, types ExportType, opts ...export.Option) Option {
	return func(cfg *options) {
		cfg.exportMode = mode
		cfg.exportTypes = types
		cfg.exportOpts = append(cfg.exportOpts, opts...)
	}
}

// WithExportDir sets the directory for export artifacts. When empty, uses
// [testing.TB.ArtifactDir].
//
// Environment: STEEP_EXPORT_DIR.
func WithExportDir(dir string) Option {
	return func(cfg *options) {
		cfg.exportDir = dir
	}
}

// WithExportName overrides the basename used for export filenames.
func WithExportName(name string) Option {
	return func(cfg *options) {
		cfg.exportName = name
	}
}

// WithSlow sleeps after every hooked harness action's After stage.
//
// Environment: STEEP_SLOW.
func WithSlow(d time.Duration) Option {
	return func(cfg *options) {
		cfg.slow = d
	}
}

// WithSlowExport sets the delay between exported GIF frames (default 250ms).
//
// Environment: STEEP_SLOW_EXPORT.
func WithSlowExport(d time.Duration) Option {
	return func(cfg *options) {
		cfg.slowExport = d
	}
}

// WithFailureGIF is shorthand for [WithExport]([ExportOnFailure], [ExportTypeGIF], ...).
func WithFailureGIF(opts ...export.Option) Option {
	return WithExport(ExportOnFailure, ExportTypeGIF, opts...)
}

// WithLog is a shorthand for [WithHooks] & [LoggerHook] with a nil logger
// (uses [testing.TB.Log] by default).
//
// Environment: STEEP_LOG.
func WithLog(tb testing.TB) Option {
	return func(cfg *options) {
		cfg.hooks = append(cfg.hooks, LoggerHook(tb, nil))
	}
}

// WithEnvVars sets extra environment variables for the [tea.Program], in the form
// of "key=value". Defaults:
//
//   - TERM=xterm-256color
//   - TERM_PROGRAM=Apple_Terminal
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
