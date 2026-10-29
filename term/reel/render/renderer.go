// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"errors"
	"image"
	"sync"
	"time"

	"github.com/lrstanley/x/term/reel/render/internal/config"
	"github.com/lrstanley/x/term/reel/render/internal/fontset"
	imetrics "github.com/lrstanley/x/term/reel/render/internal/metrics"
	"github.com/lrstanley/x/term/reel/render/internal/raster"
	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/vt"
)

// Renderer rasterizes and renders a [vt.Screen] using bundled fonts, derived cell
// metrics, palette/state-driven appearance, and terminal chrome.
type Renderer struct {
	mu sync.Mutex

	closed bool

	opts                 config.Options
	palette              types.Palette
	boxThicknessOverride bool

	fonts   *fontset.Set
	metrics Metrics

	// frame is the image Draw paints and copies out. It is kept between
	// draws, so a draw that changes little repaints little.
	frame *image.NRGBA
	// frameReady is set after Draw paints frame. A rebuild or Close clears it.
	frameReady bool
	frameGen   uint64
	frameSnap  frameSnap
	blinkGen   uint64
	blinkOn    bool

	// chromeMask is the window button shape, rebuilt when the diameter changes.
	chromeMask *image.Alpha
	// chromeCtx lays title bar text out with its own faces. It is zero
	// without window chrome.
	chromeCtx chromeText
	// status holds what program status drawing keeps between frames.
	status rendererStatus
	// progressRow is the progress bar coverage of one row of its strip.
	progressRow []uint8

	fgPass raster.Pass
	// prev holds the cells d.frame shows, row-major, for drawIncremental.
	prev []vt.Cell
	// frameImages is set when d.frame shows Kitty graphics, which cover
	// rows other than their own, so the next draw repaints it whole.
	frameImages bool
	// images keeps scaled Kitty graphics between frames.
	images imageCache
}

// rendererStatus is what program status drawing keeps between frames: the
// record dot, rebuilt when its size changes, and the last outline look along
// the window edge, with what it was built for.
type rendererStatus struct {
	dot *image.Alpha
	lut []statusPaint
	key statusLUTKey
}

// New returns a renderer configured with opts.
func New(opts ...Option) (*Renderer, error) {
	d := &Renderer{
		opts: config.DefaultOptions(),
	}
	var errs []error
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(d); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if err := d.buildRenderer(); err != nil {
		return nil, err
	}
	return d, nil
}

// MustNew is like [New] but panics on error.
func MustNew(opts ...Option) *Renderer {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// Metrics returns the current derived renderer metrics.
func (d *Renderer) Metrics() Metrics {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.metrics
}

// Draw draws scr with st, as of offset at into the timeline, into dst at
// dst.Rect.Min. It fills the [Layout.Image] of scr's size there, and leaves
// the rest of dst alone. It panics when dst is smaller than that; allocate
// one with [Renderer.Layout] to fit. The time-driven parts of the image, such
// as a blinking cursor or an indeterminate progress bar, take their phase
// from at.
//
// The renderer keeps the image it drew last, and repaints only what changed
// since, from the screen's dirty rows, the state, and at, before copying it
// into dst. dst itself is always overwritten, never read, so the caller may
// draw over it between calls.
func (d *Renderer) Draw(dst *image.NRGBA, scr vt.Screen, st vt.State, at time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	if dst == nil {
		panic("render: nil destination image")
	}
	if scr == nil {
		panic("render: nil screen")
	}
	if size := d.layout(scr.Bounds().Dx(), scr.Bounds().Dy()).Image.Size(); dst.Rect.Dx() < size.X || dst.Rect.Dy() < size.Y {
		panic("render: destination image too small")
	}
	d.paint(scr, st, at)
	copyNRGBA(dst, d.frame)
}

// paint brings d.frame up to date with scr and st as of at: as it is when it
// already shows them, by repainting the rows and indicators that changed when
// it can, and by painting it whole otherwise.
func (d *Renderer) paint(scr vt.Screen, st vt.State, at time.Duration) {
	if d.reuseFrame(scr, st, at) || d.drawIncremental(scr, st, at) {
		return
	}
	d.frameReady = false
	f := d.borrowRenderFrame(image.Point{}, scr, st, at)
	defer releaseRenderFrame(f)
	img := d.ensureFrameLocked(f.imageBounds)
	d.render(img, scr, f)
	d.rememberFrame(scr, st, 0, scr.Bounds().Dy()-1, at)
}

// copyNRGBA copies src, which starts at the origin, into dst at dst.Rect.Min.
// dst must be at least as large as src.
func copyNRGBA(dst, src *image.NRGBA) {
	w := src.Rect.Dx() * 4
	for y := range src.Rect.Dy() {
		so := y * src.Stride
		do := dst.PixOffset(dst.Rect.Min.X, dst.Rect.Min.Y+y)
		copy(dst.Pix[do:do+w], src.Pix[so:so+w])
	}
}

// Close closes the Renderer, releasing any resources.
func (d *Renderer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	err := errors.Join(d.fonts.Close(), d.chromeCtx.fonts.Close())
	d.fonts = nil
	d.chromeCtx = chromeText{}
	d.frame = nil
	d.frameReady = false
	d.closed = true
	return err
}

func (d *Renderer) ensureFrameLocked(bounds image.Rectangle) *image.NRGBA {
	if d.frame == nil || !d.frame.Bounds().Eq(bounds) {
		d.frame = image.NewNRGBA(bounds)
	}
	return d.frame
}

func (d *Renderer) ensureOpen() {
	if d.closed {
		panic("render: renderer is closed")
	}
}

func (d *Renderer) buildRenderer() error {
	d.frameReady = false
	fonts, err := fontset.Build(d.opts)
	if err != nil {
		return err
	}
	old := d.fonts
	d.fonts = fonts
	if old != nil {
		if closeErr := old.Close(); closeErr != nil {
			return closeErr
		}
	}
	if d.fonts == nil {
		panic("render: fonts are not loaded")
	}
	d.metrics = imetrics.Derive(d.opts, d.fonts.GridMetrics())
	if chromeErr := d.buildChromeFonts(); chromeErr != nil {
		// New returns no renderer on error, so nothing else would close
		// the grid fonts.
		err = errors.Join(chromeErr, d.fonts.Close())
		d.fonts = nil
		return err
	}
	d.palette = config.ClonePalette(d.opts.Palette)
	d.boxThicknessOverride = d.opts.BoxThicknessOverride()
	return nil
}
