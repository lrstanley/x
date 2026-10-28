// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"fmt"
	"image/color"

	"github.com/lrstanley/x/term/reel/render/units"
)

const (
	// MaxQuality is the highest [WithQuality], and selects lossless encoding
	// where the format has it.
	MaxQuality = 100

	// MaxFrameRate is the highest frame rate a recording can play at: GIF
	// delays are whole centiseconds, so 100 FPS.
	MaxFrameRate = 100

	// LoopForever is the default [WithLoopCount]: viewers repeat the GIF until
	// stopped.
	LoopForever = 0
	// LoopOnce plays a GIF a single time and does not repeat.
	LoopOnce = -1

	// maxGIFLoopCount is the largest loop count GIF can store (uint16).
	maxGIFLoopCount = 65535
)

// OptimizeFlags selects optimizations for [WithOptimize]. PNG reads only
// [OptimizeColorQuantization].
type OptimizeFlags uint

const (
	OptimizeNone OptimizeFlags = 0
	// OptimizeFrames merges a GIF frame identical to the previous one into it,
	// adding its delay.
	OptimizeFrames OptimizeFlags = 1 << iota
	// OptimizeDirtyRects writes GIF frames as the sub-rect that changed, with
	// disposal methods.
	OptimizeDirtyRects
	// OptimizeColorQuantization compacts each GIF palette to the colors its
	// frames use, and encodes PNG as an indexed paletted image.
	OptimizeColorQuantization

	// OptimizeAll enables every optimization, and is the default.
	OptimizeAll = OptimizeFrames | OptimizeDirtyRects | OptimizeColorQuantization
)

func (f OptimizeFlags) has(flag OptimizeFlags) bool {
	return f&flag != 0
}

// Option configures a sink or a PNG export. Each validates the options it
// reads and ignores the rest, so one option list can be shared between them.
type Option func(*options)

type options struct {
	optimize OptimizeFlags
	// background is nil when unset.
	background color.Color
	// maxFrames and maxBytes are 0 for no limit.
	maxFrames int
	maxBytes  int64
	loopCount int
	// quality is only meaningful when qualitySet; otherwise each video format
	// uses its own default.
	quality    int
	qualitySet bool
	fullChroma bool
	// ffmpeg is empty to look up "ffmpeg" on PATH.
	ffmpeg string
}

// collectOptions applies opts without validating them: each output checks
// only the options it reads ([gifOptions], [videoOptions]).
func collectOptions(opts []Option) options {
	cfg := options{optimize: OptimizeAll}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// gifOptions collects opts for a [GIFEncoder].
func gifOptions(opts []Option) (options, error) {
	cfg := collectOptions(opts)
	err := cfg.validateLoopCount()
	return cfg, err
}

// videoOptions collects opts for a [VideoEncoder].
func videoOptions(opts []Option) (options, error) {
	cfg := collectOptions(opts)
	if cfg.qualitySet && cfg.quality != units.Clamp(cfg.quality, 0, MaxQuality) {
		return cfg, fmt.Errorf("sink: WithQuality %d out of range [0, %d]", cfg.quality, MaxQuality)
	}
	err := cfg.validateLoopCount()
	return cfg, err
}

func (o options) validateLoopCount() error {
	if o.loopCount < LoopOnce || o.loopCount > maxGIFLoopCount {
		return fmt.Errorf("sink: WithLoopCount %d out of range [%d, %d]", o.loopCount, LoopOnce, maxGIFLoopCount)
	}
	return nil
}

// WithOptimize sets the optimization flags, replacing any earlier
// [WithOptimize]. The default is [OptimizeAll]; [OptimizeNone] disables all
// optimizations, which for PNG means full RGBA with alpha preserved.
func WithOptimize(flags OptimizeFlags) Option {
	return func(cfg *options) {
		cfg.optimize = flags
	}
}

// WithBackground sets an opaque background color, ignoring c's alpha.
// [GIFEncoder] fills fully transparent pixels with it. [VideoEncoder]
// composites every frame onto it and pads with it; without it, MP4 and WebM
// use black and WebP keeps transparency. A nil c leaves it unset. PNG ignores
// it.
func WithBackground(c color.Color) Option {
	return func(cfg *options) {
		cfg.background = c
	}
}

// WithLoopCount sets how many times a GIF or WebP animation repeats, like
// image/gif's GIF.LoopCount: [LoopForever] (0, the default), [LoopOnce] (-1),
// or n in [1, 65535] to play n+1 times. Other values are rejected. MP4 and
// WebM ignore it.
func WithLoopCount(n int) Option {
	return func(cfg *options) {
		cfg.loopCount = n
	}
}

// WithMaxFrames keeps only the last n frames of a GIF recording, counted after
// [OptimizeFrames] merges duplicates. The oldest frames are dropped along with
// their delays. n <= 0, the default, is no limit. With [WithMaxBytes],
// whichever limit is reached first drops frames. GIF only.
func WithMaxFrames(n int) Option {
	return func(cfg *options) {
		cfg.maxFrames = max(0, n)
	}
}

// WithMaxBytes limits the pixel memory held by GIF frames to about n bytes,
// dropping the oldest frames along with their delays. A frame costs one byte
// per pixel of the region that changed since the previous one. The newest
// frame is always kept, even if larger than n, and Close temporarily needs up
// to about as much memory again. n <= 0, the default, is no limit. GIF only.
func WithMaxBytes(n int64) Option {
	return func(cfg *options) {
		cfg.maxBytes = max(0, n)
	}
}

// WithQuality sets how closely [VideoEncoder] output matches the frames, from 0
// (smallest file) to [MaxQuality] (lossless where the format supports it).
// MP4 maps q to libx264 CRF 51..0 (default CRF 18) and WebM to libvpx-vp9 CRF
// 63..0 (default CRF 30, lossless at [MaxQuality]). WebP is lossless by
// default and at [MaxQuality], which for terminal frames is smaller than lossy
// at any useful quality; a lower q selects lossy WebP at that quality. Values
// outside [0, [MaxQuality]] are rejected. PNG and GIF ignore it.
func WithQuality(q int) Option {
	return func(cfg *options) {
		cfg.quality = q
		cfg.qualitySet = true
	}
}

// WithFullChroma encodes MP4 and WebM as 4:4:4 instead of 4:2:0, keeping thin
// colored text sharp. Many browsers and phones do not play 4:4:4 H.264; VP9
// 4:4:4 plays in Chrome and Firefox. Other formats ignore it.
func WithFullChroma(enabled bool) Option {
	return func(cfg *options) {
		cfg.fullChroma = enabled
	}
}

// WithFFmpeg sets the ffmpeg binary [VideoEncoder] runs: a path, or a name
// looked up on PATH. The default is "ffmpeg" on PATH. PNG and GIF ignore it.
func WithFFmpeg(path string) Option {
	return func(cfg *options) {
		cfg.ffmpeg = path
	}
}
