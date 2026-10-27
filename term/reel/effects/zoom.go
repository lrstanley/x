// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// zoomMargin is the cells [Zoom] keeps around its region on every side when
// it fits the region to the grid.
const zoomMargin = 1

// ZoomOption configures [Zoom]: [Scale], [Anchor], and the [Timing] options
// every region effect takes.
type ZoomOption interface {
	applyZoom(z *zoom)
}

type zoomOption func(*zoom)

func (o zoomOption) applyZoom(z *zoom) { o(z) }

// Scale sets how many times larger [Zoom] draws its region than the
// terminal does. Without it, the zoom fits the region, with a cell of margin
// on every side, to the terminal's grid, as large as it fits, and never
// smaller than the terminal draws it. A factor below 1, or one that is not
// finite, is an error from Start.
func Scale(factor float64) ZoomOption {
	return zoomOption(func(z *zoom) { z.scale, z.scaleSet = factor, true })
}

// Anchor sets where [Zoom] places its region in the grid: the point at
// fraction (x, y) of the region's width and height, from its top left, is
// drawn at the same fraction of the grid's. (0, 0) puts the region's top
// left corner in the grid's, (1, 1) its bottom right corner in the grid's,
// and the default, (0.5, 0.5), centers it. A zoom never shows past the edges
// of the terminal, so a region near one is drawn nearer it than its anchor
// asks. A fraction outside 0 to 1 is an error from Start.
func Anchor(x, y float64) ZoomOption {
	return zoomOption(func(z *zoom) { z.ax, z.ay = x, y })
}

func (o Timing) applyZoom(z *zoom) {
	if o != nil {
		o(&z.timing)
	}
}

// Zoom returns an effect that zooms in on the cells of region: the
// terminal's grid shows the region larger (see [Scale]), placed by
// [Anchor]. The window chrome, padding, and everything else around the grid
// is kept as the frame has it, so the image keeps its size. [Between] limits
// the zoom to a window of the timeline, and [Ease] eases it in and out over
// frames on the delay grid, with the part of the screen shown moving
// smoothly between the whole grid and the zoom.
//
// The zoom is drawn in cell space when the recording's rasterizer is a
// [render.Renderer]: the cells it shows are drawn again by a renderer of
// its own, with the recording's options at the zoom's scale, without window
// chrome, so the text stays sharp. With another [reel.Rasterizer], the
// frame's image is cropped and scaled up with a Catmull-Rom filter. Frames
// on the way in and out are scaled from the frame as drawn, with the sharp
// zoom drawn over the part it covers, so they play on their own GIF palette
// ([reel.Frame.Isolated]). A zoom that holds still keeps the recording's
// palette.
//
// The region is matched once a zoom starts: on the first terminal frame it
// covers cells of, at the start of the window or on the way in. The zoom
// then holds that part of the screen, wherever the region's text moves or
// whether it is still shown, until it has eased out, so it never jumps or
// drifts while it is held. A region that covers no cells until later in the
// window zooms in at once when it does. Frames without a terminal, such as
// cards, are left alone, and a terminal resized during the zoom is matched
// again.
//
// The renderer is built from [reel.Context.Render], [reel.Profile.Render],
// so a [reel.Profile.Rasterizer] that is a renderer of the caller's own
// should be built with those options. In this path the zoomed grid is drawn
// from the cells, over whatever effects before Zoom drew on it, so list Zoom
// before effects that draw on the frame, such as [Highlight], and before
// those whose overlays should keep their size and place, such as [Keys] and
// [PauseBadge]. A nil region is an error from Start.
func Zoom(region Region, opts ...ZoomOption) reel.Effect {
	z := zoom{region: region, ax: 0.5, ay: 0.5}
	for _, opt := range opts {
		if opt != nil {
			opt.applyZoom(&z)
		}
	}
	return z
}

type zoom struct {
	region Region
	timing timing
	// scale is the zoom's scale when scaleSet, and the region is fit to
	// the grid otherwise.
	scale    float64
	scaleSet bool
	ax, ay   float64
}

func (e zoom) Start(ctx *reel.Context) (reel.Stage, error) {
	inUnit := func(f float64) bool { return f >= 0 && f <= 1 }
	switch {
	case e.region == nil:
		return nil, errors.New("effects: zoom without a region")
	case e.scaleSet && !(e.scale >= 1 && !math.IsInf(e.scale, 0)):
		return nil, fmt.Errorf("effects: zoom scale %v is below 1 or not finite", e.scale)
	case !inUnit(e.ax) || !inUnit(e.ay):
		return nil, fmt.Errorf("effects: zoom anchor (%v, %v) is outside 0 to 1", e.ax, e.ay)
	}
	w, err := e.timing.start(ctx, easeAfterMark)
	if err != nil {
		return nil, err
	}
	s := &zoomStage{zoom: e, window: w, rast: ctx.Rasterizer}
	if _, ok := ctx.Rasterizer.(*render.Renderer); ok {
		s.base = ctx.Layout.Scale
		s.opts = append(slices.Clip(ctx.Render),
			render.WithPadding(0),
			render.WithMargin(0, nil),
			render.WithBorderRadius(0),
			render.WithScrollbar(false),
			render.WithWindowChrome(false),
			render.WithProgressBar(false),
		)
		ctx.OnClose(s.close)
		if e.scaleSet {
			if _, err = s.renderer(e.scale); err != nil {
				return nil, err
			}
		}
	}
	return s.run, nil
}

// zoomStage is the zoom for one recording.
type zoomStage struct {
	zoom
	window *window
	rast   reel.Rasterizer

	// opts are the options of the renderer that draws the zoom's cells,
	// but for its scale, and base is the scale of the terminal font. opts
	// is nil when the recording's rasterizer is not a [render.Renderer].
	opts []render.Option
	base float64
	// cells draws the zoom's cells at the scale cellsScale.
	cells      *render.Renderer
	cellsScale float64
	// err is the errors building and closing the cell renderers, returned
	// when the recording ends.
	err error

	// target is the zoom being shown, or nil.
	target *zoomTarget
	// buf holds the cells the cell renderer drew last, into img.
	buf *screen.Buffer
	img *image.NRGBA
	// scratch is the image transitions are scaled into.
	scratch *image.RGBA
}

func (s *zoomStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	pieces := s.window.pieces(f)
	if len(pieces) == 1 && pieces[0].level == 0 {
		s.target = nil
		emit(f)
		return
	}
	if t := s.target; t == nil || t.size != f.Cells.Screen.Bounds().Size() {
		s.target = s.acquire(f.Cells)
	}
	t := s.target
	if t == nil {
		emit(f)
		return
	}
	// The sharp cells and the held image are drawn once for every piece.
	var cells, held *image.NRGBA
	if t.sharp {
		cells = s.drawCells(t, f.Cells)
	}
	for _, p := range pieces {
		out := f
		out.At = f.At + time.Duration(float64(p.start)*s.window.speed)
		out.Delay = p.delay
		switch {
		case p.level == 1:
			if held == nil {
				held = s.drawZoom(f, t, 1, cells)
			}
			out.Image = held
		case p.level > 0:
			out.Image = s.drawZoom(f, t, p.level, cells)
			out.Isolated = true
		}
		emit(out)
	}
	if pieces[len(pieces)-1].level == 0 {
		s.target = nil
	}
}

// renderer returns the renderer that draws cells at scale, building it
// when the one built last has another scale.
func (s *zoomStage) renderer(scale float64) (*render.Renderer, error) {
	if s.cells != nil && s.cellsScale == scale {
		return s.cells, nil
	}
	r, err := render.New(append(slices.Clip(s.opts), render.WithScale(s.base*scale))...)
	if err != nil {
		return nil, fmt.Errorf("effects: zoom renderer: %w", err)
	}
	if s.cells != nil {
		s.err = errors.Join(s.err, s.cells.Close())
	}
	s.cells, s.cellsScale = r, scale
	return r, nil
}

func (s *zoomStage) close() error {
	err := s.err
	if s.cells != nil {
		err = errors.Join(err, s.cells.Close())
		s.cells = nil
	}
	s.err = nil
	return err
}

// zoomTarget is a zoom fixed for a terminal of one size: how the grid's
// pixels are drawn at full strength.
type zoomTarget struct {
	size   image.Point
	layout render.Layout
	x, y   zoomAxis
	// sharp is set when the cells in crop are drawn again, by the cell
	// renderer, as cells of size cell, with their top left at at from the
	// grid's.
	sharp bool
	crop  image.Rectangle
	cell  image.Point
	at    image.Point
}

// zoomAxis is a zoom along one axis of the grid: the pixel at p from the
// grid's edge, as the terminal frame draws it, is drawn at off + k*p.
type zoomAxis struct{ k, off float64 }

// at returns the zoom l of the way from none to a, along an axis of the
// grid size pixels long: the span of the frame's grid it shows moves from
// the whole of it to the span a shows, linearly, so the zoom never shows
// past the grid's edges on the way.
func (a zoomAxis) at(l, size float64) zoomAxis {
	from := l * (-a.off / a.k)
	to := (1-l)*size + l*(size-a.off)/a.k
	k := size / (to - from)
	return zoomAxis{k: k, off: -from * k}
}

// placeAxis returns the zoom along one axis of a grid of n cells, base
// pixels each, that draws them cell pixels each, with the point at fraction
// a of the region of length cells from cell r0 at fraction a of the grid,
// but never past the grid's edges. whole rounds the offset to whole pixels.
func placeAxis(n int, base, cell float64, r0, length int, a float64, whole bool) zoomAxis {
	size := float64(n) * base
	off := a*size - (float64(r0)+a*float64(length))*cell
	if whole {
		off = math.Round(off)
	}
	off = min(max(off, size-float64(n)*cell), 0)
	return zoomAxis{k: cell / base, off: off}
}

// acquire returns the zoom on the cells the region covers on cf, or nil when
// it covers none, or the zoom would leave the grid as it is.
func (s *zoomStage) acquire(cf *reel.CellFrame) *zoomTarget {
	rects := regionCells(s.region, cf)
	if len(rects) == 0 {
		return nil
	}
	r := rects[0]
	for _, c := range rects[1:] {
		r = r.Union(c)
	}
	size := cf.Screen.Bounds().Size()
	t := &zoomTarget{size: size, layout: s.rast.Layout(size.X, size.Y)}
	base := t.layout.Cell
	// fit is how much larger cells of w by h pixels can be drawn with the
	// region and its margin still on the grid.
	fit := func(w, h float64) float64 {
		return min(
			float64(size.X*base.X)/(w*float64(r.Dx()+2*zoomMargin)),
			float64(size.Y*base.Y)/(h*float64(r.Dy()+2*zoomMargin)),
		)
	}
	scale := s.scale
	if !s.scaleSet {
		scale = max(1, fit(float64(base.X), float64(base.Y)))
	}
	if scale == 1 {
		return nil
	}
	cw, ch := float64(base.X)*scale, float64(base.Y)*scale
	if s.opts != nil {
		rend, err := s.renderer(scale)
		if err == nil && !s.scaleSet {
			// The renderer's cells are whole pixels, which may be too
			// large for the fit: scale down by as much.
			if c := rend.Layout(1, 1).Cell; fit(float64(c.X), float64(c.Y)) < 1 {
				scale = max(1, scale*fit(float64(c.X), float64(c.Y)))
				rend, err = s.renderer(scale)
			}
		}
		if err != nil {
			// Scaled from the frame instead.
			s.err = errors.Join(s.err, err)
		} else {
			t.sharp, t.cell = true, rend.Layout(1, 1).Cell
			cw, ch = float64(t.cell.X), float64(t.cell.Y)
		}
	}
	t.x = placeAxis(size.X, float64(base.X), cw, r.Min.X, r.Dx(), s.ax, t.sharp)
	t.y = placeAxis(size.Y, float64(base.Y), ch, r.Min.Y, r.Dy(), s.ay, t.sharp)
	if t.sharp {
		grid := t.layout.Grid.Size()
		x0, x1 := cellSpan(t.x.off, cw, grid.X, size.X)
		y0, y1 := cellSpan(t.y.off, ch, grid.Y, size.Y)
		// Columns more on either side, off the grid, so the characters cut
		// by its edges are drawn as they would be with their neighbors:
		// wide characters whole, and icons with the room they grow into.
		t.crop = image.Rect(max(0, x0-2), y0, min(size.X, x1+1), y1)
		t.at = image.Pt(int(t.x.off)+t.crop.Min.X*t.cell.X, int(t.y.off)+t.crop.Min.Y*t.cell.Y)
	}
	return t
}

// cellSpan returns the cells, of n of size cell, that a zoom offset by off
// shows on a grid size pixels long.
func cellSpan(off, cell float64, size, n int) (c0, c1 int) {
	c0 = int(math.Floor(-off / cell))
	c1 = int(math.Ceil((float64(size) - off) / cell))
	return max(0, c0), min(n, c1)
}

// drawCells draws the cells of t's crop of cf with the cell renderer. The
// crop is copied into a buffer and an image kept between frames, so the
// renderer, which compares the cells with those it drew last, repaints only
// the rows that changed. The image is reused by the next call.
func (s *zoomStage) drawCells(t *zoomTarget, cf *reel.CellFrame) *image.NRGBA {
	c := t.crop
	if s.buf == nil || s.buf.Bounds().Size() != c.Size() {
		s.buf = screen.NewBuffer(c.Dx(), c.Dy())
	} else {
		s.buf.ClearDirty()
	}
	b := cf.Screen.Bounds()
	for y := range c.Dy() {
		for x := range c.Dx() {
			if cell := cf.Screen.CellAt(b.Min.X+c.Min.X+x, b.Min.Y+c.Min.Y+y); cell != nil {
				s.buf.SetRaw(x, y, *cell)
			}
		}
	}
	st := cf.State
	st.Cursor.X -= c.Min.X
	st.Cursor.Y -= c.Min.Y
	if !image.Pt(st.Cursor.X, st.Cursor.Y).In(image.Rect(0, 0, c.Dx(), c.Dy())) {
		st.Cursor.Visible = false
	}
	if r := s.cells.Layout(c.Dx(), c.Dy()).Image; s.img == nil || s.img.Rect != r {
		s.img = image.NewNRGBA(r)
	}
	s.cells.Draw(s.img, s.buf, st, cf.At)
	return s.img
}
