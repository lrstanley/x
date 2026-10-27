// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
)

// highlightRast is the fake rasterizer of the highlight tests, with cells
// large enough to see an outline around, and a border around the grid that
// dimming darkens too.
var highlightRast = testutil.Rasterizer{Cell: image.Pt(3, 4), Pad: 2}

// red is an outline color the fake rasterizer does not draw.
var red = color.NRGBA{R: 0xff, A: 0xff}

// frameInfo is what a test checks of a frame, other than its image.
type frameInfo struct {
	at, delay time.Duration
	isolated  bool
	card      bool
}

func infoOf(frames []reel.Frame) []frameInfo {
	out := make([]frameInfo, len(frames))
	for i, f := range frames {
		out[i] = frameInfo{at: f.At, delay: f.Delay, isolated: f.Isolated, card: f.Cells == nil}
	}
	return out
}

// highlightFrames exports tl through effects, with the highlight test
// rasterizer at 100 FPS, and returns the frames the effects were given, and
// those they emitted. It checks that no input image was changed.
func highlightFrames(t *testing.T, tl *reel.Timeline, effects ...reel.Effect) (in, out []reel.Frame) {
	t.Helper()
	var pix [][]byte
	keep := effectFunc(func(f reel.Frame, emit func(reel.Frame)) {
		in = append(in, f)
		pix = append(pix, bytes.Clone(f.Image.Pix))
		emit(f)
	})
	out = exportFrames(t, tl, reel.Profile{
		FPS:        100,
		Hold:       -1,
		Rasterizer: highlightRast,
		Effects:    append([]reel.Effect{keep}, effects...),
	})
	for i, f := range in {
		if !bytes.Equal(f.Image.Pix, pix[i]) {
			t.Fatalf("frame %d was drawn on in place", i)
		}
	}
	return in, out
}

// effectFunc is an [reel.Effect] whose stage is fn.
type effectFunc func(f reel.Frame, emit func(reel.Frame))

func (e effectFunc) Start(*reel.Context) (reel.Stage, error) { return reel.Stage(e), nil }

// TestHighlightCells checks a fixed rectangle of cells, dimmed around and
// outlined, on every frame.
func TestHighlightCells(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 8, 3, "", output(0, "ab cd\r\nefgh"), output(100, "\r\nxyz"))
	in, out := highlightFrames(t, tl, Highlight(Cells(image.Rect(1, 0, 4, 2)), Outline(red, 1)))
	want := []frameInfo{{0, 100 * ms, true, false}, {100 * ms, 10 * ms, true, false}}
	if got := infoOf(out); !slices.Equal(got, want) {
		t.Fatalf("frames %+v, want %+v", got, want)
	}
	if len(in) != 2 {
		t.Fatalf("%d frames in, want 2", len(in))
	}
	assertGolden(t, "highlight-cells", out)
}

// TestHighlightTextMoves checks a text region that follows its text from
// frame to frame, matches it twice on a row, and leaves alone a frame that
// does not show it.
func TestHighlightTextMoves(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 10, 3, "",
		output(0, "a key b"),
		output(100, "\x1b[2J\x1b[2;3Hkey"),
		output(200, "\x1b[2J\x1b[Hnothing"),
		output(300, "\x1b[3;1Hkey keys"),
	)
	in, out := highlightFrames(t, tl, Highlight(Text("key"), DimOthers(0.75), Outline(red, 1)))
	if len(out) != 4 || len(in) != 4 {
		t.Fatalf("%d frames in and %d out, want 4", len(in), len(out))
	}
	if out[2].Image != in[2].Image || out[2].Isolated {
		t.Fatal("the frame without the text was changed")
	}
	assertGolden(t, "highlight-text", out)
}

// TestHighlightBetween checks a highlight limited to a window between two
// marks, easing in and out on the delay grid.
func TestHighlightBetween(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 6, 2, "",
		output(0, "ab"),
		mark(60, "on"),
		output(100, " cd"),
		mark(140, "off"),
		output(300, " ef"),
	)
	_, out := highlightFrames(t, tl, Highlight(Row(0), Between("on", "off"), Ease(20*ms)))
	// The window is from 60ms to 140ms, with 20ms of easing on either
	// side: steps whose middle is from 45ms to 55ms ease in, and 145ms to
	// 155ms ease out.
	want := []frameInfo{
		{0, 40 * ms, false, false},
		{40 * ms, 10 * ms, true, false},
		{50 * ms, 10 * ms, true, false},
		{60 * ms, 40 * ms, true, false},
		{100 * ms, 40 * ms, true, false},
		{140 * ms, 10 * ms, true, false},
		{150 * ms, 10 * ms, true, false},
		{160 * ms, 140 * ms, false, false},
		{300 * ms, 10 * ms, false, false},
	}
	if got := infoOf(out); !slices.Equal(got, want) {
		t.Fatalf("frames\n got %+v\nwant %+v", got, want)
	}
	assertGolden(t, "highlight-between", out)
}

// cardEffect inserts a card, a frame without a terminal, before each
// terminal frame.
type cardEffect struct{}

func (cardEffect) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		card := image.NewNRGBA(f.Image.Rect)
		for i := range card.Pix {
			card.Pix[i] = 0xc0
		}
		emit(reel.Frame{Image: card, Delay: 50 * ms, Isolated: true})
		emit(f)
	}, nil
}

// TestHighlightCard checks that a card passes through untouched.
func TestHighlightCard(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 6, 2, "", output(0, "abc"))
	var cards []reel.Frame
	_, out := highlightFrames(t, tl, cardEffect{}, recordFrames{&cards}, Highlight(Rows(0, 2), Outline(nil, 0)))
	if len(out) != 2 || out[0].Cells != nil || out[0].Image != cards[0].Image || !out[0].Isolated || out[0].Delay != 50*ms {
		t.Fatalf("frames %+v, want the card untouched and the terminal frame", infoOf(out))
	}
	assertGolden(t, "highlight-card", out)
}

func TestHighlightErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "", output(0, "a"), mark(10, "m"))
	ctx := &reel.Context{Timeline: tl, Rasterizer: highlightRast, Layout: highlightRast.Layout(4, 1), Step: 10 * ms, Speed: 1}
	for _, tc := range []struct {
		name   string
		effect reel.Effect
		is     error
	}{
		{"nil region", Highlight(nil), nil},
		{"dim below 0", Highlight(Row(0), DimOthers(-0.1)), nil},
		{"dim above 1", Highlight(Row(0), DimOthers(1.5)), nil},
		{"dim NaN", Highlight(Row(0), DimOthers(math.NaN())), nil},
		{"outline width", Highlight(Row(0), Outline(red, -1)), nil},
		{"ease", Highlight(Row(0), Ease(-ms)), nil},
		{"unknown mark", Highlight(Row(0), Between("m", "nope")), reel.ErrNoMark},
	} {
		st, err := tc.effect.Start(ctx)
		if err == nil || st != nil || (tc.is != nil && !errors.Is(err, tc.is)) {
			t.Errorf("%s: Start = %v, want an error", tc.name, err)
		}
	}
	// Neither dimming nor outlining is no stage.
	for _, e := range []reel.Effect{
		Highlight(Row(0), DimOthers(0)),
		Highlight(Row(0), DimOthers(0), Outline(color.Transparent, 2)),
	} {
		if st, err := e.Start(ctx); st != nil || err != nil {
			t.Errorf("Start = %v, %v, want no stage", st != nil, err)
		}
	}
}

// TestHighlightOutlineOnly checks that an outline without dimming leaves
// the frame's colors, and its GIF palette, alone outside the outline.
func TestHighlightOutlineOnly(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 6, 2, "", output(0, "abc"))
	in, out := highlightFrames(t, tl, Highlight(Text("b"), DimOthers(0), Outline(red, 1)))
	if len(out) != 1 || out[0].Isolated || out[0].Image == in[0].Image {
		t.Fatalf("frames %+v, want one outlined frame on the shared palette", infoOf(out))
	}
	// Cell (1, 0) is pixels 5 to 8 by 2 to 6; the outline is one pixel
	// around it, and nothing else changes.
	b := out[0].Image.Rect
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := image.Pt(x, y)
			onOutline := p.In(image.Rect(4, 1, 9, 7)) && !p.In(image.Rect(5, 2, 8, 6))
			got, was := out[0].Image.NRGBAAt(x, y), in[0].Image.NRGBAAt(x, y)
			if onOutline && got != red || !onOutline && got != was {
				t.Fatalf("pixel %v = %v, want %v", p, got, map[bool]color.NRGBA{true: red, false: was}[onOutline])
			}
		}
	}
}
