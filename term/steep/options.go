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
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/still/export"
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
	// color is the color support advertised to the [tea.Program], or 0 to
	// leave envVars as they are.
	color ColorProfile

	// ctx is the parent context for blocking [Wait]-style helpers.
	ctx           context.Context
	wasContextSet bool

	// imageRendererOpts configures the harness-owned [still.Renderer].
	imageRendererOpts []still.Option

	exportMode     ExportMode
	exportTypes    ExportType
	exportDir      string
	exportName     string
	exportOpts     []export.Option
	exportMutators []ExportMutator
	exportKeys     time.Duration
	exportIntro    ExportMutator
	exportOutro    ExportMutator
	hooks          []Hook
	ignoreEnv      bool
	slow           time.Duration
	exportFPS      int
	exportSpeed    float64
	snapshotDir    string
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

// WithCheck configures how often polling waits re-check their condition. It is
// also the idle gap export capture waits after a paint before sampling the
// screen, so a very large value slows recorded actions.
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
// and [WaitSettle] for more details. It is capped at 80% of [WithTimeout]. When
// exporting, it also bounds how long an action waits for the program to paint
// (with a 50ms floor) before giving up.
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
// on, and to [Harness.AssertSnapshot]. It does not change what image exports
// render.
func WithANSI(enable bool) Option {
	return func(cfg *options) {
		cfg.stripANSI = !enable
	}
}

// WithRenderer configures the harness-owned [still.Renderer] used by
// [Harness.Image] and [Harness.ImageInto], and therefore by PNG and GIF exports
// (font, size, padding, margin, and so on). Options apply only at harness
// construction; per-draw emulator state is synchronized automatically. Calls
// accumulate.
//
// Environment: STEEP_RENDERER_SCALE maps to [still.WithScale] and
// STEEP_RENDERER_FONT_FAMILY maps to [still.WithFontFamilyName]. Both are
// appended after these options, so they win, unless [WithIgnoreEnv] is set.
func WithRenderer(opts ...still.Option) Option {
	return func(cfg *options) {
		cfg.imageRendererOpts = append(cfg.imageRendererOpts, opts...)
	}
}

// WithHooks appends user hooks, which run after the built-in export hooks for
// each event and stage (see [Hook]). A hook that returns an error fails the test
// immediately. Calls accumulate.
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

// WithExport enables artifact export hooks for the given mode and types. opts
// are passed to the PNG, GIF, and video encoders (see [export.Option]), each of
// which ignores the ones it does not use.
//
// With [ExportOn], artifacts are written as the test runs. With
// [ExportOnFailure], they are buffered and only written when the test fails.
// [ExportOff] disables export (except [ExportTypeLog], see [ExportMode]). If
// types is 0 and mode is not [ExportOff], GIF and PNG are exported. Files go to
// [WithExportDir], else [testing.TB.ArtifactDir].
//
// [ExportTypeMP4], [ExportTypeWebM], and [ExportTypeWebP] record the same
// frames as the GIF as a video ("name.mp4", "name.webm", "name.webp", or
// "name_failed.<ext>" for [ExportOnFailure]), in any combination with the other
// types. They are encoded by ffmpeg, which must be on PATH ([export.WithFFmpeg]
// points elsewhere), along with the format's encoder (libx264, libvpx-vp9, or
// libwebp). [ExportTypeAll] does not include them. If ffmpeg or the encoder is
// missing, the harness fails right away rather than at the end of the test, so
// remove the type from here or from STEEP_EXPORT_TYPE to run without it.
// [export.WithQuality] and [export.WithFullChroma] tune the encode.
//
// Without [WithExportDir], artifacts are only persisted when the tests are run
// with "go test -artifacts" (Go 1.26+). Otherwise, [testing.TB.ArtifactDir] is a
// temporary directory that is deleted when the test completes, so exported
// files are lost. Use "-outputdir" to choose where they are written.
//
// Environment (each overrides this option unless [WithIgnoreEnv] is set):
// STEEP_EXPORT_MODE (on, off, failure), STEEP_EXPORT_TYPE (comma-separated png,
// gif, mp4, webm, webp, log, snapshot, or all, which excludes the video types),
// STEEP_EXPORT_OPTIMIZE, STEEP_EXPORT_MAX_FRAMES, STEEP_EXPORT_MAX_BYTES,
// STEEP_EXPORT_QUALITY (0 to 100, for video), STEEP_EXPORT_DIR.
//
// GIF recordings keep every frame unless [export.WithMaxFrames] or
// [export.WithMaxBytes] (or their environment variables) set a limit. Frames
// are stored as the part of the screen that changed, so a long recording of an
// ordinary terminal UI is small, but one that repaints most of the screen on
// every frame (a scrolling log, say) costs a whole frame each.
func WithExport(mode ExportMode, types ExportType, opts ...export.Option) Option {
	return func(cfg *options) {
		cfg.exportMode = mode
		cfg.exportTypes = types
		cfg.exportOpts = append(cfg.exportOpts, opts...)
	}
}

// WithExportDir sets the directory for export artifacts, creating it if needed.
// A relative path is relative to the test's package directory. When empty, uses
// [testing.TB.ArtifactDir], which only persists files when tests are run with
// "go test -artifacts".
//
// Environment: STEEP_EXPORT_DIR (overrides this option unless [WithIgnoreEnv] is set).
func WithExportDir(dir string) Option {
	return func(cfg *options) {
		cfg.exportDir = dir
	}
}

// WithExportName overrides the basename used for export filenames, which
// defaults to the normalized test name. With [ExportOn] this gives stable names
// (for example "name.gif", "name_input.png"), so committed recordings are
// overwritten each run instead of accumulating timestamped files.
func WithExportName(name string) Option {
	return func(cfg *options) {
		cfg.exportName = name
	}
}

// WithExportMutators appends [ExportMutator]s that change GIF and video frames
// between the terminal and the encoders: overlays, inserted cards, holds, and
// so on. Calls accumulate. The key overlay from [WithExportKeys] and the cards
// from [WithExportIntro] and [WithExportOutro] are added after every mutator,
// in that order, so overlays never cover the keys or the cards.
//
// Mutators run in order, and each sees the frames the ones before it
// emitted. A later mutator draws on top of an earlier one and sees any cards
// it inserted, so list overlays that should not cover a card before it.
//
// Inserted frames count toward any GIF frame or byte limit
// (STEEP_EXPORT_MAX_FRAMES, STEEP_EXPORT_MAX_BYTES), which keep the newest
// frames, so a limited recording can drop an intro card.
// An overlay that changes pixels on otherwise identical frames defeats
// duplicate frame merging and grows the GIF. Mutators apply to [ExportOn] and
// [ExportOnFailure] recordings alike; see [ExportContext.Mode]. There is no
// environment override.
func WithExportMutators(mutators ...ExportMutator) Option {
	return func(cfg *options) {
		cfg.exportMutators = append(cfg.exportMutators, mutators...)
	}
}

// WithExportKeys overlays the keys the test presses (see [Harness.Key] and
// [Harness.Type]) on GIF and video recordings, in the bottom right corner. Keys
// pressed within d of each other are shown together, repeats as one key with a
// count, and stay for d after the last of them, fading out over its final
// third. Paste and mouse input are not shown. A d of zero or less removes the
// overlay.
//
// The overlay splits frames wherever it changes, which adds a few frames per
// burst of keys toward any GIF frame or byte limit (see [WithExportMutators]).
func WithExportKeys(d time.Duration) Option {
	return func(cfg *options) {
		cfg.exportKeys = d
	}
}

// WithExportIntro shows a title card for d before the first frame of a GIF
// recording. With lines, the card shows them (see [ExportContext.Card]). With
// none, it is an animated steep logo: a teacup and wordmark fill with tea, then
// steam rises for d while the test name appears. Terminals too small for the
// logo get "steep" and the test name as text instead. The animation's frames
// count toward any GIF frame or byte limit (see [WithExportMutators]). A d of
// zero or less removes the intro.
func WithExportIntro(d time.Duration, lines ...CardLine) Option {
	return func(cfg *options) {
		cfg.exportIntro = nil
		if d > 0 {
			cfg.exportIntro = introCard(d, lines...)
		}
	}
}

// WithExportOutro shows a title card for d after the last frame of a GIF
// recording. With lines, the card shows them (see [ExportContext.Card]). With
// none, it shows PASS or FAIL, matching the test result, and the test name. A
// FAIL card also lists the summary line of each failure the harness reported
// (see [ExportContext.Failures]), as many as fit. A d of zero or less removes
// the outro.
func WithExportOutro(d time.Duration, lines ...CardLine) Option {
	return func(cfg *options) {
		cfg.exportOutro = nil
		if d > 0 {
			cfg.exportOutro = outroCard(d, lines...)
		}
	}
}

// WithSlow makes the harness sleep for d after every hooked action (input,
// terminal, program, settle, ready, close) finishes, before its After hooks
// run. This paces the test so the program has real time to run between actions,
// which is useful for animations, ticks, and timers, or for watching a test
// play out.
//
// It is not required for complete GIF/PNG exports: frames are captured when the
// program paints, not when the sleep ends. It also does not set GIF playback
// speed; see [WithExportSpeed]. The sleep applies to every hooked action, so it
// slows the whole test, whether or not exporting is enabled.
//
// Environment: STEEP_SLOW.
func WithSlow(d time.Duration) Option {
	return func(cfg *options) {
		cfg.slow = d
	}
}

// WithExportFPS sets the frame rate grid for exported GIFs and videos (default
// 20, clamped to 1-100). Each frame is shown for the real time it was on
// screen, snapped to the nearest step of 1/fps (rounded down to whole
// centiseconds, see [export.WithDelayGrid]), and never for less than one step,
// so every distinct screen the program painted stays visible even when several
// paints land in the same instant. It does not sleep the test (see
// [WithSlow]).
//
// GIF delays are centiseconds, and browsers often cap playback near 50 FPS and
// clamp higher rates to 10 FPS, so prefer 10 or 20. See [export.WithFrameRate]
// for the full caveats.
//
// Environment: STEEP_EXPORT_FPS (overrides this option unless [WithIgnoreEnv] is set).
func WithExportFPS(fps int) Option {
	return func(cfg *options) {
		cfg.exportFPS = fps
	}
}

// WithExportSpeed sets the GIF playback speed relative to the test's real
// timing (default 1; values <= 0 use the default). 0.5 plays at half speed (every frame is shown twice as
// long) and 2 plays twice as fast. It scales recorded time only, so it never
// sleeps the test (see [WithSlow]). The one-step minimum from [WithExportFPS]
// still applies, which limits how far a recording can be sped up.
//
// Environment: STEEP_EXPORT_SPEED (overrides this option unless [WithIgnoreEnv] is set).
func WithExportSpeed(factor float64) Option {
	return func(cfg *options) {
		cfg.exportSpeed = factor
	}
}

// WithFailureGIF is shorthand for [WithExport]([ExportOnFailure], [ExportTypeGIF], ...).
// A GIF of the test is written (as "<name>_failed.gif") only if the test fails,
// and nothing is written when it passes.
//
// Unless [WithExportDir] is set, the GIF is written to [testing.TB.ArtifactDir],
// which is a temporary directory that is deleted when the test completes unless
// tests are run with "go test -artifacts". Without that flag, the GIF is lost.
func WithFailureGIF(opts ...export.Option) Option {
	return WithExport(ExportOnFailure, ExportTypeGIF, opts...)
}

// WithFailurePNG is shorthand for [WithExport]([ExportOnFailure], [ExportTypePNG], ...).
// A PNG of the last visible screen is written (as "<name>_failed.png") only if
// the test fails, and nothing is written when it passes.
//
// Unless [WithExportDir] is set, the PNG is written to [testing.TB.ArtifactDir],
// which is a temporary directory that is deleted when the test completes unless
// tests are run with "go test -artifacts". Without that flag, the PNG is lost.
func WithFailurePNG(opts ...export.Option) Option {
	return WithExport(ExportOnFailure, ExportTypePNG, opts...)
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
