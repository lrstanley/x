// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"errors"
	"image"
	"math"
	"slices"
	"testing"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
)

// zoomFrames exports tl through effects at fps, drawn by rast, or the
// highlight tests' fake rasterizer when it is nil, and returns the frames
// the effects were given, and those they emitted.
func zoomFrames(t *testing.T, tl *reel.Timeline, fps int, rast reel.Rasterizer, opts []render.Option, effects ...reel.Effect) (in, out []reel.Frame) {
	t.Helper()
	if rast == nil {
		rast = highlightRast
	}
	keep := effectFunc(func(f reel.Frame, emit func(reel.Frame)) {
		in = append(in, f)
		emit(f)
	})
	out = exportFrames(t, tl, reel.Profile{
		FPS:        fps,
		Rasterizer: rast,
		Render:     opts,
		Effects:    append([]reel.Effect{keep}, effects...),
	})
	return in, out
}

// zoomRenderer returns a renderer with opts, which the test closes.
func zoomRenderer(t *testing.T, opts ...render.Option) *render.Renderer {
	t.Helper()
	rend, err := render.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rend.Close() })
	return rend
}

// assertOutsideGrid checks that out matches in outside grid.
func assertOutsideGrid(t *testing.T, in, out *image.NRGBA, grid image.Rectangle) {
	t.Helper()
	if in.Rect != out.Rect {
		t.Fatalf("image %v, want %v", out.Rect, in.Rect)
	}
	for y := in.Rect.Min.Y; y < in.Rect.Max.Y; y++ {
		for x := in.Rect.Min.X; x < in.Rect.Max.X; x++ {
			if !image.Pt(x, y).In(grid) && in.NRGBAAt(x, y) != out.NRGBAAt(x, y) {
				t.Fatalf("pixel (%d, %d) outside the grid changed", x, y)
			}
		}
	}
}

// TestZoomPixels checks a zoom of a fixed rectangle with a rasterizer that
// is not a renderer: the grid is scaled from the frame, and the border
// around it kept.
func TestZoomPixels(t *testing.T) {
	t.Parallel()
	// Cells far apart in red, which the fake rasterizer draws text as.
	tl := buildTimeline(t, 8, 4, "", output(0, "~ A~ A~ \r\n A~~A ~A\r\n~ A~ A~ \r\nA~"))
	in, out := zoomFrames(t, tl, 100, nil, nil, Zoom(Cells(image.Rect(2, 1, 4, 2)), Scale(2)))
	if len(out) != 1 || out[0].Isolated || out[0].Image == in[0].Image {
		t.Fatalf("frames %+v, want one zoomed frame on the shared palette", infoOf(out))
	}
	assertOutsideGrid(t, in[0].Image, out[0].Image, highlightRast.Layout(8, 4).Grid)
	assertGolden(t, "zoom-pixels", out)
}

// zoomRenderOpts are the options of the renderer tests.
var zoomRenderOpts = []render.Option{render.WithWindowChrome(true), render.WithPadding(4)}

// TestZoomRenderer checks a zoom drawn by a renderer of its own, inside the
// window chrome and padding of the frame.
func TestZoomRenderer(t *testing.T) {
	t.Parallel()
	rend := zoomRenderer(t, zoomRenderOpts...)
	tl := buildTimeline(t, 12, 3, "zoom", output(0, "$ echo hi\r\nhi\r\n$ "))
	in, out := zoomFrames(t, tl, 100, rend, zoomRenderOpts, Zoom(Text("echo"), Scale(2)))
	if len(out) != 1 || out[0].Isolated {
		t.Fatalf("frames %+v, want one zoomed frame on the shared palette", infoOf(out))
	}
	assertOutsideGrid(t, in[0].Image, out[0].Image, rend.Layout(12, 3).Grid)
	assertGolden(t, "zoom-renderer", out)
}

// TestZoomSharp checks that the zoomed grid of the renderer path is the
// cells drawn directly at the zoom's scale.
func TestZoomSharp(t *testing.T) {
	t.Parallel()
	rend := zoomRenderer(t, zoomRenderOpts...)
	const out = "\x1b[1mbold\x1b[m and \x1b[4;31mred\x1b[m\r\n日本 wide ─── box\r\n\x1b[7mrev\x1b[m\r\n$ "
	for _, tc := range []struct {
		name   string
		out    string
		region Region
		scale  float64
		// cell is the cell drawn at the grid's top left.
		cell image.Point
	}{
		{"top left", out, Cells(image.Rect(0, 0, 4, 1)), 2, image.Pt(0, 0)},
		// The left column is half of a wide character.
		{"inside", out, Cells(image.Rect(1, 1, 5, 2)), 2, image.Pt(1, 1)},
		{"fractional", out, Cells(image.Rect(3, 1, 5, 2)), 2.5, image.Pt(3, 1)},
		// The right edge cuts a wide character.
		{"wide at the right", "abcdefg日本xyz", Cells(image.Rect(0, 0, 4, 1)), 2, image.Pt(0, 0)},
	} {
		tl := buildTimeline(t, 16, 4, "", output(0, tc.out))
		in, zoomed := zoomFrames(t, tl, 100, rend, zoomRenderOpts, Zoom(tc.region, Scale(tc.scale), Anchor(0, 0)))
		direct := zoomRenderer(t, append(slices.Clip(zoomRenderOpts),
			render.WithPadding(0), render.WithWindowChrome(false), render.WithScale(tc.scale))...)
		cells := in[0].Cells
		want := image.NewNRGBA(direct.Layout(16, 4).Image)
		direct.Draw(want, cells.Screen, cells.State, cells.At)
		grid := rend.Layout(16, 4).Grid
		cell := direct.Layout(1, 1).Cell
		at := image.Pt(tc.cell.X*cell.X, tc.cell.Y*cell.Y)
		got := zoomed[0].Image
		for y := range grid.Dy() {
			for x := range grid.Dx() {
				if g, w := got.NRGBAAt(grid.Min.X+x, grid.Min.Y+y), want.NRGBAAt(at.X+x, at.Y+y); g != w {
					t.Fatalf("%s: grid pixel (%d, %d) is %v, drawn directly %v", tc.name, x, y, g, w)
				}
			}
		}
	}
}

// TestZoomEase checks the frames easing a zoom in and out on a 20 fps grid,
// and that only those play on their own palette.
func TestZoomEase(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 6, 2, "",
		output(0, "ab"),
		mark(500, "on"),
		output(1000, "cd"),
		mark(1500, "off"),
		output(2500, "ef"),
	)
	in, out := zoomFrames(t, tl, 20, nil, nil,
		Zoom(Cells(image.Rect(0, 0, 2, 1)), Scale(2), Between("on", "off"), Ease(200*ms)))
	// 200ms of easing is four 50ms steps on either side of the window.
	want := []frameInfo{
		{0, 300 * ms, false, false},
		{300 * ms, 50 * ms, true, false},
		{350 * ms, 50 * ms, true, false},
		{400 * ms, 50 * ms, true, false},
		{450 * ms, 50 * ms, true, false},
		{500 * ms, 500 * ms, false, false},
		{1000 * ms, 500 * ms, false, false},
		{1500 * ms, 50 * ms, true, false},
		{1550 * ms, 50 * ms, true, false},
		{1600 * ms, 50 * ms, true, false},
		{1650 * ms, 50 * ms, true, false},
		{1700 * ms, 800 * ms, false, false},
		{2500 * ms, 50 * ms, false, false},
	}
	if got := infoOf(out); !slices.Equal(got, want) {
		t.Fatalf("frames\n got %+v\nwant %+v", got, want)
	}
	if out[0].Image != in[0].Image || out[11].Image != in[1].Image || out[12].Image != in[2].Image {
		t.Fatal("a frame outside the zoom and its easing was changed")
	}
	// Each step of the way in is further in: the zoomed cell (0, 0) grows
	// over more of the grid.
	grid := highlightRast.Layout(6, 2).Grid
	var prev int
	for i, f := range out[1:6] {
		n := 0
		for x := grid.Min.X; x < grid.Max.X; x++ {
			if f.Image.NRGBAAt(x, grid.Min.Y).R == 'a' {
				n++
			}
		}
		if n < prev || n == prev && i < 4 {
			t.Fatalf("step %d of the way in shows cell (0, 0) over %d pixels, after %d", i, n, prev)
		}
		prev = n
	}
	assertGolden(t, "zoom-ease", out[:6])
}

// TestZoomHolds checks that a zoom holds the part of the screen its region
// covered when it started, while the text moves away and disappears, and
// that it starts once the region covers cells.
func TestZoomHolds(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 10, 3, "",
		output(0, "x"),
		output(100, "\x1b[2J\x1b[Hkey"),
		output(200, "\x1b[2J\x1b[3;5Hkey"),
		output(300, "\x1b[2J"),
	)
	in, out := zoomFrames(t, tl, 100, nil, nil, Zoom(Text("key"), Scale(2), Anchor(0, 0)))
	if len(out) != 4 {
		t.Fatalf("%d frames, want 4", len(out))
	}
	if out[0].Image != in[0].Image {
		t.Fatal("a frame before the region covers cells was zoomed")
	}
	grid := highlightRast.Layout(10, 3).Grid
	topLeft := func(f reel.Frame) uint8 { return f.Image.NRGBAAt(grid.Min.X, grid.Min.Y).R }
	if got := topLeft(out[1]); got != 'k' {
		t.Fatalf("zoom starts on %q, want k", got)
	}
	for i := 2; i < 4; i++ {
		// A blank cell is a space.
		if out[i].Image == in[i].Image || topLeft(out[i]) != ' ' {
			t.Fatalf("frame %d: the zoom did not hold the top left of the screen", i)
		}
	}
}

func TestZoomErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "", output(0, "a"), mark(10, "m"))
	ctx := &reel.Context{Timeline: tl, Rasterizer: highlightRast, Layout: highlightRast.Layout(4, 1), Step: 10 * ms, Speed: 1}
	for _, tc := range []struct {
		name   string
		effect reel.Effect
		is     error
	}{
		{"nil region", Zoom(nil), nil},
		{"scale below 1", Zoom(Row(0), Scale(0.5)), nil},
		{"scale zero", Zoom(Row(0), Scale(0)), nil},
		{"scale NaN", Zoom(Row(0), Scale(math.NaN())), nil},
		{"scale infinite", Zoom(Row(0), Scale(math.Inf(1))), nil},
		{"anchor below 0", Zoom(Row(0), Anchor(-0.1, 0)), nil},
		{"anchor above 1", Zoom(Row(0), Anchor(0, 1.5)), nil},
		{"anchor NaN", Zoom(Row(0), Anchor(math.NaN(), 0)), nil},
		{"ease", Zoom(Row(0), Ease(-ms)), nil},
	} {
		st, err := tc.effect.Start(ctx)
		if err == nil || st != nil || (tc.is != nil && !errors.Is(err, tc.is)) {
			t.Errorf("%s: Start = %v, want an error", tc.name, err)
		}
	}
}

func TestZoomAxis(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// n cells of base pixels, drawn cell pixels each, with the region
		// from r0, length long, anchored at a.
		n            int
		base, cell   float64
		r0, length   int
		a            float64
		whole        bool
		wantK, wantO float64
	}{
		{"centered", 10, 2, 4, 4, 2, 0.5, false, 2, -10},
		{"top left", 10, 2, 4, 4, 2, 0, false, 2, -16},
		{"bottom right", 10, 2, 4, 4, 2, 1, false, 2, -4},
		// Near an edge, it moves in to keep the screen over the grid.
		{"near the start", 10, 2, 4, 0, 1, 0.5, false, 2, 0},
		{"near the end", 10, 2, 4, 9, 1, 0.5, false, 2, -20},
		{"whole pixels", 10, 2, 5, 3, 1, 0.5, true, 2.5, -8},
	} {
		got := placeAxis(tc.n, tc.base, tc.cell, tc.r0, tc.length, tc.a, tc.whole)
		if got.k != tc.wantK || got.off != tc.wantO {
			t.Errorf("%s: zoom %+v, want k %v off %v", tc.name, got, tc.wantK, tc.wantO)
		}
		// None of the way is no zoom, and all of it is the zoom.
		size := float64(tc.n) * tc.base
		if none, all := got.at(0, size), got.at(1, size); none.k != 1 || none.off != 0 || math.Abs(all.k-got.k) > 1e-9 || math.Abs(all.off-got.off) > 1e-9 {
			t.Errorf("%s: at 0 %+v, at 1 %+v, want none and %+v", tc.name, none, all, got)
		}
	}
}

// TestZoomCursorCrop checks that a cursor outside the cells a zoom draws is
// not drawn.
func TestZoomCursorCrop(t *testing.T) {
	t.Parallel()
	rend := zoomRenderer(t)
	tl := buildTimeline(t, 20, 4, "", output(0, "abc\r\n\r\n\r\n                xyz"))
	in, out := zoomFrames(t, tl, 100, rend, nil, Zoom(Cells(image.Rect(0, 0, 2, 1)), Scale(3), Anchor(0, 0)))
	direct := zoomRenderer(t, render.WithScale(3))
	cells := in[0].Cells
	st := cells.State
	if !st.Cursor.Visible || st.Cursor.X < 10 {
		t.Fatalf("cursor %+v, want it shown past the zoom", st.Cursor)
	}
	st.Cursor.Visible = false
	want := image.NewNRGBA(direct.Layout(20, 4).Image)
	direct.Draw(want, cells.Screen, st, cells.At)
	grid := rend.Layout(20, 4).Grid
	got := out[0].Image
	for y := range grid.Dy() {
		g := got.Pix[got.PixOffset(grid.Min.X, grid.Min.Y+y):][:grid.Dx()*4]
		w := want.Pix[want.PixOffset(0, y):][:grid.Dx()*4]
		if !bytes.Equal(g, w) {
			t.Fatalf("row %d of the zoom differs from the cells drawn without the cursor", y)
		}
	}
}

// TestZoomSharpEase checks that the frames easing a sharp zoom in start at
// the frame as drawn and end at the zoom, with the sharp cells scaled over
// the grid, and that a zoom that would not enlarge the region leaves the
// frames alone.
func TestZoomSharpEase(t *testing.T) {
	t.Parallel()
	rend := zoomRenderer(t)
	tl := buildTimeline(t, 16, 4, "", output(0, "zoom in on this\r\nand not on this"), mark(500, "on"), output(1000, "!"))
	_, out := zoomFrames(t, tl, 20, rend, nil, Zoom(Text("this"), Scale(3), Between("on", ""), Ease(400*ms)))
	grid := rend.Layout(16, 4).Grid
	// diff is the mean difference of the grids of a and b, per channel.
	diff := func(a, b *image.NRGBA) float64 {
		var sum float64
		for y := grid.Min.Y; y < grid.Max.Y; y++ {
			for x := grid.Min.X; x < grid.Max.X; x++ {
				p, q := a.NRGBAAt(x, y), b.NRGBAAt(x, y)
				sum += math.Abs(float64(p.R)-float64(q.R)) + math.Abs(float64(p.G)-float64(q.G)) + math.Abs(float64(p.B)-float64(q.B))
			}
		}
		return sum / float64(3*grid.Dx()*grid.Dy())
	}
	// The frame for 100ms, 8 steps easing in, and the zoom.
	if len(out) != 11 || out[0].Isolated || !out[1].Isolated || !out[8].Isolated || out[9].Isolated {
		t.Fatalf("frames %+v, want 8 easing in between the frame and the zoom", infoOf(out))
	}
	// It starts near the frame and ends near the zoom.
	first, apart := diff(out[1].Image, out[0].Image), diff(out[0].Image, out[9].Image)
	if last := diff(out[8].Image, out[9].Image); first > apart/4 || last > apart/2 {
		t.Fatalf("easing starts %.2f from the frame and ends %.2f from the zoom, which are %.2f apart", first, last, apart)
	}

	_, same := zoomFrames(t, tl, 20, rend, nil, Zoom(Rows(0, 4)))
	_, in := zoomFrames(t, tl, 20, rend, nil)
	if len(same) != len(in) || !bytes.Equal(same[0].Image.Pix, in[0].Image.Pix) {
		t.Fatal("a zoom fit to the whole screen changed the frames")
	}
}
