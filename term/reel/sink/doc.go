// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package sink encodes frames, such as render package output, to PNG and
// GIF, and through ffmpeg to MP4, WebM, and animated WebP.
//
// Every output is a [Sink], which is given frames with resolved delays and
// finishes the output on [Sink.Close]: [CreateGIF] or [NewGIF] for GIF,
// [CreateVideo] or [NewVideo] for video. [File] picks the sink by the
// extension of a path, and [Writer] by a [Format], for PNG of the last frame
// too. [Chan] hands frames to a channel and [Tee] writes one stream into
// several sinks. After a frame or encoder error, call [Sink.Discard] instead
// of [Sink.Close].
//
// Each sink, and [PNG], reads only the options that apply to it and ignores
// the rest, so one option list can configure them all.
//
// Sinks writing to a path write a temporary file beside it and rename it into
// place only once the output is complete, so a failed or discarded recording
// leaves an existing file alone.
//
// # Video
//
// Video needs ffmpeg, looked up on PATH or set with [WithFFmpeg]. The binary
// and the format's encoder are checked when the encoder is created, so a
// missing one fails before anything is recorded ([ErrFFmpegNotFound],
// [MissingEncoderError]); [CheckVideo] runs the same check alone. Each frame
// keeps its own duration, so a long hold stays one frame.
//
// MP4 and WebM are 4:2:0 by default for compatibility; 4:2:0 needs even
// dimensions, so odd ones are padded ([WithFullChroma] keeps full color
// resolution). Both are composited onto [WithBackground] (black by
// default). WebP keeps transparency and [WithLoopCount], and is lossless by
// default. [WithQuality] trades size for fidelity in every format.
//
// # GIF
//
// GIF stores delays in whole centiseconds. A frame identical to the previous
// one is merged into it, adding its delay ([OptimizeFrames]). A hold longer
// than a GIF frame can store (~655s) is split across frames, keeping its
// length, if the frame uses no disposal; one using another disposal method
// is clamped to ~655s.
//
// GIF capture keeps every frame in memory until Close by default.
// [WithMaxFrames] and [WithMaxBytes] keep only the most recent frames, dropping
// older frames and their delays.
//
// [WithOptimize] defaults to [OptimizeAll]: frame dedup, sub-rect disposal,
// and palette compaction for GIF, and an indexed palette for PNG. Pass
// [OptimizeNone] for unoptimized GIF or full RGBA PNG with alpha preserved.
//
// # Palette
//
// GIF and indexed PNG use a palette of up to 255 colors built from the colors
// the recording uses, plus one transparent entry. Fills, glyph bodies, and
// small flat shapes get exact entries first; antialiased edges get exact
// entries while slots remain, then median-cut representatives. Once full, new
// colors map to the nearest entry, and a frame that maps poorly (a theme
// change, a gradient through new hues) starts a fresh palette from that frame.
//
// [Frame.Isolated] frames use a separate palette and leave the shared one
// untouched. GIF writes the most-used palette as the global color table and
// the others as local color tables.
//
// Only fully transparent source pixels (alpha 0) stay transparent in GIF and
// indexed PNG, unless [WithBackground] fills them in GIF. Translucent pixels
// become their opaque color.
package sink
