// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// badgeMinSize is the smallest badge, in pixels, the glyph fits in.
const badgeMinSize = 8

var (
	// badgeColor is the default color of the pause bars, the light grey of
	// the [Keys] chip text, xterm 255.
	badgeColor = color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}
	// badgeDark and badgeLight are the translucent discs behind light and
	// dark bars. badgeDark is the grey of the [Keys] chips, xterm 238.
	badgeDark  = color.NRGBA{R: 0x44, G: 0x44, B: 0x44, A: 0xc0}
	badgeLight = color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xc0}
)

// Corner is a corner of the terminal's cell grid in a frame, where an
// effect places an overlay.
type Corner int

// The corners of the cell grid.
const (
	TopLeft Corner = iota
	TopRight
	BottomLeft
	BottomRight
)

// BadgeOption configures [PauseBadge].
type BadgeOption func(*pauseBadge)

// WithBadgeCorner puts the badge in corner c of the terminal's cell grid,
// inset by a third of its size. The default, [TopRight], sits below any
// window chrome and clear of the [Keys] overlay, which is bottom right:
// with [BottomRight], keys still shown on the last frame overlap it.
func WithBadgeCorner(c Corner) BadgeOption {
	return func(b *pauseBadge) { b.corner = c }
}

// WithBadgeSize sets the badge's diameter in pixels. Zero, the default, is
// one and a half times the height of a terminal cell
// ([reel.Context.Layout]), and at least 8. Any other size below 8, which
// the glyph does not fit, is an error from Start.
func WithBadgeSize(px int) BadgeOption {
	return func(b *pauseBadge) { b.size = px }
}

// WithBadgeColor sets the color of the pause bars. The default, and a nil
// c, is the light grey of the [Keys] chip text. The disc behind the bars is
// a translucent dark grey for a light color, and a translucent light grey
// for a dark one.
func WithBadgeColor(c color.Color) BadgeOption {
	return func(b *pauseBadge) {
		b.color = badgeColor
		if c != nil {
			b.color, _ = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
	}
}

// PauseBadge returns an effect that draws a pause glyph, two bars on a
// translucent disc, in a corner of the last terminal frame, so a viewer
// reads the end of a looping recording as finished and about to loop,
// rather than frozen. The badge is drawn on copies of the images, and the
// frames before the last are left alone, so duplicate frame merging still
// merges them.
//
// The badge covers the last frame, which plays for [reel.Profile.Hold] past
// its own time. When an effect before PauseBadge, such as [Keys], or an
// indicator that moves while the screen holds still has split the last
// frame into pieces, the badge covers the final pieces that play for the
// hold, and at least the final one. For that, the stage holds back the
// pieces of the newest terminal frame that play for the hold, until a frame
// of another terminal comes along.
//
// Only a terminal frame gets the badge: when the last frame has no terminal
// ([reel.Frame.Cells] is nil), as when an effect listed before PauseBadge
// adds a card after the last frame, no frame gets it. List PauseBadge
// before such an effect, so the badge lands on the held terminal frame and
// not on the card.
//
// A recording that plays once (sink.WithLoopCount(sink.LoopOnce)) stays on
// its last frame when it ends, badge and all, where the glyph reads as
// paused rather than about to loop, so such a profile may leave the effect
// out. Effects change only the frames of recordings: a PNG output, which
// writes the last frame, gets the badge, but the stills of
// [reel.File.Marks] get no effects at all.
func PauseBadge(opts ...BadgeOption) reel.Effect {
	b := pauseBadge{corner: TopRight, color: badgeColor}
	for _, opt := range opts {
		opt(&b)
	}
	return b
}

type pauseBadge struct {
	corner Corner
	size   int
	color  color.NRGBA
}

func (e pauseBadge) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.corner < TopLeft || e.corner > BottomRight {
		return nil, fmt.Errorf("effects: pause badge corner %d out of range", e.corner)
	}
	size := e.size
	switch {
	case size == 0:
		size = max(badgeMinSize, ctx.Layout.Cell.Y*3/2)
	case size < badgeMinSize:
		return nil, fmt.Errorf("effects: pause badge size %d below %d", size, badgeMinSize)
	}
	s := &badgeStage{ctx: ctx, corner: e.corner, size: size, glyph: pauseGlyph(size, e.color)}
	return s.run, nil
}

// badgeStage is the badge for one recording.
type badgeStage struct {
	ctx    *reel.Context
	corner Corner
	size   int
	glyph  *image.RGBA

	// pieces are the newest frames of one terminal (see [PauseBadge]),
	// held back until a frame shows whether they are last, and delay is how
	// long they play. Older pieces of that terminal are sent on once the
	// newer ones play for the hold.
	pieces []reel.Frame
	delay  time.Duration
}

func (s *badgeStage) run(f reel.Frame, emit func(reel.Frame)) {
	if len(s.pieces) > 0 && (f.Cells == nil || f.Cells.Screen != s.pieces[0].Cells.Screen) {
		s.flush(emit, false)
	}
	if f.Cells == nil {
		emit(f)
		return
	}
	s.pieces = append(s.pieces, f)
	s.delay += f.Delay
	for len(s.pieces) > 1 && s.delay-s.pieces[0].Delay >= s.ctx.Hold {
		s.delay -= s.pieces[0].Delay
		emit(s.pieces[0])
		s.pieces = s.pieces[1:]
	}
	if f.Last {
		s.flush(emit, true)
	}
}

// flush sends on the held pieces, with the badge when badge is set. Pieces
// that share an image share its badged copy.
func (s *badgeStage) flush(emit func(reel.Frame), badge bool) {
	var src, badged *image.NRGBA
	for _, f := range s.pieces {
		if badge {
			if f.Image != src {
				b := f.Cells.Screen.Bounds()
				grid := s.ctx.Rasterizer.Layout(b.Dx(), b.Dy()).Grid
				src, badged = f.Image, cloneNRGBA(f.Image)
				draw.Draw(badged, s.glyph.Rect.Add(badgeAt(grid, s.size, s.corner)), s.glyph, image.Point{}, draw.Over)
			}
			f.Image = badged
		}
		emit(f)
	}
	s.pieces, s.delay = nil, 0
}

// badgeAt is where a badge of size goes in corner of grid, a third of its
// size in from the edges.
func badgeAt(grid image.Rectangle, size int, corner Corner) image.Point {
	inset := size / 3
	at := image.Pt(grid.Min.X+inset, grid.Min.Y+inset)
	if corner == TopRight || corner == BottomRight {
		at.X = grid.Max.X - inset - size
	}
	if corner == BottomLeft || corner == BottomRight {
		at.Y = grid.Max.Y - inset - size
	}
	return at
}

// pauseGlyph draws the badge: two bars of fg, rounded, on a disc of size
// that contrasts with fg, antialiased by each pixel's coverage. The bars'
// edges fall on whole pixels, so they stay sharp at small sizes.
func pauseGlyph(size int, fg color.NRGBA) *image.RGBA {
	disc := badgeDark
	if 299*int(fg.R)+587*int(fg.G)+114*int(fg.B) < 128*1000 {
		disc = badgeLight
	}

	// Each bar is an eighth of the disc wide and over two fifths tall, with
	// a gap of a ninth between them. The gap and height take the parity of
	// size, so the bars, mirrored about the center, start on whole pixels.
	barW := max(1, int(math.Round(float64(size)/8)))
	barH := int(math.Round(float64(size) * 0.42))
	barH += (size - barH) % 2
	gap := max(1, int(math.Round(float64(size)/9)))
	gap += (size - 2*barW - gap) % 2
	// off is how far each bar's center is from the disc's.
	off := float64(barW+gap) / 2
	halfW, halfH := float64(barW)/2, float64(barH)/2
	radius := float64(barW) / 3

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			discCov := clamp01(r - math.Hypot(dx, dy) + 0.5)
			if discCov == 0 {
				continue
			}
			barCov := clamp01(0.5 - roundedBox(math.Abs(dx)-off, dy, halfW, halfH, radius))
			img.SetRGBA(x, y, over(fg, barCov, disc, discCov))
		}
	}
	return img
}

// roundedBox is the signed distance from (x, y) to a box centered on the
// origin, halfW wide and halfH tall each way, with corners rounded by
// radius: negative inside.
func roundedBox(x, y, halfW, halfH, radius float64) float64 {
	qx := math.Abs(x) - halfW + radius
	qy := math.Abs(y) - halfH + radius
	return math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - radius
}

// over is fg at coverage fgCov over bg at coverage bgCov, premultiplied.
func over(fg color.NRGBA, fgCov float64, bg color.NRGBA, bgCov float64) color.RGBA {
	fa := float64(fg.A) / 0xff * fgCov
	ba := float64(bg.A) / 0xff * bgCov * (1 - fa)
	channel := func(f, b uint8) uint8 {
		return uint8(math.Round(float64(f)*fa + float64(b)*ba))
	}
	return color.RGBA{
		R: channel(fg.R, bg.R),
		G: channel(fg.G, bg.G),
		B: channel(fg.B, bg.B),
		A: uint8(math.Round((fa + ba) * 0xff)),
	}
}

func clamp01(v float64) float64 {
	return min(1, max(0, v))
}
