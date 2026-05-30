// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/lrstanley/x/charm/still/units"
)

// GIF records animated frames to path. Returns a closer that finalizes and
// encodes on first call; later calls are no-op. Setup errors are returned
// immediately; finalize and encode errors come from the closer.
func GIF(path string, opts ...Option) (closer func() error, err error) {
	cfg, err := collectOptions(formatGIF, opts...)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	rec := newGIFRecorder(cfg)
	rec.encodeFn = func(out *gif.GIF) error {
		eerr := gif.EncodeAll(f, out)
		if eerr != nil {
			_ = f.Close()
			return eerr
		}
		return f.Close()
	}
	rec.start()
	return rec.closer(), nil
}

// WriteGIF records animated frames to w. See [GIF] for lifecycle and errors.
func WriteGIF(w io.Writer, opts ...Option) (closer func() error, err error) {
	cfg, err := collectOptions(formatGIF, opts...)
	if err != nil {
		return nil, err
	}
	rec := newGIFRecorder(cfg)
	rec.encodeFn = func(out *gif.GIF) error {
		return gif.EncodeAll(w, out)
	}
	rec.start()
	return rec.closer(), nil
}

// MustGIF is a convenience wrapper around [GIF] that panics on setup or close
// errors.
func MustGIF(path string, opts ...Option) func() {
	closer, err := GIF(path, opts...)
	if err != nil {
		panic(err)
	}
	return func() {
		cerr := closer()
		if cerr != nil {
			panic(cerr)
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

// gifRecorder captures palettized frames into a [gifRecording] and encodes on close.
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
	// done is closed by closer to signal capture goroutines to finalize.
	done chan struct{}
	// wg waits for capture goroutines during close.
	wg sync.WaitGroup
	// encodeFn writes the finalized GIF to the destination file or writer.
	encodeFn func(*gif.GIF) error
	// closeErr stores the first capture, validation, or encode error.
	closeErr error
	// once ensures done is closed and wg.Wait runs exactly once on close.
	once sync.Once
	// finalizeOnce ensures finalizeAndEncode runs exactly once.
	finalizeOnce sync.Once
	// capturing is true while a strict-mode frame function call is in progress.
	capturing bool
	// captureMu protects capturing for strict-mode tick skipping.
	captureMu sync.Mutex
}

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
			loopCount: 0,
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

// closer returns the idempotent finalize callback; the first call closes
// capture and waits for encode, later calls return the stored error only.
func (r *gifRecorder) closer() func() error {
	return func() error {
		r.once.Do(func() {
			close(r.done)
			r.wg.Wait()
		})
		return r.closeErr
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
			r.finalizeAndEncode()
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
// is signaled.
func (r *gifRecorder) runChannel() {
	r.lastCapture = time.Now()
	ch := r.opts.channel
	for {
		select {
		case <-r.done:
			r.finalizeAndEncode()
			return
		case f, ok := <-ch:
			if !ok {
				r.finalizeAndEncode()
				return
			}
			r.captureChannelFrame(f)
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

	rec.images = append(rec.images, &image.Paletted{
		Pix:     pm.Pix,
		Stride:  pm.Stride,
		Rect:    pm.Rect,
		Palette: pm.Palette,
	})
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
// modes snap to the nearest nominal grid multiple; channel mode rounds once. If
// we stored delays as centiseconds, it doesn't provide enough precision for some
// of the other rounding, deduplication, etc that we do, and it would result in
// slightly inconsistent FPS for higher-FPS GIFs.
func quantizeDelays(delays []time.Duration, opts options) []int {
	cs := make([]int, len(delays))
	if opts.frameRateSet {
		fps := opts.frameRate
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
	return &gif.GIF{
		LoopCount:       rec.loopCount,
		Config:          rec.config,
		Image:           rec.images,
		Delay:           quantizeDelays(rec.delays, opts),
		Disposal:        rec.disposal,
		BackgroundIndex: rec.backgroundIndex,
	}
}

// clampCentiseconds enforces GIF delay bounds: minimum 1 cs, maximum 65535cs
// (~655.35s).
func clampCentiseconds(cs int) int {
	return units.Clamp(cs, 1, 65535)
}

// finalizeAndEncode runs disposal, palette, and transparency passes then
// invokes encodeFn once; errors are stored in closeErr.
func (r *gifRecorder) finalizeAndEncode() {
	r.finalizeOnce.Do(func() {
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
		if r.encodeFn != nil {
			if err := r.encodeFn(out); err != nil && r.closeErr == nil {
				r.closeErr = err
			}
		}
	})
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
