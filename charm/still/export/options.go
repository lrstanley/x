// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/lrstanley/x/charm/still/units"
)

const (
	formatPNG = "png"
	formatGIF = "gif"

	// MaxFrameRate is the highest FPS accepted by [WithFrameRate]. GIF delays
	// are stored in centiseconds, so cannot represent more than 100 FPS.
	MaxFrameRate = 100
)

// OptimizeFlags selects post-process and capture optimizations. Each
// [WithOptimize] call replaces the full flag set; [OptimizeNone] resets to zero.
//
// When [WithOptimize] is omitted, export defaults to [OptimizeAll]. On PNG,
// GIF-only flags are stripped so only [OptimizeColorQuantization] applies.
type OptimizeFlags uint

const (
	OptimizeNone OptimizeFlags = 0
	// OptimizeFrames deduplicates byte-identical GIF frames during capture,
	// merging their delays. GIF only; ignored on PNG.
	OptimizeFrames OptimizeFlags = 1 << iota
	// OptimizeDirtyRects runs disposal/sub-rect optimization at GIF finalize.
	// GIF only; ignored on PNG.
	OptimizeDirtyRects
	// OptimizeColorQuantization compacts the GIF palette at finalize when no
	// custom palette is set, or encodes PNG as an indexed paletted image.
	OptimizeColorQuantization

	// OptimizeAll enables every optimization flag above. On PNG export, GIF-only
	// bits are ignored and only color quantization runs.
	OptimizeAll = OptimizeFrames | OptimizeDirtyRects | OptimizeColorQuantization
)

func (f OptimizeFlags) has(flag OptimizeFlags) bool {
	return f&flag != 0
}

// Frame is the channel payload for [WithChannel]. Delay zero accumulates elapsed
// time since the last capture attempt (including deduped ticks) as Duration
// until finalize, when it is rounded once to centiseconds (minimum 1 cs).
// Explicit delays are preserved as Duration until finalize.
type Frame struct {
	Image image.Image
	Delay time.Duration
}

// Option configures PNG or GIF exports. Options are validated in
// collectOptions; GIF-only capture options return an error from PNG entry
// points.
type Option func(*options)

type options struct {
	// optimize selects capture and finalize optimizations; defaults to
	// [OptimizeAll] when optimizeSet is false.
	optimize OptimizeFlags
	// optimizeSet records whether [WithOptimize] was called explicitly.
	optimizeSet bool
	// palette overrides the GIF capture palette when paletteSet is true.
	palette color.Palette
	// paletteSet records whether [WithPalette] was set.
	paletteSet bool
	// background is the opaque compositing color for semi-transparent pixels
	// when backgroundSet is true.
	background color.Color
	// backgroundSet records whether [WithBackground] was set.
	backgroundSet bool
	// frameRate is the capture FPS when frameRateSet is true.
	frameRate int
	// frameRateStrict selects fixed-delay strict mode vs wall-clock non-strict
	// timing for [WithFrameRate].
	frameRateStrict bool
	// frameRateFn returns the image captured on each frame-rate tick.
	frameRateFn func() image.Image
	// frameRateSet records whether [WithFrameRate] was set.
	frameRateSet bool
	// channel receives frames for channel-driven capture when channelSet is true.
	channel <-chan Frame
	// channelSet records whether [WithChannel] was set.
	channelSet bool
	// maxFrames caps stored palettized frames during GIF capture; 0 is unlimited.
	maxFrames int
}

func defaultOptions() options {
	return options{}
}

func collectOptions(format string, opts ...Option) (options, error) {
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if !cfg.optimizeSet {
		cfg.optimize = OptimizeAll
	}
	if format == formatPNG {
		cfg.optimize &^= OptimizeFrames | OptimizeDirtyRects
	}
	return cfg, cfg.validate(format)
}

func (o options) validate(format string) error {
	if o.frameRateSet && o.channelSet {
		return errors.New("export: WithFrameRate and WithChannel are mutually exclusive")
	}
	if o.frameRateSet {
		if o.frameRate != units.Clamp(o.frameRate, 1, MaxFrameRate) {
			return fmt.Errorf("export: WithFrameRate fps %d out of range [1, %d]", o.frameRate, MaxFrameRate)
		}
		if o.frameRateFn == nil {
			return errors.New("export: WithFrameRate requires a non-nil frame function")
		}
	}
	if format == formatPNG {
		if o.paletteSet {
			return errors.New("export: WithPalette applies to GIF only")
		}
		if o.backgroundSet {
			return errors.New("export: WithBackground applies to GIF only")
		}
		if o.frameRateSet {
			return errors.New("export: WithFrameRate applies to GIF only")
		}
		if o.channelSet {
			return errors.New("export: WithChannel applies to GIF only")
		}
		if o.maxFrames > 0 {
			return errors.New("export: WithMaxFrames applies to GIF only")
		}
	}
	if format == formatGIF {
		if !o.frameRateSet && !o.channelSet {
			return errors.New("export: GIF requires WithFrameRate or WithChannel")
		}
	}
	return nil
}

// WithOptimize sets optimization flags, replacing any prior [WithOptimize]
// call. Pass [OptimizeNone] to disable all optimizations.
//
// When omitted, export defaults to [OptimizeAll]. Pass [OptimizeNone] for full
// RGBA PNG with alpha preserved.
func WithOptimize(flags OptimizeFlags) Option {
	return func(cfg *options) {
		cfg.optimize = flags
		cfg.optimizeSet = true
	}
}

// WithPalette overrides the capture palette for GIF palettization. Disables
// automatic palette compaction when [OptimizeColorQuantization] is set. GIF
// only. When using disposal optimization, palettes with 256 entries should
// reserve [TransparentPaletteIndex] (255) as a zero-alpha color; see package
// documentation for the transparency invariant.
func WithPalette(p color.Palette) Option {
	return func(cfg *options) {
		cfg.palette = p
		cfg.paletteSet = true
	}
}

// WithBackground sets an opaque compositing color for semi-transparent GIF
// pixels before palettization. GIF only.
func WithBackground(c color.Color) Option {
	return func(cfg *options) {
		cfg.background = c
		cfg.backgroundSet = true
	}
}

// WithFrameRate captures frames from fn at FPS. When strict is true, each tick
// stores an exact delay of time.Second/FPS and ticks are skipped if fn is still
// running; delays are grid-snapped to max(1, 100/FPS) centiseconds at finalize.
// When strict is false, delay accumulates wall time per tick and snaps to the
// nearest nominal FPS grid at finalize. GIF only.
//
// It is important to note that most browsers support a max of 50 FPS (and if
// higher, will actually cap at 10 FPS). Most operating systems and supported
// GIF players will cap at 100 FPS. Additionally, as GIFs use centiseconds for
// per-frame delays, the maximum is [MaxFrameRate] FPS (even if we could export
// higher). Additionally, for smoother GIFs, prefer centisecond-aligned FPS (10,
// 20, 25, 50, 100).
//
// fn must return quickly and must not retain the returned [image.Image] beyond
// the call (same contract as [still.Renderer.Draw] buffer reuse).
func WithFrameRate(fps int, strict bool, fn func() image.Image) Option {
	return func(cfg *options) {
		cfg.frameRate = fps
		cfg.frameRateStrict = strict
		cfg.frameRateFn = fn
		cfg.frameRateSet = true
	}
}

// WithChannel reads frames from ch until it is closed, then finalizes and
// encodes. The returned closer can cancel early; either channel close or the
// first closer call triggers finalize (once). GIF only.
func WithChannel(ch <-chan Frame) Option {
	return func(cfg *options) {
		cfg.channel = ch
		cfg.channelSet = true
	}
}

// WithMaxFrames keeps only the last n palettized frames in memory during GIF
// capture. When [OptimizeFrames] is enabled, duplicate consecutive frames merged
// during dedup do not consume cap slots. Oldest frames are dropped without
// carrying their delays forward. n == 0 disables the cap (same as omitting this
// option). GIF only.
func WithMaxFrames(n int) Option {
	return func(cfg *options) {
		cfg.maxFrames = max(0, n)
	}
}
