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

// GIFEncoder is a [Sink] that writes an animated GIF. It keeps the frames
// palettized in memory and encodes the GIF on [GIFEncoder.Close], as the
// palette and disposal passes need the whole recording.
//
// It reads [WithOptimize], [WithBackground], [WithLoopCount], [WithMaxFrames],
// and [WithMaxBytes]. Other options are ignored.
type GIFEncoder struct {
	opts options
	rec  *gifRecording
	// file, when set, is the temporary file behind w.
	w    io.Writer
	file *pendingFile
	// err is the first frame error; once set, Close writes nothing.
	err  error
	done bool
}

var _ Sink = (*GIFEncoder)(nil)

// NewGIF returns an encoder that writes the GIF to w on [GIFEncoder.Close].
func NewGIF(w io.Writer, opts ...Option) (*GIFEncoder, error) {
	if w == nil {
		return nil, errors.New("sink: nil writer")
	}
	cfg, err := gifOptions(opts)
	if err != nil {
		return nil, err
	}
	return newGIFEncoder(w, cfg), nil
}

// CreateGIF returns an encoder that writes the GIF to path. It is written to a
// temporary file beside path, which [GIFEncoder.Close] renames into place.
func CreateGIF(path string, opts ...Option) (*GIFEncoder, error) {
	cfg, err := gifOptions(opts)
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
	return &GIFEncoder{
		opts: cfg,
		w:    w,
		rec: &gifRecording{
			loopCount: cfg.loopCount,
			maxFrames: cfg.maxFrames,
			maxBytes:  cfg.maxBytes,
		},
	}
}

// WriteFrame palettizes f and adds it to the recording. f.Delay is rounded to
// whole centiseconds, at least one.
func (e *GIFEncoder) WriteFrame(f Frame) error {
	switch {
	case e.done:
		return errors.New("sink: write to closed gif encoder")
	case e.err != nil:
		return e.err
	}
	e.err = appendRecordingFrame(e.rec, e.opts.optimize, e.opts.background, f.Isolated, f.Image, f.Delay)
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

func encodeGIF(w io.Writer, rec *gifRecording, opts options) error {
	// The palette most captured frames use is the global color table; frames
	// on any other carry a local one.
	global := busiestPalette(rec.images)
	if opts.optimize.has(OptimizeDirtyRects) {
		optimizeDisposals(rec)
	}
	out := buildGIF(rec, global)
	if opts.optimize.has(OptimizeColorQuantization) {
		palette.CompactGIF(out)
	}
	return gif.EncodeAll(w, out)
}

// optimizeDisposals rewrites rec in place with [gifopt.Optimize]. A single
// frame only gets its disposal method, and an empty canvas is left alone.
func optimizeDisposals(rec *gifRecording) {
	n := len(rec.images)
	if n <= 1 {
		if n == 1 && len(rec.disposal) == 0 {
			rec.disposal = []byte{gif.DisposalNone}
		}
		return
	}
	screen := image.Rectangle{Max: rec.size}
	if screen.Empty() {
		return
	}
	deltaAt := -1
	if rec.last != nil {
		deltaAt = rec.deltaAt
	}
	rec.images, rec.delays, rec.disposal = gifopt.Optimize(screen, rec.images, rec.delays, deltaAt)
	rec.last, rec.spare = nil, nil
}

// gifRecording holds the palettized frames of a GIF capture.
type gifRecording struct {
	loopCount int
	// size is the canvas size, which the newest frame sets.
	size   image.Point
	images []*image.Paletted
	delays []time.Duration
	// bytes is the pixel storage held by images, not counting last or spare.
	bytes int64
	// maxFrames and maxBytes limit len(images) and bytes; 0 is no limit.
	maxFrames int
	maxBytes  int64
	// disposal is set by [optimizeDisposals].
	disposal []byte
	// last is the newest full frame and spare a recycled capture buffer. When
	// last is set, the recording is in delta mode: images[deltaAt] is the full
	// base of the current segment, later images are crops of what changed
	// since the frame before, and earlier images are full frames.
	last, spare *image.Paletted
	deltaAt     int
	// adaptive is the shared palette, and isolated the palette of the current
	// run of [Frame.Isolated] frames (nil outside one).
	adaptive *palette.Adaptive
	isolated *palette.Adaptive
}

// fullFrames returns every recorded frame as a full image, which is
// rec.images itself outside delta mode.
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

// endDeltaSegment rebuilds the current delta segment as full frames and
// leaves delta mode.
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

// capturePalette returns the palette builder for the next frame. A run of
// isolated frames shares one builder, with no reserve.
func (rec *gifRecording) capturePalette(isolated bool) *palette.Adaptive {
	if !isolated {
		rec.isolated = nil
		if rec.adaptive == nil {
			rec.adaptive = palette.NewAdaptive(true, true)
		}
		return rec.adaptive
	}
	if rec.isolated == nil {
		rec.isolated = palette.NewAdaptive(false, false)
	}
	return rec.isolated
}

// truncateRecording drops the oldest frames until the recording fits
// rec.maxFrames and rec.maxBytes, always keeping the newest. The base of a
// delta segment is never dropped: the deltas after it that are dropped are
// folded into it instead, so it stays a full frame.
func truncateRecording(rec *gifRecording) {
	n := len(rec.images)
	if n < 2 || (rec.maxFrames <= 0 && rec.maxBytes <= 0) {
		return
	}
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

func (rec *gifRecording) push(img *image.Paletted) {
	rec.images = append(rec.images, img)
	rec.bytes += int64(len(img.Pix))
}

func (rec *gifRecording) recount() {
	rec.bytes = 0
	for _, img := range rec.images {
		rec.bytes += int64(len(img.Pix))
	}
}

// appendRecordingFrame palettizes frame into storage rec owns and appends it,
// merging it into the previous frame when identical and [OptimizeFrames] is
// set.
func appendRecordingFrame(
	rec *gifRecording,
	optimize OptimizeFlags,
	background color.Color,
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

	rec.size = bounds.Size()

	adaptive := rec.capturePalette(isolated)
	pm := rec.spare
	rec.spare = nil
	if pm == nil || pm.Rect != bounds {
		pm = image.NewPaletted(bounds, nil)
	}
	pm.Palette = adaptive.Palette()
	if background != nil {
		frame = compositeBackgroundNRGBA(asNRGBA(frame), background)
	}
	adaptive.Palettize(pm, frame) // replaces pm.Palette if adaptive rolls over

	// Indices on different palettes cannot be diffed, so a palette change
	// starts a new segment. A canvas size change ends delta mode for good.
	startSegment := len(rec.images) == 0
	if prev := rec.activePalette(); prev != nil && !palette.Same(prev, pm.Palette) {
		rec.endDeltaSegment()
		startSegment = true
	}
	if rec.last != nil && rec.last.Rect != bounds {
		rec.endDeltaSegment()
		startSegment = false
	}
	// With dedup and dirty rects both on, only a segment's first frame is
	// stored in full and later frames keep the bbox that changed.
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

// asNRGBA returns img as *image.NRGBA, converting it unless it is one.
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

// buildGIF converts rec into a gif.GIF with global as its global color
// table.
func buildGIF(rec *gifRecording, global color.Palette) *gif.GIF {
	images, delays, disposals := splitLongDelays(rec.images, quantizeDelays(rec.delays), rec.disposal)
	return &gif.GIF{
		LoopCount: rec.loopCount,
		Config:    image.Config{Width: rec.size.X, Height: rec.size.Y, ColorModel: global},
		Image:     images,
		Delay:     delays,
		Disposal:  disposals,
	}
}

// maxGIFCentiseconds is the largest delay GIF can store in one frame.
const maxGIFCentiseconds = 65535

// quantizeDelays rounds each delay to whole centiseconds, at least one.
// Values above [maxGIFCentiseconds] are left for [splitLongDelays].
func quantizeDelays(delays []time.Duration) []int {
	cs := make([]int, len(delays))
	for i, d := range delays {
		cs[i] = max(1, int(math.Round(float64(d)/float64(10*time.Millisecond))))
	}
	return cs
}

// splitLongDelays splits a DisposalNone frame whose delay is longer than GIF
// can store into the frame plus 1×1 transparent continuations. Other disposal
// methods are clamped instead, since a continuation would fire the disposal
// before the hold finishes.
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

// holdFrame is a 1×1 transparent patch on src's palette, which adds delay
// without changing the canvas.
func holdFrame(src *image.Paletted) *image.Paletted {
	p := image.NewPaletted(image.Rect(0, 0, 1, 1), src.Palette)
	p.Pix[0] = palette.TransparentIndex
	return p
}

// opaqueNRGBA converts c to NRGBA with its alpha dropped.
func opaqueNRGBA(c color.Color) color.NRGBA {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff} //nolint:gosec // RGBA contract.
}

// compositeBackgroundNRGBA returns a copy of src with fully transparent pixels
// replaced by bg.
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
