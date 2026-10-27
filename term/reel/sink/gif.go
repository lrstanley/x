// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel/sink/internal/gifopt"
	"github.com/lrstanley/x/term/reel/sink/internal/palette"
)

// TransparentPaletteIndex is the palette index reserved for transparency during
// GIF capture and optimization. See package documentation for the full invariant.
const TransparentPaletteIndex = palette.TransparentIndex

// GIFEncoder is an [Encoder] that writes an animated GIF. It keeps every frame
// palettized in memory, deduplicating and diffing them as they arrive, and
// encodes the GIF on [GIFEncoder.Close]: the global palette, disposal, and
// palette compaction passes need the whole recording.
//
// It reads [WithOptimize], [WithPalette], [WithBackground], [WithLoopCount],
// [WithMaxFrames], and [WithMaxBytes]. Other options are ignored.
type GIFEncoder struct {
	opts options
	// palette is the capture palette; custom when WithPalette is set, otherwise
	// the adaptive palette built from captured colors.
	palette color.Palette
	rec     *gifRecording
	// w receives the GIF on Close. file, when set, is the temporary file
	// behind w, which Close moves into place.
	w    io.Writer
	file *pendingFile
	// err is the first frame error; once set, frames are rejected and Close
	// writes nothing.
	err  error
	done bool
}

var _ Sink = (*GIFEncoder)(nil)

// NewGIF returns an encoder that writes the GIF to w on [GIFEncoder.Close].
func NewGIF(w io.Writer, opts ...Option) (*GIFEncoder, error) {
	if w == nil {
		return nil, errors.New("sink: nil writer")
	}
	cfg, err := collectOptions(validateGIF, opts...)
	if err != nil {
		return nil, err
	}
	return newGIFEncoder(w, cfg), nil
}

// CreateGIF returns an encoder that writes the GIF to path. It is written to a
// temporary file beside path, which [GIFEncoder.Close] renames into place, so
// a failed or discarded recording leaves an existing file alone.
func CreateGIF(path string, opts ...Option) (*GIFEncoder, error) {
	cfg, err := collectOptions(validateGIF, opts...)
	if err != nil {
		return nil, err
	}
	file, err := createPending(path)
	if err != nil {
		return nil, fmt.Errorf("sink: creating gif output: %w", err)
	}
	e := newGIFEncoder(file.f, cfg)
	e.file = file
	return e, nil
}

func newGIFEncoder(w io.Writer, cfg options) *GIFEncoder {
	var adaptive *palette.Adaptive
	pal := cfg.palette
	if !cfg.paletteSet {
		adaptive = palette.NewAdaptive(true, true)
		pal = adaptive.Palette()
	}
	return &GIFEncoder{
		opts:    cfg,
		palette: pal,
		w:       w,
		rec: &gifRecording{
			loopCount: cfg.loopCount,
			config:    image.Config{ColorModel: pal},
			adaptive:  adaptive,
			maxFrames: cfg.maxFrames,
			maxBytes:  cfg.maxBytes,
		},
	}
}

// WriteFrame palettizes f and adds it to the recording. f.Delay is rounded to
// whole centiseconds (at least one) when the GIF is encoded; a frame identical
// to the previous one only adds its delay to it.
func (e *GIFEncoder) WriteFrame(f Frame) error {
	switch {
	case e.done:
		return errors.New("sink: write to closed gif encoder")
	case e.err != nil:
		return e.err
	}
	e.err = appendRecordingFrame(
		e.rec, e.palette, false, e.opts.optimize,
		e.opts.background, e.opts.backgroundSet, f.Isolated, f.Image, f.Delay,
	)
	return e.err
}

// Close encodes the GIF. With no frames written, or after a frame error, it
// writes nothing. Later calls return the same result.
func (e *GIFEncoder) Close() error {
	if e.done {
		return e.err
	}
	e.done = true
	if e.err == nil && len(e.rec.images) > 0 {
		e.err = encodeGIF(e.w, e.rec, e.opts)
		if e.err == nil && e.file != nil {
			e.err = e.file.commit()
		}
	}
	if e.file != nil && (e.err != nil || len(e.rec.images) == 0) {
		_ = e.file.abort()
	}
	return e.err
}

// Discard drops the recording without writing anything. It returns nil after
// [GIFEncoder.Close] has run.
func (e *GIFEncoder) Discard() error {
	if e.done {
		return nil
	}
	e.done = true
	if e.file != nil {
		return e.file.abort()
	}
	return nil
}

// encodeGIF runs the palette, disposal, and transparency passes over rec and
// writes the GIF to w.
func encodeGIF(w io.Writer, rec *gifRecording, opts options) error {
	if !opts.paletteSet {
		// Frames on any other palette carry a local color table, so the
		// global one goes to the palette the most frames use.
		rec.config.ColorModel = busiestPalette(rec.images)
	}
	if opts.optimize.has(OptimizeDirtyRects) {
		optimizeDisposals(rec, opts.background, opts.backgroundSet)
	}
	out := buildGIF(rec)
	if opts.optimize.has(OptimizeColorQuantization) {
		palette.CompactGIF(out, opts.paletteSet)
	}
	palette.MarkTransparent(out)
	return gif.EncodeAll(w, out)
}

// optimizeDisposals rewrites rec in place with [gifopt.Optimize]. A single
// frame only gets its disposal method, and an empty canvas is left alone.
func optimizeDisposals(rec *gifRecording, bg color.Color, bgSet bool) {
	n := len(rec.images)
	if n <= 1 {
		if n == 1 && len(rec.disposal) == 0 {
			rec.disposal = []byte{gif.DisposalNone}
		}
		return
	}
	w, h := rec.config.Width, rec.config.Height
	if w <= 0 || h <= 0 {
		return
	}
	deltaAt := -1
	if rec.last != nil {
		deltaAt = rec.deltaAt
	}
	rec.images, rec.delays, rec.disposal, rec.backgroundIndex = gifopt.Optimize(
		image.Rect(0, 0, w, h), palette.From(rec.config.ColorModel, rec.images),
		rec.images, rec.delays, deltaAt, bg, bgSet,
	)
	rec.last, rec.spare = nil, nil
}

// gifRecording accumulates palettized frames and duration delays during capture.
// Centisecond conversion happens at finalize via [quantizeDelays].
type gifRecording struct {
	// loopCount is the GIF loop count copied to the encoded output.
	loopCount int
	// config holds canvas dimensions and the capture palette.
	config image.Config
	// images are palettized frames accumulated during capture.
	images []*image.Paletted
	// delays are per-frame durations accumulated before centisecond conversion.
	delays []time.Duration
	// bytes is the pixel storage held by images. It is what maxBytes limits, and
	// counts neither last nor spare.
	bytes int64
	// maxFrames and maxBytes cap len(images) and bytes during capture; 0 is
	// unlimited for each.
	maxFrames int
	maxBytes  int64
	// disposal is per-frame disposal metadata; set by [optimizeDisposals].
	disposal []byte
	// backgroundIndex is the disposal background palette index; set by
	// [optimizeDisposals].
	backgroundIndex uint8
	// last is the newest full frame and spare a recycled capture buffer. When
	// last is set, images[deltaAt] is the full base of the current segment and
	// later images are bbox crops of what changed since the previous frame
	// (see [appendRecordingFrame]). Images before deltaAt are full frames.
	last, spare *image.Paletted
	// deltaAt is the index of the current delta base when last is set. It stays
	// 0 for a recording that never changes palette, which stores one segment
	// from the first frame.
	deltaAt int
	// adaptive grows config.ColorModel from captured colors when no capture
	// palette was given. Nil uses the palette passed to [appendRecordingFrame].
	adaptive *palette.Adaptive
	// isolated is the palette of the current run of [Frame.Isolated] frames.
	// It is nil while the newest frame uses the shared palette, and it never
	// adds colors to adaptive.
	isolated *palette.Adaptive
}

// fullFrames returns every recorded frame as a full image. Delta recordings
// rebuild the current segment from its base; earlier images are already full.
// Recordings outside delta mode return rec.images itself.
func (rec *gifRecording) fullFrames() []*image.Paletted {
	if rec.last == nil {
		return rec.images
	}
	full := make([]*image.Paletted, len(rec.images))
	copy(full, rec.images[:rec.deltaAt])
	for i := rec.deltaAt; i < len(rec.images); i++ {
		img := rec.images[i]
		if i > rec.deltaAt {
			prev := img
			img = gifopt.Clone(full[i-1])
			gifopt.BlitOpaque(prev, img)
		}
		full[i] = img
	}
	return full
}

// endDeltaSegment materializes the current delta segment into full frames and
// leaves delta mode. A new segment starts when the next frame is stored as a base.
func (rec *gifRecording) endDeltaSegment() {
	if rec.last == nil {
		return
	}
	rec.images = rec.fullFrames()
	rec.last = nil
	rec.spare = nil
	rec.recount()
}

// activePalette is the palette of the newest stored frame, or nil when the
// recording is empty.
func (rec *gifRecording) activePalette() color.Palette {
	if rec.last != nil {
		return rec.last.Palette
	}
	if n := len(rec.images); n > 0 {
		return rec.images[n-1].Palette
	}
	return nil
}

// capturePalette returns the palette and, when it is adaptive, the builder
// for this frame. Isolated frames use a palette of their own, sized with no
// reserve, and a run of them reuses one builder.
func (rec *gifRecording) capturePalette(shared color.Palette, isolated bool) (color.Palette, *palette.Adaptive) {
	if !isolated {
		rec.isolated = nil
		if rec.adaptive != nil {
			return rec.adaptive.Palette(), rec.adaptive
		}
		return shared, nil
	}
	if rec.isolated == nil {
		rec.isolated = palette.NewAdaptive(false, false)
	}
	return rec.isolated.Palette(), rec.isolated
}

// truncateRecording drops the oldest stored frames until the recording fits
// rec.maxFrames and rec.maxBytes (either may be 0 for no limit). The newest
// frame is always kept, so a single frame larger than the byte limit stays.
// Delta frames are folded into the segment base so that base stays full.
func truncateRecording(rec *gifRecording) {
	n := len(rec.images)
	if n < 2 || (rec.maxFrames <= 0 && rec.maxBytes <= 0) {
		return
	}
	// The base of a delta segment is never dropped; it absorbs the deltas after
	// it. Full frames before that base are the oldest and drop first. The
	// newest frame is always kept.
	excess, freed := 0, int64(0)
	for excess < n-1 {
		overFrames := rec.maxFrames > 0 && n-excess > rec.maxFrames
		overBytes := rec.maxBytes > 0 && rec.bytes-freed > rec.maxBytes
		if !overFrames && !overBytes {
			break
		}
		idx := excess
		if rec.last != nil && excess >= rec.deltaAt {
			idx = excess + 1 // skip the base
		}
		freed += int64(len(rec.images[idx].Pix))
		excess++
	}
	if excess == 0 {
		return
	}
	if rec.last != nil {
		pre := min(excess, rec.deltaAt)
		folded := excess - pre
		if pre > 0 {
			rec.images = slices.Delete(rec.images, 0, pre)
			rec.deltaAt -= pre
		}
		if folded > 0 {
			base := rec.deltaAt
			for _, delta := range rec.images[base+1 : base+1+folded] {
				gifopt.BlitOpaque(delta, rec.images[base])
			}
			rec.images = slices.Delete(rec.images, base+1, base+1+folded)
		}
	} else {
		rec.images = rec.images[excess:]
	}
	rec.delays = rec.delays[excess:]
	rec.bytes -= freed
}

// push stores img as the newest frame.
func (rec *gifRecording) push(img *image.Paletted) {
	rec.images = append(rec.images, img)
	rec.bytes += int64(len(img.Pix))
}

// recount recomputes rec.bytes from rec.images.
func (rec *gifRecording) recount() {
	rec.bytes = 0
	for _, img := range rec.images {
		rec.bytes += int64(len(img.Pix))
	}
}

// appendRecordingFrame palettizes frame into owned output storage, optionally
// deduplicates identical consecutive frames, and appends to rec. An isolated
// frame is mapped through its own adaptive palette and does not add colors to
// the shared one. A run of isolated frames shares that palette.
func appendRecordingFrame(
	rec *gifRecording,
	pal color.Palette,
	useLUT bool,
	optimize OptimizeFlags,
	background color.Color,
	backgroundSet bool,
	isolated bool,
	frame image.Image,
	delay time.Duration,
) error {
	if frame == nil {
		return errNilFrame
	}
	bounds := frame.Bounds()
	if bounds.Empty() {
		return errEmptyFrame
	}

	if rec.config.Width != bounds.Dx() || rec.config.Height != bounds.Dy() {
		rec.config.Width = bounds.Dx()
		rec.config.Height = bounds.Dy()
	}
	if rec.config.ColorModel == nil {
		rec.config.ColorModel = pal
	}

	pal, adaptive := rec.capturePalette(pal, isolated)
	pm := rec.spare
	rec.spare = nil
	if pm == nil || pm.Rect != bounds {
		pm = image.NewPaletted(bounds, pal)
	}
	pm.Palette = pal
	palettizeCaptureFrame(pm, frame, adaptive, useLUT, background, backgroundSet)

	// A palette change closes the open segment and this frame starts a new one,
	// so later diffs never compare indices across palettes. A canvas size change
	// closes the segment and does not start another: deltas cannot span canvases.
	startSegment := len(rec.images) == 0
	if prev := rec.activePalette(); prev != nil && !palette.Same(prev, pm.Palette) {
		rec.endDeltaSegment()
		startSegment = true
	}
	if rec.last != nil && rec.last.Rect != bounds {
		rec.endDeltaSegment()
		startSegment = false
	}
	// With dedupe and dirty-rect optimization both on, only the first frame of
	// a segment is stored in full; later frames keep the bbox that changed,
	// and a frame with no changes only adds its delay. pm is recycled for the
	// next capture.
	if optimize.has(OptimizeFrames) && optimize.has(OptimizeDirtyRects) && (rec.last != nil || startSegment) {
		if rec.last == nil {
			rec.deltaAt = len(rec.images)
			rec.push(pm)
			rec.last = gifopt.Clone(pm)
		} else {
			bbox, diff := gifopt.Diff(rec.last, pm, bounds)
			if diff == 0 {
				rec.delays[len(rec.delays)-1] += delay
				rec.spare = pm
				return nil
			}
			rec.push(gifopt.Crop(pm, bbox))
			rec.last, rec.spare = pm, rec.last
		}
		rec.delays = append(rec.delays, delay)
		truncateRecording(rec)
		return nil
	}

	if optimize.has(OptimizeFrames) {
		n := len(rec.images)
		if n > 0 && palette.Same(rec.images[n-1].Palette, pm.Palette) && bytes.Equal(rec.images[n-1].Pix, pm.Pix) {
			rec.delays[n-1] += delay
			return nil
		}
	}

	rec.push(pm)
	rec.delays = append(rec.delays, delay)
	truncateRecording(rec)
	return nil
}

// palettizeCaptureFrame writes src into pm, compositing onto background when
// configured. It uses adaptive when non-nil, otherwise the LUT or draw.Draw
// path per useLUT. An adaptive palette that rolls over replaces pm.Palette.
func palettizeCaptureFrame(pm *image.Paletted, frame image.Image, adaptive *palette.Adaptive, useLUT bool, background color.Color, backgroundSet bool) {
	src := frame
	if backgroundSet {
		src = compositeBackgroundNRGBA(asNRGBA(frame), background)
	}
	switch {
	case adaptive != nil:
		adaptive.Palettize(pm, src)
		return
	case useLUT:
		palette.Palettize(pm, src)
		return
	}
	draw.Draw(pm, frame.Bounds(), src, src.Bounds().Min, draw.Src)
}

// asNRGBA returns img as *image.NRGBA. The render package returns NRGBA; foreign
// types are converted once at the capture boundary via draw.Draw.
func asNRGBA(img image.Image) *image.NRGBA {
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

// busiestPalette returns the palette the most frames use.
func busiestPalette(frames []*image.Paletted) color.Palette {
	groups := palette.Groups(frames)
	best := groups[0]
	for _, g := range groups[1:] {
		if len(g.Frames) > len(best.Frames) {
			best = g
		}
	}
	return best.Palette
}

// buildGIF converts a finalized recording into a gif.GIF with its delays in
// centiseconds.
func buildGIF(rec *gifRecording) *gif.GIF {
	images, delays, disposals := splitLongDelays(rec.images, quantizeDelays(rec.delays), rec.disposal)
	return &gif.GIF{
		LoopCount:       rec.loopCount,
		Config:          rec.config,
		Image:           images,
		Delay:           delays,
		Disposal:        disposals,
		BackgroundIndex: rec.backgroundIndex,
	}
}

// maxGIFCentiseconds is the largest delay GIF can store in one frame.
const maxGIFCentiseconds = 65535

// quantizeDelays rounds each delay to whole centiseconds, at least one. The
// frames of a reel export are on its delay grid already, so this only rounds
// delays from other callers. Values above
// [maxGIFCentiseconds] are split into continuation frames by [splitLongDelays].
func quantizeDelays(delays []time.Duration) []int {
	cs := make([]int, len(delays))
	for i, d := range delays {
		cs[i] = max(1, int(math.Round(float64(d)/float64(10*time.Millisecond))))
	}
	return cs
}

// splitLongDelays breaks delays longer than one GIF frame into a chain.
// DisposalNone holds keep the canvas with a 1×1 transparent continuation, so
// a static recording longer than ~655s does not lose time. Other disposal
// methods stay on their original frame and are clamped; extending them would
// fire disposal before the hold finishes.
func splitLongDelays(images []*image.Paletted, delays []int, disposals []byte) (outImages []*image.Paletted, outDelays []int, outDisposals []byte) {
	extra := 0
	for _, d := range delays {
		if d > maxGIFCentiseconds {
			extra += (d+maxGIFCentiseconds-1)/maxGIFCentiseconds - 1
		}
	}
	if extra == 0 {
		return images, delays, disposals
	}

	outImages = make([]*image.Paletted, 0, len(images)+extra)
	outDelays = make([]int, 0, len(images)+extra)
	outDisposals = make([]byte, 0, len(images)+extra)
	for i, d := range delays {
		img := images[i]
		disp := byte(gif.DisposalNone)
		if i < len(disposals) {
			disp = disposals[i]
		}
		if d <= maxGIFCentiseconds || disp != gif.DisposalNone {
			if d > maxGIFCentiseconds {
				d = maxGIFCentiseconds
			}
			outImages = append(outImages, img)
			outDelays = append(outDelays, d)
			outDisposals = append(outDisposals, disp)
			continue
		}
		first := true
		for d > 0 {
			n := min(d, maxGIFCentiseconds)
			frame := img
			if !first {
				frame = holdFrame(img)
			}
			outImages = append(outImages, frame)
			outDelays = append(outDelays, n)
			outDisposals = append(outDisposals, gif.DisposalNone)
			d -= n
			first = false
		}
	}
	return outImages, outDelays, outDisposals
}

// holdFrame is a 1×1 transparent patch on src's own palette. With DisposalNone
// it adds delay without changing the canvas. Palettes without a transparent
// slot at [TransparentPaletteIndex] repeat the full frame instead, so an
// isolated palette only keeps that slot when a later frame on it uses one.
func holdFrame(src *image.Paletted) *image.Paletted {
	if src == nil || len(src.Palette) <= int(TransparentPaletteIndex) {
		return src
	}
	p := image.NewPaletted(image.Rect(0, 0, 1, 1), src.Palette)
	p.Pix[0] = TransparentPaletteIndex
	return p
}

// opaqueNRGBA converts c to NRGBA with its alpha dropped, for use as a
// background.
func opaqueNRGBA(c color.Color) color.NRGBA {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff} //nolint:gosec // RGBA contract.
}

// compositeBackgroundNRGBA returns an NRGBA copy of src with fully transparent
// pixels replaced by bg. src must be NRGBA; use [asNRGBA] at API boundaries.
func compositeBackgroundNRGBA(src *image.NRGBA, bg color.Color) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	c := opaqueNRGBA(bg)
	width := b.Dx()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		si := (y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (b.Min.X-dst.Rect.Min.X)*4
		srcRow := src.Pix[si : si+width*4]
		dstRow := dst.Pix[di : di+width*4]
		for x := range width {
			sx := x * 4
			if srcRow[sx+3] == 0 {
				dstRow[sx] = c.R
				dstRow[sx+1] = c.G
				dstRow[sx+2] = c.B
				dstRow[sx+3] = 0xff
				continue
			}
			dstRow[sx] = srcRow[sx]
			dstRow[sx+1] = srcRow[sx+1]
			dstRow[sx+2] = srcRow[sx+2]
			dstRow[sx+3] = srcRow[sx+3]
		}
	}
	return dst
}
