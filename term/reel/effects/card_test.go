// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"image"
	"path/filepath"
	"testing"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// cardContext returns the context of a recording of an 80x24 timeline drawn
// with a renderer, which the test closes.
func cardContext(t *testing.T) *reel.Context {
	t.Helper()
	rend, err := render.New(render.WithWindowChrome(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rend.Close() })
	return &reel.Context{
		Timeline:   buildTimeline(t, 80, 24, "", output(0, "x")),
		Rasterizer: rend,
		Layout:     rend.Layout(80, 24),
		Step:       50 * ms,
		Speed:      1,
	}
}

// cellsOf is a terminal frame of a blank cols by rows screen titled title.
func cellsOf(cols, rows int, title string) *reel.CellFrame {
	return &reel.CellFrame{Screen: vt.NewBuffer(cols, rows), State: vt.State{Title: title, Focused: true}}
}

func TestCard(t *testing.T) {
	t.Parallel()
	ctx := cardContext(t)
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// A card without lines is the blank terminal, chrome and all.
	blank := cards.Card(nil)
	want := image.NewNRGBA(ctx.Layout.Image)
	ctx.Rasterizer.Draw(want, vt.NewBuffer(80, 24), vt.State{Focused: true}, 0)
	if blank.Rect != want.Rect || !bytes.Equal(blank.Pix, want.Pix) {
		t.Fatal("a card without lines is not the blank terminal")
	}

	// Lines are drawn in the middle, and only there.
	card := cards.Card(nil, Line{Text: "hello", Style: vt.Style{Attrs: vt.AttrBold}})
	text := cards.Text(nil, Line{Text: "hello"})
	if card.Rect != blank.Rect || text == nil {
		t.Fatalf("card is %v with text %v, want %v with text", card.Rect, text, blank.Rect)
	}
	b, tb := card.Bounds(), text.Bounds()
	middle := tb.Add(image.Pt((b.Dx()-tb.Dx())/2, (b.Dy()-tb.Dy())/2))
	for y := range b.Dy() {
		for x := range b.Dx() {
			if card.NRGBAAt(x, y) != blank.NRGBAAt(x, y) && !image.Pt(x, y).In(middle) {
				t.Fatalf("card differs from the blank terminal at %d,%d, outside the text at %v", x, y, middle)
			}
		}
	}
	if bytes.Equal(card.Pix, blank.Pix) {
		t.Fatal("card shows no text")
	}

	// A card is the size of its terminal frame, with its title.
	titled := cards.Card(cellsOf(40, 10, "titled"))
	if want := ctx.Rasterizer.Layout(40, 10).Image; titled.Rect != want {
		t.Fatalf("40x10 card is %v, want %v", titled.Rect, want)
	}
	if bytes.Equal(titled.Pix, cards.Card(cellsOf(40, 10, "")).Pix) {
		t.Fatal("the card's title bar does not show the frame's title")
	}
}

func TestCardText(t *testing.T) {
	t.Parallel()
	cards, err := NewCardRenderer(cardContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if cards.Text(nil) != nil || cards.Text(nil, Line{}) != nil {
		t.Fatal("text of no lines, or of empty lines, is not nil")
	}

	// Card text grows at three quarters the rate the terminal grows past
	// 80x24, and is cut to what fits.
	for _, tc := range []struct {
		cols, rows         int
		gridCols, gridRows int
	}{
		{40, 10, 40, 10},
		{80, 24, 80, 24},
		{160, 48, 91, 27},
		{160, 24, 160, 24},
	} {
		src := cellsOf(tc.cols, tc.rows, "")
		if cols, rows := cards.Grid(src); cols != tc.gridCols || rows != tc.gridRows {
			t.Errorf("%dx%d: grid %dx%d, want %dx%d", tc.cols, tc.rows, cols, rows, tc.gridCols, tc.gridRows)
		}
	}
	small := cards.Text(nil, Line{Text: "hi"})
	large := cards.Text(cellsOf(160, 48, ""), Line{Text: "hi"})
	if got, want := large.Bounds().Dx(), small.Bounds().Dx()*7/4; got < want-2 || got > want+2 {
		t.Errorf("text on 160x48 is %dpx wide, want about %dpx, 1.75 times 80x24's", got, want)
	}
	// Back to the first size, the text is drawn as it was.
	if again := cards.Text(nil, Line{Text: "hi"}); !bytes.Equal(again.Pix, small.Pix) {
		t.Error("text on 80x24 changed after drawing on 160x48")
	}

	long := cards.Text(cellsOf(10, 2, ""), Line{Text: "a line far too long"}, Line{Text: "b"}, Line{Text: "c"})
	cell := cards.Text(cellsOf(10, 2, ""), Line{Text: "a"}).Bounds().Size()
	if got := long.Bounds().Size(); got != image.Pt(10*cell.X, 2*cell.Y) {
		t.Errorf("text cut to a 10x2 grid is %v, want %v", got, image.Pt(10*cell.X, 2*cell.Y))
	}
}

// cardsEffect is an [reel.Effect] that keeps the card renderer it builds.
type cardsEffect struct{ cards **CardRenderer }

func (e cardsEffect) Start(ctx *reel.Context) (reel.Stage, error) {
	var err error
	*e.cards, err = NewCardRenderer(ctx)
	return func(f reel.Frame, emit func(reel.Frame)) { emit(f) }, err
}

// TestCardRendererClosed checks that a card renderer is closed when its
// recording ends.
func TestCardRendererClosed(t *testing.T) {
	t.Parallel()
	var cards *CardRenderer
	err := reel.Export(t.Context(), buildTimeline(t, 20, 4, "", output(0, "x")), reel.Profile{
		Outputs:    reel.MustFiles(filepath.Join(t.TempDir(), "x.gif")),
		Rasterizer: testutil.Rasterizer{},
		Effects:    []reel.Effect{cardsEffect{&cards}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("the card renderer still draws after the recording ended")
		}
	}()
	cards.Text(nil, Line{Text: "x"})
}
