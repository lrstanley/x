// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/vt"
)

// badgeTimeline is a 40x10 terminal that changes twice and holds still to
// 400ms.
func badgeTimeline(t *testing.T) *reel.Timeline {
	t.Helper()
	return buildTimeline(t, 40, 10, "", output(0, "a"), output(100, "b"), output(200, "c"), mark(400, "end"))
}

// badgeRect is where a badge of 8 pixels goes on a frame of badgeTimeline
// drawn by the fake rasterizer, top right, 2 pixels in from the edges.
var badgeRect = image.Rect(30, 2, 38, 10)

// redraw draws the terminal of f again with the fake rasterizer.
func redraw(f reel.Frame) *image.NRGBA {
	img := image.NewNRGBA(f.Image.Rect)
	testutil.Rasterizer{}.Draw(img, f.Cells.Screen, f.Cells.State, f.At)
	return img
}

// TestPauseBadge checks that the badge is drawn on a copy of the last
// frame, in its corner, and that the frames before it are left alone.
func TestPauseBadge(t *testing.T) {
	t.Parallel()
	var in []reel.Frame
	out := exportFrames(t, badgeTimeline(t), reel.Profile{
		Effects: []reel.Effect{recordFrames{&in}, PauseBadge(WithBadgeSize(8))},
	})
	if len(in) < 2 || len(out) != len(in) {
		t.Fatalf("%d frames in, %d out, want the same frames", len(in), len(out))
	}
	for i, f := range out[:len(out)-1] {
		if f.Image != in[i].Image || f.Delay != in[i].Delay {
			t.Errorf("frame %d changed", i)
		}
	}
	last, src := out[len(out)-1], in[len(in)-1]
	if last.Image == src.Image || last.Delay != src.Delay {
		t.Fatal("the last frame was not redrawn on a copy")
	}
	plain := redraw(src)
	if !bytes.Equal(src.Image.Pix, plain.Pix) {
		t.Error("the badge was drawn on the last frame's own image")
	}
	var changed bool
	b := last.Image.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			same := last.Image.NRGBAAt(x, y) == plain.NRGBAAt(x, y)
			if inBadge := image.Pt(x, y).In(badgeRect); !inBadge && !same {
				t.Errorf("pixel (%d, %d) outside the badge changed", x, y)
			} else if inBadge && !same {
				changed = true
			}
		}
	}
	if !changed {
		t.Errorf("no pixel in %v changed", badgeRect)
	}
}

// outroEffect inserts a card of a single color after the last frame.
type outroEffect struct{}

var outroColor = color.NRGBA{R: 1, G: 2, B: 3, A: 0xff}

func (outroEffect) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		emit(f)
		if f.Last {
			card := image.NewNRGBA(f.Image.Rect)
			draw.Draw(card, card.Rect, image.NewUniform(outroColor), image.Point{}, draw.Src)
			emit(reel.Frame{Image: card, Delay: ms * 500, Isolated: true})
		}
	}, nil
}

// TestPauseBadgeCard checks that a card that ends the recording never gets
// the badge: listed after the badge, the card follows the badged terminal
// frame, and listed before it, no frame is badged.
func TestPauseBadgeCard(t *testing.T) {
	t.Parallel()
	badge := PauseBadge(WithBadgeSize(8))

	var in []reel.Frame
	out := exportFrames(t, badgeTimeline(t), reel.Profile{
		Effects: []reel.Effect{outroEffect{}, recordFrames{&in}, badge},
	})
	if len(out) != len(in) || in[len(in)-1].Cells != nil {
		t.Fatalf("%d frames in, %d out, want the same frames ending on the card", len(in), len(out))
	}
	for i, f := range out {
		if badged := f.Image != in[i].Image; badged != in[i].Hold {
			t.Errorf("badge before the card: frame %d badged = %v, want %v", i, badged, in[i].Hold)
		}
	}

	in = nil
	out = exportFrames(t, badgeTimeline(t), reel.Profile{
		Effects: []reel.Effect{recordFrames{&in}, badge, outroEffect{}},
	})
	if len(out) != len(in)+1 {
		t.Fatalf("%d frames in, %d out, want a card after them", len(in), len(out))
	}
	if held := out[len(in)-1]; held.Image == in[len(in)-1].Image {
		t.Error("badge after the card: the held terminal frame has no badge")
	}
	want := image.NewNRGBA(in[0].Image.Rect)
	draw.Draw(want, want.Rect, image.NewUniform(outroColor), image.Point{}, draw.Src)
	if card := out[len(out)-1]; card.Cells != nil || !bytes.Equal(card.Image.Pix, want.Pix) {
		t.Error("badge after the card: the card changed")
	}
}

// TestPauseBadgeMerge checks that the GIF still merges the frames that look
// the same before the badged one.
func TestPauseBadgeMerge(t *testing.T) {
	t.Parallel()
	tl := badgeTimeline(t)
	for _, tc := range []struct {
		name    string
		effects []reel.Effect
		delays  []int
	}{
		{"none", nil, []int{140}},
		{"badge", []reel.Effect{PauseBadge(WithBadgeSize(8))}, []int{40, 100}},
	} {
		var in []reel.Frame
		path := filepath.Join(t.TempDir(), "out.gif")
		err := reel.Export(t.Context(), tl, reel.Profile{
			Outputs:    reel.Files(path),
			Rasterizer: blankRasterizer{},
			Effects:    append([]reel.Effect{recordFrames{&in}}, tc.effects...),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(in) != 4 {
			t.Fatalf("%s: %d terminal frames, want 4, the hold apart", tc.name, len(in))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(g.Delay, tc.delays) {
			t.Errorf("%s: GIF delays %v, want %v", tc.name, g.Delay, tc.delays)
		}
	}
}

// blankRasterizer is the fake rasterizer drawing every cell as if empty, so
// every frame of a terminal of one size looks alike.
type blankRasterizer struct{ testutil.Rasterizer }

func (blankRasterizer) Draw(dst *image.NRGBA, _ vt.Screen, _ vt.State, _ time.Duration) {
	for i := 0; i < len(dst.Pix); i += 4 {
		copy(dst.Pix[i:], []uint8{0, 0, 0xff, 0xff})
	}
}

// TestPauseBadgeGlyph checks the glyph's colors: bars of the color on a
// disc that contrasts with it, and nothing outside the disc.
func TestPauseBadgeGlyph(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fg, disc color.NRGBA
	}{
		{badgeColor, badgeDark},
		{color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}, badgeLight},
	} {
		img := pauseGlyph(30, tc.fg)
		// Bars 4 wide and 14 tall, with 4 between them, from (9, 8).
		for _, p := range []struct {
			x, y int
			want color.NRGBA
		}{
			{0, 0, color.NRGBA{}},
			{29, 29, color.NRGBA{}},
			{15, 15, tc.disc},
			{15, 4, tc.disc},
			{11, 15, tc.fg},
			{19, 15, tc.fg},
		} {
			got, _ := color.NRGBAModel.Convert(img.At(p.x, p.y)).(color.NRGBA)
			if !closeColor(got, p.want) {
				t.Errorf("fg %v: pixel (%d, %d) = %v, want %v", tc.fg, p.x, p.y, got, p.want)
			}
		}
	}
}

// closeColor reports whether a and b differ by at most 2 in each channel,
// as premultiplying rounds.
func closeColor(a, b color.NRGBA) bool {
	near := func(x, y uint8) bool { return max(x, y)-min(x, y) <= 2 }
	return near(a.R, b.R) && near(a.G, b.G) && near(a.B, b.B) && near(a.A, b.A)
}

// TestPauseBadgeStart checks the options Start rejects.
func TestPauseBadgeStart(t *testing.T) {
	t.Parallel()
	ctx := &reel.Context{Rasterizer: testutil.Rasterizer{}, Layout: testutil.Rasterizer{}.Layout(40, 10)}
	for i, opt := range []BadgeOption{WithBadgeSize(-1), WithBadgeSize(7), WithBadgeCorner(BottomRight + 1), WithBadgeCorner(-1)} {
		if _, err := PauseBadge(opt).Start(ctx); err == nil {
			t.Errorf("Start with bad option %d = nil error", i)
		}
	}
	if _, err := PauseBadge(WithBadgeSize(8), WithBadgeColor(nil)).Start(ctx); err != nil {
		t.Errorf("Start with size 8 = %v", err)
	}
}

func TestBadgeAt(t *testing.T) {
	t.Parallel()
	grid := image.Rect(5, 10, 105, 60)
	for corner, want := range map[Corner]image.Point{
		TopLeft:     {7, 12},
		TopRight:    {97, 12},
		BottomLeft:  {7, 52},
		BottomRight: {97, 52},
	} {
		if got := badgeAt(grid, 6, corner); got != want {
			t.Errorf("badgeAt corner %d = %v, want %v", corner, got, want)
		}
	}
}

// TestPauseBadgePieces checks that when the last frame comes in pieces of
// one terminal, the badge covers the final pieces that play for the hold,
// pieces that share an image share its badged copy, and the pieces of a
// terminal that is not last are left alone.
func TestPauseBadgePieces(t *testing.T) {
	t.Parallel()
	ctx := &reel.Context{
		Rasterizer: testutil.Rasterizer{},
		Layout:     testutil.Rasterizer{}.Layout(40, 10),
		Hold:       time.Second,
	}
	stage, err := PauseBadge(WithBadgeSize(8)).Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a, b := cellsOf(40, 10, ""), cellsOf(40, 10, "")
	img := func() *image.NRGBA { return image.NewNRGBA(ctx.Layout.Image) }
	shared := img()
	in := []reel.Frame{
		{Image: img(), Cells: a, Delay: 400 * ms, First: true},
		{Image: img(), Cells: a, Delay: 900 * ms},
		{Image: img(), Cells: b, Delay: 300 * ms},
		{Image: shared, Cells: b, Delay: 500 * ms, Hold: true},
		{Image: shared, Cells: b, Delay: 400 * ms, Hold: true},
		{Image: img(), Cells: b, Delay: 300 * ms, Hold: true, Last: true},
	}
	var out []reel.Frame
	for _, f := range in {
		stage(f, func(f reel.Frame) { out = append(out, f) })
	}
	if len(out) != len(in) {
		t.Fatalf("%d frames out, want %d", len(out), len(in))
	}
	// The last 300+400+500ms cover the hold of 1s.
	wantBadge := []bool{false, false, false, true, true, true}
	for i, f := range out {
		if f.Delay != in[i].Delay || f.Cells != in[i].Cells {
			t.Errorf("frame %d reordered or retimed", i)
		}
		if badged := f.Image != in[i].Image; badged != wantBadge[i] {
			t.Errorf("frame %d badged = %v, want %v", i, badged, wantBadge[i])
		}
	}
	if out[3].Image != out[4].Image {
		t.Error("pieces of one image got two badged copies")
	}
}

// TestPauseBadgeKeys checks that with [Keys] before it, which splits the
// last frame while keys fade, the badge still covers the hold.
func TestPauseBadgeKeys(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 10, "", output(0, "a"), input(300, "x"), output(300, "x"), mark(500, "end"))
	var mid []reel.Frame
	out := exportFrames(t, tl, reel.Profile{
		Effects: []reel.Effect{Keys(time.Second), recordFrames{&mid}, PauseBadge(WithBadgeSize(8))},
	})
	if len(out) != len(mid) || len(out) < 3 {
		t.Fatalf("%d frames in, %d out, want the key overlay's pieces", len(mid), len(out))
	}
	var badged time.Duration
	for i, f := range out {
		switch {
		case f.Image != mid[i].Image:
			badged += f.Delay
		case badged > 0:
			t.Errorf("frame %d after a badged one has no badge", i)
		}
	}
	if badged < time.Second {
		t.Errorf("badge plays for %v, want at least the hold of 1s", badged)
	}
}
