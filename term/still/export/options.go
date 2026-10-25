// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/lrstanley/x/term/still/units"
)

const (
	// MaxQuality is the highest [WithQuality], and selects lossless encoding
	// where the format has it.
	MaxQuality = 100

	// MaxFrameRate is the highest FPS accepted by [WithFrameRate]. GIF delays
	// are stored in centiseconds, so cannot represent more than 100 FPS.
	MaxFrameRate = 100

	// LoopForever is the default [WithLoopCount]: viewers repeat the GIF until
	// stopped.
	LoopForever = 0
	// LoopOnce plays a GIF a single time and does not repeat.
	LoopOnce = -1

	// maxGIFLoopCount is the largest loop count image/gif can store. Larger
	// values wrap when encoded as a uint16.
	maxGIFLoopCount = 65535
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

// Option configures a [Recorder], an encoder, or a PNG export. Each validates
// the options it reads and ignores the rest, so one option list can be shared
// between them.
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
	// delayGrid is the FPS that explicit [Frame] delays snap to when
	// delayGridSet is true.
	delayGrid int
	// delayGridSet records whether [WithDelayGrid] was set.
	delayGridSet bool
	// channel receives frames for channel-driven capture when channelSet is true.
	channel <-chan Frame
	// channelSet records whether [WithChannel] was set.
	channelSet bool
	// maxFrames caps stored palettized frames during GIF capture; 0 is unlimited.
	maxFrames int
	// maxBytes caps pixel storage of retained frames during GIF capture; 0 is
	// unlimited.
	maxBytes int64
	// loopCount is copied into GIF output. [LoopForever] (0) loops forever,
	// [LoopOnce] (-1) plays once, and n > 0 plays n+1 times. PNG ignores it.
	loopCount int
	// quality is the video encode quality in [0, MaxQuality] when qualitySet
	// is true; otherwise each video format uses its own default.
	quality    int
	qualitySet bool
	// fullChroma encodes MP4 and WebM as 4:4:4 instead of 4:2:0.
	fullChroma bool
	// ffmpeg is the ffmpeg binary name or path; empty looks up "ffmpeg" on
	// PATH.
	ffmpeg string
}

func defaultOptions() options {
	return options{}
}

// collectOptions applies opts, then validate, which checks the options its
// output reads and ignores the rest. That is what lets one option list be
// shared between a [Recorder], every encoder, and PNG.
func collectOptions(validate func(*options) error, opts ...Option) (options, error) {
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if !cfg.optimizeSet {
		cfg.optimize = OptimizeAll
	}
	err := validate(&cfg)
	return cfg, err
}

// validatePNG strips the GIF-only optimize flags. PNG reads nothing else.
func validatePNG(o *options) error {
	o.optimize &^= OptimizeFrames | OptimizeDirtyRects
	return nil
}

// validateRecorder checks the frame source and its grid.
func validateRecorder(o *options) error {
	if o.channelSet == o.frameRateSet {
		return errors.New("export: Record requires exactly one of WithChannel or WithFrameRate")
	}
	if o.frameRateSet {
		if o.frameRate != units.Clamp(o.frameRate, 1, MaxFrameRate) {
			return fmt.Errorf("export: WithFrameRate fps %d out of range [1, %d]", o.frameRate, MaxFrameRate)
		}
		if o.frameRateFn == nil {
			return errors.New("export: WithFrameRate requires a non-nil frame function")
		}
	}
	if o.frameRateSet && o.delayGridSet {
		return errors.New("export: WithFrameRate and WithDelayGrid are mutually exclusive")
	}
	if o.delayGridSet && o.delayGrid != units.Clamp(o.delayGrid, 1, MaxFrameRate) {
		return fmt.Errorf("export: WithDelayGrid fps %d out of range [1, %d]", o.delayGrid, MaxFrameRate)
	}
	return nil
}

func validateGIF(o *options) error {
	return o.validateLoopCount()
}

func validateVideo(o *options) error {
	if o.qualitySet && o.quality != units.Clamp(o.quality, 0, MaxQuality) {
		return fmt.Errorf("export: WithQuality %d out of range [0, %d]", o.quality, MaxQuality)
	}
	return o.validateLoopCount()
}

func (o options) validateLoopCount() error {
	if o.loopCount < LoopOnce || o.loopCount > maxGIFLoopCount {
		return fmt.Errorf("export: WithLoopCount %d out of range [%d, %d]", o.loopCount, LoopOnce, maxGIFLoopCount)
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

// WithPalette overrides the capture palette for GIF palettization, replacing
// the palette built from captured colors (see package documentation).
// Disables automatic palette compaction when [OptimizeColorQuantization] is
// set. When using disposal optimization, palettes with 256 entries should
// reserve [TransparentPaletteIndex] (255) as a zero-alpha color; see package
// documentation for the transparency invariant. [GIFEncoder] only.
func WithPalette(p color.Palette) Option {
	return func(cfg *options) {
		cfg.palette = p
		cfg.paletteSet = true
	}
}

// WithBackground sets an opaque compositing color for semi-transparent pixels.
// [GIFEncoder] fills fully transparent pixels with it before palettization. A
// [VideoEncoder] composites every frame onto it and uses it for padding; without it, MP4 and WebM composite onto
// black and WebP keeps transparency.
func WithBackground(c color.Color) Option {
	return func(cfg *options) {
		cfg.background = c
		cfg.backgroundSet = true
	}
}

// WithFrameRate makes a [Recorder] capture a frame from fn at fps, on a delay
// grid of the same fps (see [WithDelayGrid]). When strict is true, each frame
// plays for exactly time.Second/fps, so ticks dropped while fn runs shorten
// the recording. When strict is false, each frame plays for the wall time
// since the previous one. Mutually exclusive with [WithChannel] and
// [WithDelayGrid].
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

// WithDelayGrid makes a [Recorder] round each delay to the nearest multiple of
// max(1, 100/fps) centiseconds, at least one step. That is the grid GIF can
// store for fps: at rates that are not centisecond-aligned (such as 30, a
// 30ms step), steady frames play slightly fast, while holds keep their
// length. Without it, delays are rounded to whole centiseconds. Mutually
// exclusive with [WithFrameRate], which sets its own grid. fps must be in
// [1, [MaxFrameRate]]; see [WithFrameRate] for which rates play back smoothly.
func WithDelayGrid(fps int) Option {
	return func(cfg *options) {
		cfg.delayGrid = fps
		cfg.delayGridSet = true
	}
}

// WithChannel makes a [Recorder] read frames from ch until it is closed or
// [Recorder.Close] is called. Mutually exclusive with [WithFrameRate].
func WithChannel(ch <-chan Frame) Option {
	return func(cfg *options) {
		cfg.channel = ch
		cfg.channelSet = true
	}
}

// WithLoopCount sets how many times an animation repeats, matching
// image/gif's GIF.LoopCount field.
//
// [LoopForever] (0), the default, loops forever. [LoopOnce] (-1) plays the
// animation once and does not repeat. A count n in [1, 65535] plays the
// animation n+1 times. Values outside [-1, 65535] are rejected. GIF and
// animated WebP use it; MP4 and WebM have no loop count and ignore it.
func WithLoopCount(n int) Option {
	return func(cfg *options) {
		cfg.loopCount = n
	}
}

// WithMaxFrames keeps only the last n palettized frames in memory during GIF
// capture. When [OptimizeFrames] is enabled, duplicate consecutive frames merged
// during dedup do not consume cap slots. Oldest frames are dropped without
// carrying their delays forward. n <= 0 disables the cap (same as omitting this
// option), which is the default. Combine with [WithMaxBytes], whichever limit
// is reached first drops frames. GIF only.
func WithMaxFrames(n int) Option {
	return func(cfg *options) {
		cfg.maxFrames = max(0, n)
	}
}

// WithMaxBytes limits the pixel memory held by retained GIF frames to about n
// bytes, dropping the oldest frames (without carrying their delays forward)
// once the total goes over. Frames are stored as the region that changed since
// the previous one, so a small change costs little and a full repaint costs a
// whole frame (one byte per pixel). The newest frame is always kept, even if
// it alone is larger than n, and the buffers used to diff frames are not
// counted. Finalizing temporarily needs up to about as much memory again. n <=
// 0 disables the limit (same as omitting this option), which is the default.
// GIF only.
func WithMaxBytes(n int64) Option {
	return func(cfg *options) {
		cfg.maxBytes = max(0, n)
	}
}

// WithQuality sets how closely [VideoEncoder] output matches the frames, from 0
// (smallest file) to [MaxQuality] (lossless where the format supports it).
// Without it, each format uses its own default, which is already visually
// close for terminal content. MP4 maps q to libx264 CRF 51..0 (default CRF 18)
// and WebM to libvpx-vp9 CRF 63..0 (default CRF 30, lossless at
// [MaxQuality]). WebP is lossless by default and at [MaxQuality], which for
// terminal frames is smaller than lossy at any quality worth using; a lower q
// selects lossy WebP at that quality. Values outside [0, [MaxQuality]] are
// rejected. PNG and GIF ignore it.
func WithQuality(q int) Option {
	return func(cfg *options) {
		cfg.quality = q
		cfg.qualitySet = true
	}
}

// WithFullChroma encodes MP4 and WebM as 4:4:4, keeping the full color
// resolution of thin colored text instead of halving it (4:2:0, the default).
// Most desktop players handle it; many browsers and phones do not play 4:4:4
// H.264, while VP9 4:4:4 (profile 1) plays in Chrome and Firefox. WebP, PNG,
// and GIF ignore it.
func WithFullChroma(enabled bool) Option {
	return func(cfg *options) {
		cfg.fullChroma = enabled
	}
}

// WithFFmpeg sets the ffmpeg binary [VideoEncoder] runs: a path, or a name
// looked up on PATH. The default is "ffmpeg" on PATH (with the platform's
// executable extension, such as ffmpeg.exe on Windows). PNG and GIF ignore it.
func WithFFmpeg(path string) Option {
	return func(cfg *options) {
		cfg.ffmpeg = path
	}
}
