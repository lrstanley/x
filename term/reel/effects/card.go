// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"slices"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// cardCols and cardRows are the terminal size up to which card text is the
// size of the terminal font. Past it, card text grows (see [cardScale]).
const (
	cardCols = 80
	cardRows = 24
)

// Line is one line of text on a card.
type Line struct {
	// Text is the text, drawn as it is: escape sequences are not
	// interpreted.
	Text string
	// Style is how the text is drawn.
	Style vt.Style
}

// CardRenderer draws the cards of one recording: lines of text centered on
// an otherwise blank terminal, drawn by the recording's [reel.Rasterizer] at
// the size and with the window chrome of a terminal frame, so a card can sit
// beside the frames. Cards use the rasterizer's default colors, not ones the
// program set, so every card in a recording looks the same.
//
// The text is drawn by a renderer of its own, built from [reel.Context.Render]
// without window chrome, padding, margin, or scrollbar. On terminals larger
// than 80x24 the text is larger than the terminal font, so it stays readable
// when the recording is scaled down to fit a page, and the renderer is
// rebuilt when a terminal frame of a size that changes the text size comes
// along. Text that does not fit is cut off.
//
// A card renderer is for the stages of the recording it was made for, which
// run on one goroutine. It is closed when the recording ends, and must not be
// used after.
type CardRenderer struct {
	rast reel.Rasterizer
	// opts are the text renderer's options, but for its scale.
	opts []render.Option
	// base is the scale of the terminal font.
	base float64
	// cols and rows are the timeline's starting size, for cards drawn
	// without a terminal frame.
	cols, rows int

	text *render.Renderer
	// scale is the text renderer's card scale (see [cardScale]).
	scale float64
	// err is the errors rebuilding the text renderer and closing the ones
	// replaced, returned when the recording ends.
	err error
}

// NewCardRenderer returns a card renderer for the recording ctx describes,
// which is closed when the recording ends (see [reel.Context.OnClose]). It
// is for an [reel.Effect]'s Start: an error building the text renderer is
// returned there.
func NewCardRenderer(ctx *reel.Context) (*CardRenderer, error) {
	h := ctx.Timeline.Header()
	c := &CardRenderer{
		rast: ctx.Rasterizer,
		opts: append(slices.Clip(ctx.Render),
			render.WithPadding(0),
			render.WithMargin(0, nil),
			render.WithBorderRadius(0),
			render.WithScrollbar(false),
			render.WithWindowChrome(false),
		),
		base: ctx.Layout.Scale,
		cols: h.Cols,
		rows: h.Rows,
	}
	if !(c.base > 0) {
		c.base = 1
	}
	c.scale = cardScale(c.cols, c.rows)
	var err error
	if c.text, err = c.build(c.scale); err != nil {
		return nil, fmt.Errorf("effects: card text renderer: %w", err)
	}
	ctx.OnClose(c.close)
	return c, nil
}

func (c *CardRenderer) build(scale float64) (*render.Renderer, error) {
	return render.New(append(slices.Clip(c.opts), render.WithScale(c.base*scale))...)
}

func (c *CardRenderer) close() error {
	err := errors.Join(c.err, c.text.Close())
	c.err = nil
	return err
}

// Card returns a card of lines, centered, the size of src, a terminal frame
// of the recording, with its title and focus. A nil src is a terminal of the
// timeline's starting size, focused and untitled. The image is the caller's.
func (c *CardRenderer) Card(src *reel.CellFrame, lines ...Line) *image.NRGBA {
	cols, rows, st := c.source(src)
	card := image.NewNRGBA(c.rast.Layout(cols, rows).Image)
	// Cards hold still, so they are drawn as of offset 0.
	c.rast.Draw(card, vt.NewBuffer(cols, rows), st, 0)
	if text := c.drawText(cols, rows, st, lines); text != nil {
		b, tb := card.Bounds(), text.Bounds()
		at := b.Min.Add(image.Pt((b.Dx()-tb.Dx())/2, (b.Dy()-tb.Dy())/2))
		draw.Draw(card, tb.Sub(tb.Min).Add(at), text, tb.Min, draw.Src)
	}
	return card
}

// Text returns lines as a block of card text for src (see
// [CardRenderer.Card]), each line centered on the widest, without the
// terminal around it, cut to [CardRenderer.Grid]. Cells without a background
// of their own have the default background. It returns nil when there is no
// text. The image is the caller's.
func (c *CardRenderer) Text(src *reel.CellFrame, lines ...Line) *image.NRGBA {
	cols, rows, st := c.source(src)
	return c.drawText(cols, rows, st, lines)
}

// Grid returns how many columns and rows of card text fit on a card for src
// (see [CardRenderer.Card]).
func (c *CardRenderer) Grid(src *reel.CellFrame) (cols, rows int) {
	cols, rows, _ = c.source(src)
	return cardGrid(cols, rows)
}

// source returns the size of src and the state its cards are drawn with.
func (c *CardRenderer) source(src *reel.CellFrame) (cols, rows int, st vt.State) {
	if src == nil {
		return c.cols, c.rows, vt.State{Focused: true}
	}
	b := src.Screen.Bounds()
	return b.Dx(), b.Dy(), vt.State{Title: src.State.Title, Focused: src.State.Focused}
}

func (c *CardRenderer) drawText(cols, rows int, st vt.State, lines []Line) *image.NRGBA {
	gridCols, gridRows := cardGrid(cols, rows)
	var width int
	for _, line := range lines {
		width = max(width, textWidth(line.Text))
	}
	width, height := min(width, gridCols), min(len(lines), gridRows)
	if width == 0 || height == 0 {
		return nil
	}
	buf := vt.NewBuffer(width, height)
	for i, line := range lines[:height] {
		writeCentered(buf, i, line)
	}
	r := c.renderer(cardScale(cols, rows))
	img := image.NewNRGBA(r.Layout(width, height).Image)
	r.Draw(img, buf, st, 0)
	return img
}

// renderer returns the text renderer for card scale scale, rebuilding it
// when the scale changed. The options built one already, so rebuilding
// should not fail; when it does, it keeps the one it has, which draws at
// the old scale, and the error is returned when the recording ends.
func (c *CardRenderer) renderer(scale float64) *render.Renderer {
	if scale == c.scale {
		return c.text
	}
	r, err := c.build(scale)
	if err != nil {
		c.err = errors.Join(c.err, fmt.Errorf("effects: card text renderer: %w", err))
		c.scale = scale
		return c.text
	}
	c.err = errors.Join(c.err, c.text.Close())
	c.text, c.scale = r, scale
	return r
}

// cardScale is how much larger card text is than the terminal font on a
// cols by rows terminal. It grows at three quarters the rate the terminal
// grows past 80x24, so text on a large terminal stays readable without
// filling the card.
func cardScale(cols, rows int) float64 {
	grown := min(float64(cols)/cardCols, float64(rows)/cardRows)
	return max(1, 1+(grown-1)*3/4)
}

// cardGrid is the number of columns and rows of card text that fit on a
// cols by rows terminal.
func cardGrid(cols, rows int) (gridCols, gridRows int) {
	scale := cardScale(cols, rows)
	return int(float64(cols) / scale), int(float64(rows) / scale)
}

// textWidth is the width of s in cells, grapheme by grapheme.
func textWidth(s string) int {
	var width int
	for s != "" {
		g, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			break
		}
		width += w
		s = s[len(g):]
	}
	return width
}

// writeCentered writes line into row y of buf, centered by display width,
// and cut where it no longer fits.
func writeCentered(buf *vt.Buffer, y int, line Line) {
	x := max(0, (buf.Width()-textWidth(line.Text))/2)
	for s := line.Text; s != ""; {
		g, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			return
		}
		s = s[len(g):]
		if w == 0 {
			continue
		}
		if x+w > buf.Width() {
			return
		}
		buf.SetCell(x, y, &vt.Cell{Content: g, Style: line.Style, Width: w})
		x += w
	}
}

// cloneNRGBA returns a copy of img, to draw an overlay on.
func cloneNRGBA(img *image.NRGBA) *image.NRGBA {
	return &image.NRGBA{Pix: slices.Clone(img.Pix), Stride: img.Stride, Rect: img.Rect}
}
