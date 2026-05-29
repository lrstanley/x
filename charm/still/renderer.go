// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"errors"
	"image"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/fontset"
	imetrics "github.com/lrstanley/x/charm/still/internal/metrics"
	"github.com/lrstanley/x/charm/still/internal/raster"
)

type rendererHooks struct {
	cellBgDrawer     CellDrawer
	cellFgDrawer     CellDrawer
	cursorDrawer     CursorDrawer
	scrollbarDrawer  ScrollbarDrawer
	backgroundDrawer BackgroundDrawer
}

func defaultRendererHooks() rendererHooks {
	return rendererHooks{
		cellBgDrawer:     DrawCellBg,
		cellFgDrawer:     DrawCellFg,
		cursorDrawer:     DrawCursor,
		scrollbarDrawer:  DrawScrollbar,
		backgroundDrawer: DrawBackground,
	}
}

// Renderer rasterizes and renders a [uv.Screen] using bundled fonts, derived cell
// metrics, palette/state-driven appearance, and pluggable render hooks.
type Renderer struct {
	mu sync.Mutex

	closed bool

	opts  config.Options
	hooks rendererHooks

	fonts   *fontset.Set
	metrics Metrics

	emulatorState *EmulatorState

	frame *image.NRGBA

	fgPass raster.Pass
}

// New returns a renderer configured with opts.
func New(opts ...Option) (*Renderer, error) {
	d := &Renderer{
		opts:  config.DefaultOptions(),
		hooks: defaultRendererHooks(),
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
// Passing nil clears the state.
func (d *Renderer) UpdateEmulatorState(state *EmulatorState) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	if state == nil {
		d.emulatorState = nil
		return
	}
	st := *state
	d.emulatorState = &st
}

// Metrics returns the current derived renderer metrics.
func (d *Renderer) Metrics() Metrics {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.metrics
}

// Draw rasterizes scr into the renderer's reusable frame buffer and returns it.
func (d *Renderer) Draw(scr uv.Screen) image.Image {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	ctx := d.contextLocked(image.Point{}, scr)
	bounds := ctx.ImageBounds()
	img := d.ensureFrameLocked(bounds)
	d.drawIntoContextLocked(ctx, img, bounds, scr)
	return img
}

// Close closes the Renderer, releasing any resources.
func (d *Renderer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	err := d.fonts.Close()
	d.fonts = nil
	d.frame = nil
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
	return nil
}
