// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
)

// Renderer rasterizes and renders a [uv.Screen] using bundled fonts, derived cell
// metrics, palette/state-driven appearance, and pluggable render hooks.
//
// ## Locking
//
// [Renderer.mu] serializes exported mutators and observers together with draw paths
// so fonts, metrics, option snapshots (including [EmulatorState] cached in
// [rendererOptions]), and [Renderer.Close] remain race-free. [Option] callbacks
// invoked from [Renderer.Apply] run under [Renderer.mu]; custom [Option] implementations
// must not call back into the same [Renderer] or deadlock may occur.
//
// ## Lifecycle and closed state
//
// [Renderer.Close] closes loaded faces and sets [Renderer.closed]; [Renderer.fonts] and
// [Renderer.frame] are cleared afterward. [Renderer.ensureOpen] panics if [Renderer.Draw]
// or [Renderer.Apply] is invoked after [Renderer.Close]. [Metrics] may retain the last
// derived snapshot until the renderer is discarded.
//
// ## Dirty tracking and rebuilds
//
// [Renderer.dirty] records whether fonts and/or metrics must refresh during
// [Renderer.rebuildLocked] after options change. Only font-affecting options set
// [dirtyFonts]; metric adjustment options set [dirtyMetrics]. Palette, geometry,
// emulator state, and render hooks apply on the next draw without rebuilds.
// [Renderer.rebuildLocked] rebuilds font faces when [dirtyFonts] is set, recomputes
// [Metrics] when fonts or metrics inputs change, then clears [Renderer.dirty].
// [Renderer.fontVersion] and [Renderer.metricsVersion] increment when faces or
// metrics are rebuilt for tests and diagnostics.
//
// ## Full-frame composition order (Ghostty-aligned concepts)
//
// Default rendering walks layers in this order (see [Renderer.renderLocked]): window
// background, cell backgrounds, cell foregrounds (glyphs and decorations, including
// SGR blink phases via drawable hooks), cursor, scrollbar gutter widget, unfocused
// dimming overlay,
// then antialiased rounding mask on the terminal window. Ghostty documents the
// closest configuration knobs on its reference page:
//
//   - Font sizing and faces: [cfg-font-size], [cfg-font-family], [cfg-font-codepoint-map]
//   - Cell metric tweaks: [cfg-adjust-cell-width], [cfg-adjust-cell-height]
//   - Window padding (insets around the cell grid): [cfg-window-padding-x],
//     [cfg-window-padding-y], [cfg-window-padding-color]
//   - Terminal background opacity and cell tinting: [cfg-background-opacity],
//     [cfg-background-opacity-cells]
//   - Faint rendering intensity: [cfg-faint-opacity]
//   - Cursor drawing and blink timing: [cfg-cursor-style], [cfg-cursor-color],
//     [cfg-cursor-text], [cfg-cursor-style-blink]
//   - Scrollbar visibility rules: [cfg-scrollbar]
//   - Focus/unfocused presentation: [cfg-unfocused-split-opacity]
//
// Rounded terminal masking is a raster convenience for renders; Ghostty
// inherits native window corners instead of exposing a dedicated terminal-surface
// radius knob (see window chrome notes near [cfg-window-decoration]).
//
// [cfg-font-size]: https://ghostty.org/docs/config/reference#font-size
// [cfg-font-family]: https://ghostty.org/docs/config/reference#font-family
// [cfg-font-codepoint-map]: https://ghostty.org/docs/config/reference#font-codepoint-map
// [cfg-adjust-cell-width]: https://ghostty.org/docs/config/reference#adjust-cell-width
// [cfg-adjust-cell-height]: https://ghostty.org/docs/config/reference#adjust-cell-height
// [cfg-window-padding-x]: https://ghostty.org/docs/config/reference#window-padding-x
// [cfg-window-padding-y]: https://ghostty.org/docs/config/reference#window-padding-y
// [cfg-window-padding-color]: https://ghostty.org/docs/config/reference#window-padding-color
// [cfg-background-opacity]: https://ghostty.org/docs/config/reference#background-opacity
// [cfg-background-opacity-cells]: https://ghostty.org/docs/config/reference#background-opacity-cells
// [cfg-faint-opacity]: https://ghostty.org/docs/config/reference#faint-opacity
// [cfg-cursor-style]: https://ghostty.org/docs/config/reference#cursor-style
// [cfg-cursor-color]: https://ghostty.org/docs/config/reference#cursor-color
// [cfg-cursor-text]: https://ghostty.org/docs/config/reference#cursor-text
// [cfg-cursor-style-blink]: https://ghostty.org/docs/config/reference#cursor-style-blink
// [cfg-scrollbar]: https://ghostty.org/docs/config/reference#scrollbar
// [cfg-unfocused-split-opacity]: https://ghostty.org/docs/config/reference#unfocused-split-opacity
// [cfg-window-decoration]: https://ghostty.org/docs/config/reference#window-decoration
type Renderer struct {
	// mu protects all fields below and coordinates with other Renderer methods
	// that lock the same mutex (for example [Renderer.DrawInto]).
	mu sync.Mutex

	// closed becomes true after [Renderer.Close]; [Renderer.Draw] or [Renderer.Apply]
	// call [Renderer.ensureOpen] first.
	closed bool

	// opts stores normalized renderer options and optional [EmulatorState]
	// ([rendererOptions.hasState]).
	opts rendererOptions

	// fonts holds resolved faces built from opts when [dirtyFonts] fires.
	fonts *fontSet

	// metrics is the latest [deriveMetrics] product aligned with fonts and opts.
	metrics Metrics

	// dirty accumulates rebuild categories between [Renderer.Apply] batches.
	dirty dirtyCategories

	// fontVersion increments whenever [Renderer.rebuildLocked] replaces font faces.
	fontVersion uint64

	// metricsVersion increments whenever [Renderer.rebuildLocked] recomputes metrics.
	metricsVersion uint64

	// frame is the reusable raster returned by [Renderer.Draw]; reallocated when
	// required bounds change.
	frame *image.NRGBA

	// glyphCov holds per-frame max-alpha glyph coverage for the cell grid.
	glyphCov glyphCoverage

	// fgPass holds deferred foreground draws for the unified NRGBA pipeline.
	fgPass glyphCoveragePass
}

// New returns a renderer configured with opts.
func New(opts ...Option) (*Renderer, error) {
	d := &Renderer{
		opts:  defaultRendererOptions(),
		dirty: dirtyFonts | dirtyMetrics,
	}
	d.applyLocked(opts...)
	if err := d.rebuildLocked(); err != nil {
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

// EmulatorState returns the current emulator state.
func (d *Renderer) EmulatorState() EmulatorState {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.opts.state
}

// HasEmulatorState reports whether an emulator state has been applied.
func (d *Renderer) HasEmulatorState() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.opts.hasState
}

// Metrics returns the current derived renderer metrics.
func (d *Renderer) Metrics() Metrics {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.metrics
}

// Apply applies the given options to the Renderer, under protection of a mutex.
func (d *Renderer) Apply(opts ...Option) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ensureOpen()
	d.applyLocked(opts...)
	return d.rebuildLocked()
}

// Draw rasterizes scr into the renderer's reusable frame buffer and returns it.
//
// The returned [image.Image] is owned by the renderer and invalidated by the next
// [Renderer.Draw] call or [Renderer.Close]. Do not mutate it and do not retain it
// across [Renderer.Draw] calls; encode or copy immediately, or use [Renderer.DrawInto]
// with a caller-owned destination when pixels must outlive the draw.
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

	err := d.fonts.close()
	d.fonts = nil
	d.frame = nil
	d.closed = true
	return err
}

// ensureFrameLocked returns a frame buffer sized for bounds, reusing [Renderer.frame]
// when its bounds already match. The caller must hold [Renderer.mu].
func (d *Renderer) ensureFrameLocked(bounds image.Rectangle) *image.NRGBA {
	if d.frame == nil || !d.frame.Bounds().Eq(bounds) {
		d.frame = image.NewNRGBA(bounds)
	}
	return d.frame
}

// applyLocked runs [Option] callbacks sequentially while [Renderer.mu] is held by
// the caller.
func (d *Renderer) applyLocked(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
}

// ensureOpen panics when the renderer has been [Renderer.Close]'d.
func (d *Renderer) ensureOpen() {
	if d.closed {
		panic("renderer is closed")
	}
}

// markDirty merges rebuild categories before the next [Renderer.rebuildLocked].
func (d *Renderer) markDirty(cats dirtyCategories) {
	d.dirty |= cats
}

// rebuildLocked reconstructs font faces and/or metrics from [Renderer.dirty] flags
// and then clears [Renderer.dirty].
func (d *Renderer) rebuildLocked() error {
	if d.dirty&dirtyFonts != 0 {
		fonts, err := buildFontSet(d.opts)
		if err != nil {
			return err
		}
		old := d.fonts
		d.fonts = fonts
		if closeErr := old.close(); closeErr != nil {
			return closeErr
		}
		d.fontVersion++
	}
	if d.dirty&(dirtyFonts|dirtyMetrics) != 0 {
		d.metrics = deriveMetrics(d.opts, d.fonts)
		d.metricsVersion++
	}
	d.dirty = dirtyNone
	return nil
}
