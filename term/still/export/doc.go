// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package export encodes still renderer output to PNG and GIF.
//
// GIF capture accumulates per-frame delays as [time.Duration] internally;
// conversion to centiseconds (1/100s) happens at finalize. Delays shorter than
// 10 ms round up to 1 cs. A single GIF frame stores at most 65535cs (~655.35s);
// longer [DisposalNone] holds are split into a full frame plus 1×1 transparent
// continuations so the timeline is preserved. [WithChannel] accepts explicit [Frame.Delay] values preserved as
// Duration until finalize; zero delay accumulates elapsed wall time since the
// last capture attempt (including deduped ticks). Non-strict [WithFrameRate]
// snaps accumulated delays to the nearest nominal FPS grid at finalize.
//
// [WithMaxFrames] limits in-memory GIF capture to the last n palettized frames
// (after dedup when [OptimizeFrames] is enabled). Dropped frame delays are not
// carried forward; disposal optimization runs on the retained window only.
//
// [WithLoopCount] sets how many times a GIF animation repeats. The default,
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
// and glyph bodies) get exact entries first. Antialiased glyph edges get exact
// entries while slots remain, otherwise median cut picks representatives along
// each blend, which keeps edges on the foreground's hue. Once every slot is
// used, new colors map to the nearest existing entry. PNG with
// [OptimizeColorQuantization] uses the same palette builder on its single
// image.
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
package export
