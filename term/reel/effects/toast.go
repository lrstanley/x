// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"image/color"
	"image/draw"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/plaintext"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	// toastsMax is how many toasts show at once. Older ones drop off.
	toastsMax = 3
	// toastMaxText is the longest title or body a toast shows before cutting
	// it.
	toastMaxText = 40
	// toastShown is how long a toast stays by default.
	toastShown = 2 * time.Second
	// toastIn and toastOut are how long a toast takes to fade in and out,
	// in toastInLevels and toastOutLevels steps of opacity. Each step is a
	// frame that defeats duplicate frame merging, so keep them few.
	toastIn        = 150 * time.Millisecond
	toastOut       = 300 * time.Millisecond
	toastInLevels  = 3
	toastOutLevels = 4
)

// The colors of a toast: a dark blue-grey panel with a lighter edge, a
// near-white title, and a grey body.
var (
	toastBg     = color.NRGBA{R: 0x26, G: 0x28, B: 0x2e, A: 0xff}
	toastBorder = color.NRGBA{R: 0x3c, G: 0x3f, B: 0x47, A: 0xff}
	toastShadow = color.NRGBA{A: 0x60}
	toastTitle  = vt.Style{Fg: color.NRGBA{R: 0xf2, G: 0xf2, B: 0xf2, A: 0xff}, Bg: toastBg, Attrs: vt.AttrBold}
	toastBody   = vt.Style{Fg: color.NRGBA{R: 0xa8, G: 0xa8, B: 0xa8, A: 0xff}, Bg: toastBg}
)

// ToastOption configures [Toasts].
type ToastOption func(*toasts)

// WithToastCorner puts the toasts in corner c of the terminal's cell grid,
// newest nearest the corner. The default is [TopRight].
func WithToastCorner(c Corner) ToastOption {
	return func(t *toasts) { t.corner = c }
}

// WithToastDuration sets how long each toast stays, fades included. Zero or
// less is the default, 2s.
func WithToastDuration(d time.Duration) ToastOption {
	return func(t *toasts) { t.shown = d }
}

// Toasts returns an effect that shows the program's desktop notifications,
// OSC 9 and OSC 777 ([vt.State.Notifications]), as toasts on the terminal
// frames: rounded panels with the title in bold over the body, each cut to
// 40 columns. A toast slides and fades in, stays, then fades out, 2s in all
// by default. Up to three show at once, stacked in a corner of the
// terminal's cell grid.
//
// Only the notifications in the timeline's window are shown, and a [Hide]
// listed before Toasts leaves out those that arrive in what it cuts.
func Toasts(opts ...ToastOption) reel.Effect {
	t := toasts{corner: TopRight}
	for _, o := range opts {
		o(&t)
	}
	return t
}

type toasts struct {
	shown  time.Duration
	corner Corner
}

func (e toasts) Start(ctx *reel.Context) (reel.Stage, error) {
	if err := e.corner.check("toast"); err != nil {
		return nil, err
	}
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	shown := e.shown
	if shown <= 0 {
		shown = toastShown
	}
	s := &toastStage{
		ctx:    ctx,
		shown:  max(shown.Round(ctx.Step), ctx.Step),
		corner: e.corner,
		cards:  cards,
		drawn:  make(map[toastKey]toastCard),
	}
	s.from, _ = ctx.Timeline.Window()
	s.cuts, _ = ctx.Value(cutsKey{}).([]*cuts)
	return s.run, nil
}

// toastStage is the toasts of one recording.
type toastStage struct {
	ctx *reel.Context
	// shown is how long a toast stays, in whole steps.
	shown  time.Duration
	corner Corner
	cards  *CardRenderer
	// from is the start of the timeline's window: notifications before it
	// are not shown.
	from time.Duration
	// cuts are those of each [Hide] before Toasts, whose notifications it
	// leaves out.
	cuts []*cuts
	// drawn are the toasts drawn for the frames so far, by what they show.
	// Only those of the latest frame are kept.
	drawn map[toastKey]toastCard
}

// toastKey is what a toast's image depends on.
type toastKey struct {
	title, body string
	cols, rows  int
	focused     bool
}

// toastLook is how a toast shows at one moment: its opacity, zero when it
// is not shown, and how far it has come in, from 0 to 255.
type toastLook struct {
	alpha, in uint8
}

// look is how the toast that arrived at offset at shows at offset p, on
// the step grid: it fades and slides in over toastIn, and fades out over
// the last toastOut of shown, each in a few steps of opacity.
func (s *toastStage) look(at, p time.Duration) toastLook {
	d := p - at
	if d < 0 || d >= s.shown {
		return toastLook{}
	}
	step := s.ctx.Step
	in := quantize(float64(d+step)/float64(toastIn+step), toastInLevels)
	out := quantize(float64(s.shown-d)/float64(toastOut+step), toastOutLevels)
	return toastLook{alpha: uint8(math.Round(255 * min(in, out))), in: uint8(math.Round(255 * in))}
}

// quantize rounds v, from 0, up to the next of levels steps, at most 1.
func quantize(v float64, levels int) float64 {
	n := float64(levels)
	return min(1, math.Ceil(v*n-1e-9)/n)
}

// toastState is how each of a frame's toasts shows at one moment, newest
// first. The toasts shown come first (see [toastStage.toasts]).
type toastState [toastsMax]toastLook

func (s *toastStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	ts := s.toasts(f)
	if len(ts) == 0 {
		emit(f)
		return
	}
	// The look may change on any step until the newest toast, which goes
	// last, is gone; splitFrame merges the steps that look the same.
	var cuts []time.Duration
	for p := time.Duration(0); p <= ts[0].at+s.shown && p < f.Delay; p += s.ctx.Step {
		cuts = append(cuts, p)
	}
	state := func(p time.Duration) toastState {
		var st toastState
		for i, t := range ts {
			st[i] = s.look(t.at, p)
		}
		return st
	}
	splitFrame(s.ctx, f, trimCuts(cuts, f.Delay, s.ctx.Step), state, func(st toastState) *image.NRGBA {
		return s.draw(f, ts, st)
	}, emit)
}

// toast is a notification a frame shows, the offset into the frame's delay
// it arrived at, which is zero or less, and its card.
type toast struct {
	at   time.Duration
	key  toastKey
	card toastCard
}

// toasts returns the toasts f may show, newest first: the newest
// toastsMax of its notifications with a title or body, and room for a
// card, that are still shown at its start. Every toast is shown as long,
// and arrived at or before f, so whenever an older one shows, the newer
// ones do too.
func (s *toastStage) toasts(f reel.Frame) []toast {
	ns := f.Cells.State.Notifications
	b := f.Cells.Screen.Bounds()
	var out []toast
	// seen are the toasts looked at, which keep their cards, even one with
	// no room, so it is not drawn again each frame.
	var seen []toastKey
	defer func() {
		maps.DeleteFunc(s.drawn, func(k toastKey, _ toastCard) bool { return !slices.Contains(seen, k) })
	}()
	for i := len(ns) - 1; i >= 0 && len(out) < toastsMax && ns[i].At >= s.from; i-- {
		at := playOffset(s.ctx, f, ns[i].At)
		if at+s.shown <= 0 {
			break
		}
		key := toastKey{
			title:   toastText(ns[i].Title),
			body:    toastText(ns[i].Body),
			cols:    b.Dx(),
			rows:    b.Dy(),
			focused: f.Cells.State.Focused,
		}
		if (key.title == "" && key.body == "") || hidden(s.cuts, ns[i].At) {
			continue
		}
		seen = append(seen, key)
		if card := s.card(f.Cells, key); card.panel != nil {
			out = append(out, toast{at: at, key: key, card: card})
		}
	}
	return out
}

// toastText is untrusted text made one plain line (see [plaintext.Line]),
// cut to toastMaxText.
func toastText(s string) string {
	return ansi.Truncate(plaintext.Line(s, 4*toastMaxText), toastMaxText, "…")
}

// toastInset is how far toasts sit in from the edges of the grid, and how
// far they slide in from.
func (s *toastStage) toastInset() int { return s.ctx.Layout.Cell.Y / 2 }

// draw returns a copy of f's image with the toasts st shows stacked in the
// corner, or f's image when it shows none. They sit a quarter of a cell
// apart. Every shadow is drawn before any panel, so none darkens another
// toast.
func (s *toastStage) draw(f reel.Frame, ts []toast, st toastState) *image.NRGBA {
	if st[0].alpha == 0 {
		return f.Image
	}
	b := f.Cells.Screen.Bounds()
	grid := s.ctx.Rasterizer.Layout(b.Dx(), b.Dy()).Grid
	inset, gap := s.toastInset(), s.ctx.Layout.Cell.Y/4
	ats := make([]image.Point, len(ts))
	for i, t := range ts {
		size := t.card.panel.Bounds().Size()
		at := cornerAt(grid, size, inset, s.corner)
		// The next toast goes past this one, away from the corner.
		if s.corner == TopLeft || s.corner == TopRight {
			grid.Min.Y = at.Y + size.Y + gap - inset
		} else {
			grid.Max.Y = at.Y - gap + inset
		}
		slide := inset * int(255-st[i].in) / 255
		if s.corner == TopLeft || s.corner == BottomLeft {
			slide = -slide
		}
		ats[i] = at.Add(image.Pt(slide, 0))
	}
	img := cloneNRGBA(f.Image)
	for i, t := range ts {
		drawFaded(img, t.card.shadow, ats[i].Add(t.card.shadow.Rect.Min), st[i].alpha)
	}
	for i, t := range ts {
		drawFaded(img, t.card.panel, ats[i], st[i].alpha)
	}
	return img
}

// toastCard is a toast's panel, and its shadow, placed relative to the
// panel's top left corner. A nil panel is no toast.
type toastCard struct {
	panel, shadow *image.NRGBA
}

// card returns the card of the toast key, drawn on the terminal of src: its
// title and body on a rounded panel, cut to fit in the grid, with a shadow
// below. Its padding, corners, and shadow scale with the card text.
func (s *toastStage) card(src *reel.CellFrame, key toastKey) toastCard {
	if card, ok := s.drawn[key]; ok {
		return card
	}
	var card toastCard
	b := src.Screen.Bounds()
	room := s.ctx.Rasterizer.Layout(b.Dx(), b.Dy()).Grid.Dx() - 2*s.toastInset()
	gridCols, _ := s.cards.Grid(src)
	cols := min(max(textWidth(key.title), textWidth(key.body)), gridCols)
	if text, row := s.cardText(src, key, cols); text != nil {
		// Cut the text by the columns its padding does not leave room for.
		if excess := text.Rect.Dx() + 2*toastPadX(row) - room; excess > 0 {
			cell := text.Rect.Dx() / cols
			cols -= (excess + cell - 1) / cell
			text, row = s.cardText(src, key, cols)
		}
		if text != nil {
			card = toastPanel(text, row)
		}
	}
	s.drawn[key] = card
	return card
}

// cardText returns the title and body of key as card text cut to cols
// columns, each padded to the wider, and the height of a row, or nil when
// none fits.
func (s *toastStage) cardText(src *reel.CellFrame, key toastKey, cols int) (text *image.NRGBA, row int) {
	if cols < 1 {
		return nil, 0
	}
	title, body := ansi.Truncate(key.title, cols, "…"), ansi.Truncate(key.body, cols, "…")
	width := max(textWidth(title), textWidth(body))
	var lines []Line
	for _, l := range []Line{{title, toastTitle}, {body, toastBody}} {
		if l.Text != "" {
			l.Text += strings.Repeat(" ", width-textWidth(l.Text))
			lines = append(lines, l)
		}
	}
	if text = s.cards.Text(src, lines...); text == nil {
		return nil, 0
	}
	return text, text.Rect.Dy() / len(lines)
}

// toastPadX is the padding either side of a toast's text, for card text
// rows of height row.
func toastPadX(row int) int { return row / 2 }

// toastPanel draws text, card text on the toast background with rows of
// height row, on a rounded panel with a one pixel edge, and a soft shadow
// below it.
func toastPanel(text *image.NRGBA, row int) toastCard {
	padX, padY := toastPadX(row), row*9/20
	blur, drop := max(1, row/3), row/6
	tb := text.Bounds()
	panel := image.NewNRGBA(image.Rect(0, 0, tb.Dx()+2*padX, tb.Dy()+2*padY))
	shadow := image.NewNRGBA(panel.Rect.Inset(-blur).Add(image.Pt(0, drop)))

	halfW, halfH := float64(panel.Rect.Dx())/2, float64(panel.Rect.Dy())/2
	radius := float64(row) * 0.4
	for y := shadow.Rect.Min.Y; y < shadow.Rect.Max.Y; y++ {
		for x := shadow.Rect.Min.X; x < shadow.Rect.Max.X; x++ {
			dx, dy := float64(x)+0.5-halfW, float64(y)+0.5-halfH
			d := roundedBox(dx, dy, halfW, halfH, radius)
			if image.Pt(x, y).In(panel.Rect) {
				panel.Set(x, y, over(toastBg, clamp01(-0.5-d), toastBorder, clamp01(0.5-d)))
			}
			// The shadow is black, so only its opacity is set, and none
			// where the panel covers it.
			sa := clamp01(1-roundedBox(dx, dy-float64(drop), halfW, halfH, radius)/float64(blur)) * (1 - clamp01(0.5-d))
			shadow.SetNRGBA(x, y, color.NRGBA{A: uint8(math.Round(sa * float64(toastShadow.A)))})
		}
	}
	draw.Draw(panel, tb.Sub(tb.Min).Add(image.Pt(padX, padY)), text, tb.Min, draw.Src)
	return toastCard{panel: panel, shadow: shadow}
}
