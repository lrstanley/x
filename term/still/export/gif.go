// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

// FrameRecorder captures GIF frames, then finalizes and encodes via [FrameRecorder.EncodeAll].
// Call [FrameRecorder.Close] to stop capture goroutines; encoding does not happen on close.
type FrameRecorder interface {
	// Frames returns frames captured so far (palettized images and duration delays).
	Frames() []Frame
	// EncodeAll runs disposal, palette, and transparency passes, then writes the GIF to w.
	// w must be non-nil.
	EncodeAll(w io.Writer) error
	// Close stops capture and waits for background goroutines. Does not encode.
	Close() error
}

// GIF returns a new GIF-based [FrameRecorder].
func GIF(opts ...Option) (FrameRecorder, error) {
	cfg, err := collectOptions(formatGIF, opts...)
	if err != nil {
		return nil, err
	}
	rec := newGIFRecorder(cfg)
	rec.start()
	return rec, nil
}

// WriteGIF records animated frames for encoding to w. The returned closer calls
// [FrameRecorder.Close] then [FrameRecorder.EncodeAll] with w; later calls are no-op.
func WriteGIF(w io.Writer, opts ...Option) (closer func() error, err error) {
	if w == nil {
		return nil, errors.New("export: nil writer")
	}
	cfg, err := collectOptions(formatGIF, opts...)
	if err != nil {
		return nil, err
	}
	rec := newGIFRecorder(cfg)
	rec.start()
	return rec.finishCloser(w), nil
}

// MustGIF is a convenience wrapper around [GIF] that panics on setup, encode, or close
// errors. The returned function closes capture, then encodes to path.
func MustGIF(path string, opts ...Option) func() {
	rec, err := GIF(opts...)
	if err != nil {
		panic(err)
	}
	return func() {
		if err := rec.Close(); err != nil {
			panic(err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := rec.EncodeAll(f); err != nil {
			panic(err)
		}
	}
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
	// disposal is per-frame disposal metadata; set by [optimizeDisposals].
	disposal []byte
	// backgroundIndex is the disposal background palette index; set by
	// [optimizeDisposals].
	backgroundIndex uint8
}

// gifRecorder captures palettized frames into a [gifRecording].
type gifRecorder struct {
	// opts is the validated export configuration for this recording session.
	opts options
	// palette is the capture palette; custom when WithPalette is set, otherwise
	// the default internal xterm-256 palette.
	palette color.Palette
	// useLUT enables the fast LUT palettize path when palette is the default.
	useLUT bool
	// rec accumulates frames and duration delays until finalize and encode.
	rec *gifRecording
	// lastCapture is wall time of the previous capture attempt, used to derive
	// non-strict frame delays and deduped idle accumulation.
	lastCapture time.Time
	// done is closed by Close to signal capture goroutines to stop.
	done chan struct{}
	// wg waits for capture goroutines during close.
	wg sync.WaitGroup
	// closeErr stores the first capture or validation error.
	closeErr error
	// once ensures done is closed and wg.Wait runs exactly once on close.
	once sync.Once
	// encodeOnce ensures EncodeAll runs finalize and write exactly once.
	encodeOnce sync.Once
	// encoded records whether EncodeAll completed (including a no-frame encode).
	encoded bool
	// captureMu protects capturing for strict-mode tick skipping.
	captureMu sync.Mutex
	// capturing is true while a strict-mode frame function call is in progress.
	capturing bool
}

var _ FrameRecorder = (*gifRecorder)(nil)

// newGIFRecorder builds a recorder from validated options, selecting the
// capture palette and LUT vs draw.Draw palettize path.
func newGIFRecorder(cfg options) *gifRecorder {
	pal := defaultGIFPalette()
	useLUT := true
	if cfg.paletteSet {
		pal = cfg.palette
		useLUT = false
	}
	return &gifRecorder{
		opts:    cfg,
		palette: pal,
		useLUT:  useLUT,
		rec: &gifRecording{
			loopCount: cfg.loopCount,
			config: image.Config{
				ColorModel: pal,
			},
		},
		done: make(chan struct{}),
	}
}

// start launches the frame-rate ticker goroutine and/or channel reader.
func (r *gifRecorder) start() {
	if r.opts.frameRateSet {
		r.wg.Go(r.runFrameRate)
	}
	if r.opts.channelSet {
		r.wg.Go(r.runChannel)
	}
}

// Frames returns captured frames and their duration delays.
func (r *gifRecorder) Frames() []Frame {
	frames := make([]Frame, len(r.rec.images))
	for i, img := range r.rec.images {
		var delay time.Duration
		if i < len(r.rec.delays) {
			delay = r.rec.delays[i]
		}
		frames[i] = Frame{Image: img, Delay: delay}
	}
	return frames
}

// EncodeAll finalizes optimizations and writes the GIF to w.
func (r *gifRecorder) EncodeAll(w io.Writer) error {
	var encodeErr error
	r.encodeOnce.Do(func() {
		if w == nil {
			encodeErr = errors.New("export: nil writer")
			return
		}
		if r.opts.optimize.has(OptimizeDirtyRects) && len(r.rec.images) > 0 {
			optimizeDisposals(r.rec, r.opts.background, r.opts.backgroundSet)
		}
		out := buildGIF(r.rec, r.opts)
		if r.opts.optimize.has(OptimizeColorQuantization) && !r.opts.paletteSet && len(out.Image) > 0 {
			optimizePalette(out)
		}
		if len(out.Image) > 0 {
			ensureTransparentPaletteSlot(out)
		}
		encodeErr = gif.EncodeAll(w, out)
		r.encoded = true
	})
	if encodeErr != nil {
		return encodeErr
	}
	return r.closeErr
}

// Close stops capture goroutines. Does not encode or close the destination file.
func (r *gifRecorder) Close() error {
	r.once.Do(func() {
		close(r.done)
		r.wg.Wait()
	})
	return r.closeErr
}

// finishCloser returns the idempotent stop-and-encode callback used by [WriteGIF].
func (r *gifRecorder) finishCloser(w io.Writer) func() error {
	var finishOnce sync.Once
	var finishErr error
	return func() error {
		finishOnce.Do(func() {
			if err := r.Close(); err != nil {
				finishErr = err
				return
			}
			finishErr = r.EncodeAll(w)
		})
		return finishErr
	}
}

// runFrameRate captures frames from the WithFrameRate callback on each tick
// until done is closed.
func (r *gifRecorder) runFrameRate() {
	fps := r.opts.frameRate
	tick := time.NewTicker(time.Second / time.Duration(fps))
	defer tick.Stop()

	r.lastCapture = time.Now()
	for {
		select {
		case <-tick.C:
			if r.opts.frameRateStrict && !r.tryBeginCapture() {
				continue
			}
			r.captureTick()
			if r.opts.frameRateStrict {
				r.endCapture()
			}
		case <-r.done:
			tick.Stop()
			return
		}
	}
}

// tryBeginCapture returns false when a strict-mode tick arrives while the
// previous capture is still running (no backlog).
func (r *gifRecorder) tryBeginCapture() bool {
	r.captureMu.Lock()
	defer r.captureMu.Unlock()
	if r.capturing {
		return false
	}
	r.capturing = true
	return true
}

// endCapture marks strict-mode capture complete so the next tick may run.
func (r *gifRecorder) endCapture() {
	r.captureMu.Lock()
	r.capturing = false
	r.captureMu.Unlock()
}

// runChannel reads WithChannel frames until the channel closes or done
// is signaled. When done is signaled, frames already buffered are still kept.
func (r *gifRecorder) runChannel() {
	r.lastCapture = time.Now()
	ch := r.opts.channel
	for {
		select {
		case <-r.done:
			r.drainChannel(ch)
			return
		case f, ok := <-ch:
			if !ok {
				return
			}
			r.captureChannelFrame(f)
		}
	}
}

// drainChannel records frames already buffered in ch without blocking for more.
// A sender that fills the channel and then calls [FrameRecorder.Close] (like
// closing ch first) would otherwise lose whatever the reader had not yet
// palettized, since select picks randomly between done and a ready channel.
func (r *gifRecorder) drainChannel(ch <-chan Frame) {
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return
			}
			r.captureChannelFrame(f)
		default:
			return
		}
	}
}

// captureTick records one WithFrameRate tick.
func (r *gifRecorder) captureTick() {
	r.palettizeAndAppend(r.opts.frameRateFn(), r.nextDelay(0))
}

// captureChannelFrame records one WithChannel frame.
func (r *gifRecorder) captureChannelFrame(f Frame) {
	r.palettizeAndAppend(f.Image, r.nextDelay(f.Delay))
}

// truncateRecording drops the oldest stored frames when len(rec.images) exceeds maxFrames.
func truncateRecording(rec *gifRecording, maxFrames int) {
	if maxFrames <= 0 {
		return
	}
	if excess := len(rec.images) - maxFrames; excess > 0 {
		rec.images = rec.images[excess:]
		rec.delays = rec.delays[excess:]
	}
}

// appendRecordingFrame palettizes frame into owned output storage, optionally
// deduplicates identical consecutive frames, and appends to rec.
func appendRecordingFrame(
	rec *gifRecording,
	palette color.Palette,
	useLUT bool,
	optimize OptimizeFlags,
	background color.Color,
	backgroundSet bool,
	frame image.Image,
	delay time.Duration,
	maxFrames int,
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
		rec.config.ColorModel = palette
	}

	pm := image.NewPaletted(bounds, palette)
	palettizeCaptureFrame(pm, frame, useLUT, background, backgroundSet)

	if optimize.has(OptimizeFrames) {
		if n := len(rec.images); n > 0 && bytes.Equal(rec.images[n-1].Pix, pm.Pix) {
			rec.delays[n-1] += delay
			return nil
		}
	}

	rec.images = append(rec.images, pm)
	rec.delays = append(rec.delays, delay)
	truncateRecording(rec, maxFrames)
	return nil
}

// palettizeAndAppend converts frame to palettized storage and appends to rec,
// storing validation errors in closeErr.
func (r *gifRecorder) palettizeAndAppend(frame image.Image, delay time.Duration) {
	if err := appendRecordingFrame(
		r.rec, r.palette, r.useLUT, r.opts.optimize,
		r.opts.background, r.opts.backgroundSet, frame, delay, r.opts.maxFrames,
	); err != nil {
		r.closeErr = err
	}
}

// palettizeCaptureFrame writes src into pm, compositing onto background when
// configured and using the LUT or draw.Draw path per useLUT.
func palettizeCaptureFrame(pm *image.Paletted, frame image.Image, useLUT bool, background color.Color, backgroundSet bool) {
	src := frame
	if backgroundSet {
		src = compositeBackgroundNRGBA(asNRGBA(frame), background)
	}
	if useLUT {
		palettizeInto(pm, src)
		return
	}
	draw.Draw(pm, frame.Bounds(), src, src.Bounds().Min, draw.Src)
}

// asNRGBA returns img as *image.NRGBA. Still's renderer returns NRGBA; foreign
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

// nextDelay returns the duration delay for the current capture tick or channel
// frame. lastCapture advances on every call so deduped identical frames accumulate
// only the elapsed time since the previous capture attempt.
func (r *gifRecorder) nextDelay(explicit time.Duration) time.Duration {
	if explicit > 0 {
		r.lastCapture = time.Now()
		return explicit
	}
	if r.opts.frameRateStrict {
		r.lastCapture = time.Now()
		return time.Second / time.Duration(r.opts.frameRate)
	}
	now := time.Now()
	delay := now.Sub(r.lastCapture)
	r.lastCapture = now
	return delay
}

// quantizeDelays converts accumulated duration delays to GIF centiseconds. FPS
// modes and [WithDelayGrid] snap to the nearest nominal grid multiple; plain
// channel mode rounds once. If we stored delays as centiseconds, it doesn't
// provide enough precision for some of the other rounding, deduplication, etc
// that we do, and it would result in slightly inconsistent FPS for higher-FPS
// GIFs.
func quantizeDelays(delays []time.Duration, opts options) []int {
	cs := make([]int, len(delays))
	fps := opts.frameRate
	if opts.delayGridSet {
		fps = opts.delayGrid
	}
	if opts.frameRateSet || opts.delayGridSet {
		nominal := time.Second / time.Duration(fps)
		nominalCS := max(1, 100/fps)
		for i, d := range delays {
			cs[i] = clampCentiseconds(max(int(math.Round(float64(d)/float64(nominal))), 1) * nominalCS)
		}
		return cs
	}
	for i, d := range delays {
		cs[i] = clampCentiseconds(max(int(math.Round(float64(d)/float64(10*time.Millisecond))), 1))
	}
	return cs
}

// buildGIF converts a finalized recording into a gif.GIF with quantized delays.
func buildGIF(rec *gifRecording, opts options) *gif.GIF {
	images, delays, disposals := splitLongDelays(rec.images, quantizeDelays(rec.delays, opts), rec.disposal)
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

// clampCentiseconds enforces the GIF minimum delay of 1 cs. Values above
// [maxGIFCentiseconds] are split into continuation frames by [splitLongDelays].
func clampCentiseconds(cs int) int {
	if cs < 1 {
		return 1
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

// holdFrame is a 1×1 transparent patch. With DisposalNone it adds delay
// without changing the canvas. Palettes without a transparent slot repeat
// the full frame instead.
func holdFrame(src *image.Paletted) *image.Paletted {
	if src == nil || len(src.Palette) <= int(TransparentPaletteIndex) {
		return src
	}
	p := image.NewPaletted(image.Rect(0, 0, 1, 1), src.Palette)
	p.Pix[0] = TransparentPaletteIndex
	return p
}

var (
	errNilFrame   = gifError("export: nil frame")
	errEmptyFrame = gifError("export: empty frame bounds")
)

// gifError is a sentinel error type for invalid GIF capture frames.
type gifError string

func (e gifError) Error() string { return string(e) }

// compositeBackgroundNRGBA returns an NRGBA copy of src with fully transparent
// pixels replaced by bg. src must be NRGBA; use [asNRGBA] at API boundaries.
func compositeBackgroundNRGBA(src *image.NRGBA, bg color.Color) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	br, bgC, bb, _ := bg.RGBA()
	br8, bgG8, bb8 := uint8(br>>8), uint8(bgC>>8), uint8(bb>>8) //nolint:gosec // RGBA contract.
	width := b.Dx()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		si := (y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (b.Min.X-dst.Rect.Min.X)*4
		srcRow := src.Pix[si : si+width*4]
		dstRow := dst.Pix[di : di+width*4]
		for x := range width {
			sx := x * 4
			if srcRow[sx+3] == 0 {
				dstRow[sx] = br8
				dstRow[sx+1] = bgG8
				dstRow[sx+2] = bb8
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
