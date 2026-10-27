// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package sink encodes frames, such as render package output, to PNG and
// GIF, and through ffmpeg to MP4, WebM, and animated WebP.
//
// An animation is a [Recorder] writing into an [Encoder]: [CreateGIF] or
// [NewGIF] for GIF, [CreateVideo] for video. The recorder reads [Frame] values
// from [WithChannel], or captures them with [WithFrameRate], and resolves
// every delay before the encoder sees it: a zero [Frame.Delay] becomes the
// time since the previous frame, and each delay is rounded to whole steps of
// the delay grid ([WithDelayGrid], the [WithFrameRate] FPS, or 10ms), at least
// one step. Encoders fed the same frames therefore play on the same timeline.
// [Recorder.Close] finishes the output; a recording with an invalid frame or
// an encoder error is discarded instead. Encoders can also be written to
// directly with [Encoder.WriteFrame].
//
// Options are shared: the recorder, every encoder, and [PNG] each check the
// options they read and ignore the rest, so one list can configure them all.
//
// # Video
//
// [CreateVideo] encodes MP4 (H.264), WebM (VP9), or animated WebP through
// ffmpeg, which must be installed: it is looked up on PATH, or set with
// [WithFFmpeg]. The binary and the format's encoder are checked when the
// encoder is created, so a missing one fails before anything is recorded
// ([ErrFFmpegNotFound], [MissingEncoderError]); [CheckVideo] runs the same
// check alone. Frames stream into ffmpeg's stdin uncompressed, each with its
// own timestamp and duration, so ffmpeg encodes while frames arrive and a
// hold stays one frame.
//
// MP4 and WebM are 4:2:0 by default for compatibility ([WithFullChroma] keeps
// full color resolution), padded to even dimensions, and composited onto
// [WithBackground] (black by default). WebP keeps transparency and
// [WithLoopCount], and is lossless by default. [WithQuality] trades size for
// fidelity in every format.
//
// [CreateGIF] and [CreateVideo] write a temporary file beside the
// destination, renamed into place only once the output is complete, so a
// failed or discarded recording leaves an existing file alone.
//
// # GIF
//
// GIF stores delays in centiseconds, so the 10ms default grid is exact. A
// single GIF frame stores at most 65535cs (~655.35s); longer [DisposalNone]
// holds are split into a full frame plus 1×1 transparent continuations so the
// timeline is preserved. A frame identical to the previous one is merged into
// it, adding its delay ([OptimizeFrames]).
//
// GIF capture keeps every frame by default. [WithMaxFrames] limits it to the
// last n palettized frames (after dedup when [OptimizeFrames] is enabled), and
// [WithMaxBytes] limits the pixel memory those frames hold, which suits long
// recordings better because a frame costs only what changed on screen. Dropped
// frame delays are not carried forward; disposal optimization runs on the
// retained window only.
//
// [WithLoopCount] sets how many times a GIF (or WebP) animation repeats. The default,
// [LoopForever], loops until the viewer stops. [LoopOnce] plays the animation
// a single time. PNG ignores [WithLoopCount].
//
// Export defaults to [OptimizeAll] when WithOptimize is omitted. GIF uses frame
// dedup, disposal/sub-rect optimization, and palette compaction. PNG applies
// [OptimizeColorQuantization] only ([OptimizeFrames] and [OptimizeDirtyRects]
// are stripped). Pass [OptimizeNone] for unoptimized GIF or full RGBA PNG
// with alpha preserved.
//
// # Palette
//
// Without [WithPalette], GIF capture builds its palette from the colors the
// recording uses. Each color keeps the index it was first given, so earlier
// frames stay valid as the palette grows. Colors covering many pixels (fills
// and glyph bodies) or forming a flat block, however small (window buttons,
// status dots), get exact entries first. Antialiased glyph edges get exact
// entries while slots remain, otherwise median cut picks representatives along
// each blend, which keeps edges on the foreground's hue. Once every slot is
// used, new colors map to the nearest existing entry. When a frame brings
// more than a few cells' worth of colors that map poorly that way (a theme
// change, a gradient sweeping through new hues), the recording starts a fresh
// shared palette from that frame. Frames before it keep the old one. PNG with
// [OptimizeColorQuantization] uses the same palette builder on its single
// image.
//
// [Frame.Isolated] builds a separate palette for that frame and leaves the
// shared one untouched. A run of consecutive isolated frames shares the
// separate palette. GIF writes the palette the most frames use as the global
// color table, and every other palette as a local color table compacted to
// the colors its frames use. Where the palette changes, disposal optimization
// compares the colors indices name rather than the indices, and writes only
// what changed color (nothing, when no color did), or a full keyframe when
// most of the screen did.
//
// # GIF palette transparency
//
// The capture and optimization pipeline reserves palette index 255
// ([TransparentPaletteIndex]) for transparency. During palettization, fully
// transparent source pixels (alpha 0) map to this index, and opaque pixels
// never do. The default capture palette pre-fills slot 255 with a zero-alpha
// marker.
//
// At finalize, disposal sub-rect optimization marks unchanged pixels with
// TransparentPaletteIndex; GIF viewers require a zero-alpha palette entry at
// that index (finalize patches opaque slot 255 when needed). Palette
// compaction may move transparent pixels to a lower index but keeps a
// zero-alpha palette entry so [ReplayGIF] can detect transparency.
//
// Custom capture palettes ([WithPalette]) with 256 entries should leave slot
// 255 transparent when using disposal optimization; finalize patches an
// opaque slot 255 automatically when the palette is long enough.
package sink
