// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"errors"
	"image"
	"sync"

	"github.com/lrstanley/x/term/still/internal/config"
	"github.com/lrstanley/x/term/still/internal/fontset"
	imetrics "github.com/lrstanley/x/term/still/internal/metrics"
	"github.com/lrstanley/x/term/still/internal/raster"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/vt/screen"
)

// Renderer rasterizes and renders a [screen.Screen] using bundled fonts, derived cell
// metrics, palette/state-driven appearance, and terminal chrome.
type Renderer struct {
	mu sync.Mutex

	closed bool

	opts                 config.Options
	palette              types.Palette
	boxThicknessOverride bool

	fonts   *fontset.Set
	metrics Metrics

	emulatorState *EmulatorState

	frame *image.NRGBA
	// frameReady is set after Draw paints frame. A rebuild or Close clears it.
	frameReady bool
	frameGen   uint64
	frameSnap  frameSnap
	blinkGen   uint64
	blinkOn    bool

	// chromeMask is the window button shape, rebuilt when the diameter changes.
	chromeMask *image.Alpha

	fgPass raster.Pass
	// prev holds the cells d.frame shows, row-major, for drawIncremental.
	prev []screen.Cell
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

// GetEmulatorState returns a copy of the current emulator state, or nil if unset.
func (d *Renderer) GetEmulatorState() *EmulatorState {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.emulatorState == nil {
		return nil
	}
	state := *d.emulatorState
	return &state
}

// UpdateEmulatorState replaces live emulator state used during [Renderer.Draw].
// Passing nil clears the state. The renderer keeps one state value and
// updates it in place. The caller's struct is copied, not retained.
func (d *Renderer) UpdateEmulatorState(state *EmulatorState) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	if state == nil {
		d.emulatorState = nil
		return
	}
	if d.emulatorState == nil {
		st := *state
		d.emulatorState = &st
		return
	}
	*d.emulatorState = *state
}

// Metrics returns the current derived renderer metrics.
func (d *Renderer) Metrics() Metrics {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.metrics
}

// Draw rasterizes scr into the renderer's reusable frame buffer and returns it.
// When the cached frame is already that picture, Draw returns it without painting,
// and when only some rows changed it repaints just those rows.
func (d *Renderer) Draw(scr screen.Screen) image.Image {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	if d.reuseFrame(scr) {
		return d.frame
	}
	if d.drawIncremental(scr) {
		return d.frame
	}
	d.frameReady = false
	f := d.borrowRenderFrame(image.Point{}, scr)
	defer releaseRenderFrame(f)
	bounds := f.imageBounds
	img := d.ensureFrameLocked(bounds)
	d.render(img, scr, f)
	d.rememberFrame(scr, 0, scr.Bounds().Dy()-1)
	return img
}

// Close closes the Renderer, releasing any resources.
func (d *Renderer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	err := d.fonts.Close()
	d.fonts = nil
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
		panic("renderer is closed")
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
		panic("fonts are not loaded")
	}
	d.metrics = imetrics.Derive(d.opts, d.fonts.GridMetrics())
	d.palette = config.ClonePalette(d.opts.Palette)
	d.boxThicknessOverride = d.opts.BoxThicknessOverride()
	return nil
}
