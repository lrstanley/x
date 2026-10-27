// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"image"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// RedactOption configures [Redact]: how it hides cells, with [Block],
// [Blur], or [Replace], [Between], a [Timing] option, and [MinPartial], a
// [Matching] option. [Ease], [EaseIn], and [EaseOut] are an error from
// Start.
type RedactOption interface {
	applyRedact(r *redact)
}

type redactOption func(*redact)

func (o redactOption) applyRedact(r *redact) { o(r) }

func (o Timing) applyRedact(r *redact) {
	if o != nil {
		o(&r.timing)
	}
}

// redactBlurred is the text a cell [Blur] hides shows before it is
// blurred: it blurs into the look of blurred text.
const redactBlurred = "x"

// redactMode is how [Redact] hides a cell.
type redactMode uint8

const (
	redactBlock redactMode = iota
	redactBlur
	redactReplace
)

// Block hides each cell [Redact] covers as a blank cell in the background
// the cell is drawn with: its text, colors but the background, underline,
// and attributes are dropped, but for reverse video, whose background is
// the text color, so the cells look like the blank part of their row. It is
// the default.
func Block() RedactOption {
	return redactOption(func(r *redact) { r.mode = redactBlock })
}

// Blur hides each cell [Redact] covers as a blurred shape of text: the cell
// shows an x in its own colors, as with [Replace]('x'), and the pixels of
// the cells are then blurred, so the frame looks like it holds blurred
// text. What the cells held is never drawn, so no amount of sharpening
// brings it back. Effects after Redact that draw cells of their own, such
// as [Zoom], draw the x's unblurred. Frames with blurred cells are
// palettized on their own GIF palette ([reel.Frame.Isolated]).
func Blur() RedactOption {
	return redactOption(func(r *redact) { r.mode = redactBlur })
}

// Replace hides each cell [Redact] covers as the character c, in the cell's
// own colors and attributes, so "hunter2" shows as "•••••••" with
// Replace('•'). A wide character, which covers two cells, shows c twice. c
// must be a printable character one cell wide, or Start returns an error.
func Replace(c rune) RedactOption {
	return redactOption(func(r *redact) { r.mode, r.mask = redactReplace, c })
}

// Redact returns an effect that hides the cells of region, such as a
// secret, from every frame it passes on: it edits a copy of each terminal
// frame's cells, hiding those of the region with [Block], the default,
// [Blur], or [Replace], and draws the frame again from them with the
// recording's rasterizer, in place of the frame drawn before the effects.
// The frames it passes on hold the edited cells in [reel.Frame.Cells], so
// the effects after it, such as [Zoom], which draws cells of its own, never
// see what it hid. A frame the region covers no cells of is left alone, as
// are frames without a terminal, such as cards.
//
// [Text] and [Env] regions also hide their text where only part of it
// shows: its first three characters or more where it is cut off, at the end
// of a line, at the cursor, or before a character that is not a letter or
// digit, as a secret shows while it is typed into a prompt that echoes it,
// or cut off by the edge of a box; and its last three characters or more at
// the start of a line, or after a character that is not a letter or digit,
// as the rest of a secret soft-wrapped over rows shows once its first row
// scrolls away. A text of three characters or fewer is only hidden whole.
// [MinPartial] sets how many characters, three by default.
// They, and [Regexp] regions, also hide their text in the terminal's title
// and the program status (OSC 7501) the frame is drawn with, as a • for
// each character, and in [reel.Context.Title], which the effects after
// Redact read, such as [Intro]. [Cells], [Row], and [Rows] regions hide
// cells only.
//
// Redact is a [reel.Concealer]: list it, and [RedactInputs], before every
// other effect, such as [Keys], [Highlight], or [Intro]. What the effects
// after it draw themselves, such as a card of a caller's own, is not
// redacted. A redacted recording can not be saved as asciicast. A still of
// [reel.File.Marks] is redacted as a frame of the recording that starts at
// its mark would be.
//
// Redact can only hide what it finds, in the text as the program wrote it.
// Text a program lays out over lines itself, by moving the cursor, as
// full-screen programs do, rather than soft-wrapped by the terminal, is
// not matched across them (see [Text]), nor is a middle part of a secret
// cut off on both sides, such as by a field that scrolls, nor one a program
// spells with characters that draw as nothing, such as zero-width spaces.
// The first two characters of a secret typed into a prompt show before it
// is matched (as many as [MinPartial] allows). Hide the cells of such
// prompts with [Cells] or [Row] where that matters.
//
// [Between] limits Redact to a window of the timeline. A step of the delay
// grid that shows any of the window is redacted whole, as is the rest of a
// live recording ([reel.Record]) once its from mark is appended. [Ease],
// [EaseIn], and [EaseOut] are an error from Start, since easing would show
// part of what is hidden. A nil region is an error from Start.
func Redact(region Region, opts ...RedactOption) reel.Effect {
	r := redact{region: region, matching: defaultMatching}
	for _, opt := range opts {
		if opt != nil {
			opt.applyRedact(&r)
		}
	}
	return r
}

type redact struct {
	region Region
	timing timing
	matching
	mode redactMode
	mask rune
}

// Conceals implements [reel.Concealer].
func (redact) Conceals() bool { return true }

func (e redact) Start(ctx *reel.Context) (reel.Stage, error) {
	switch {
	case e.region == nil:
		return nil, errors.New("effects: redact without a region")
	case e.mode == redactReplace && (!unicode.IsPrint(e.mask) || ansi.StringWidth(string(e.mask)) != 1):
		return nil, fmt.Errorf("effects: redact replacement %q is not a printable character one cell wide", e.mask)
	}
	if err := e.check(); err != nil {
		return nil, err
	}
	w, err := e.timing.start(ctx, coverWindow)
	if err != nil {
		return nil, err
	}
	ctx.Title = secretsOf(e.region, e.matching).scrub(ctx.Title)
	s := &redactStage{redact: e, window: w, rast: ctx.Rasterizer}
	return s.run, nil
}

// redactStage is the redaction of one recording.
type redactStage struct {
	redact
	window *window
	rast   reel.Rasterizer

	// layout is the rasterizer's layout for screens of size, for Blur.
	layout render.Layout
	size   image.Point

	// src and srcState are the terminal hidden last, which frames of one
	// terminal share, such as those a held frame is played as, and
	// hidden is it as hidden, or nil when the region covers nothing of
	// it. covered is the cells hidden, or nil.
	src      *vt.Buffer
	srcState vt.State
	hidden   *reel.CellFrame
	covered  []bool
}

func (s *redactStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	pieces := s.window.pieces(f)
	if len(pieces) == 1 && pieces[0].level == 0 {
		emit(f)
		return
	}
	cells, img := s.hide(f)
	if cells == nil {
		emit(f)
		return
	}
	for _, p := range pieces {
		out := f
		out.At = f.At + time.Duration(float64(p.start)*s.window.speed)
		out.Delay = p.delay
		if p.level > 0 {
			out.Image, out.Cells = img, cells
			out.Isolated = f.Isolated || s.mode == redactBlur
		}
		emit(out)
	}
}

// hide returns f's terminal with what the region covers hidden, and its
// image drawn from it, or nil when the region covers nothing.
func (s *redactStage) hide(f reel.Frame) (*reel.CellFrame, *image.NRGBA) {
	cf := f.Cells
	if cf.Screen != s.src || !cf.State.Equal(s.srcState) {
		s.src, s.srcState = cf.Screen, cf.State
		s.hidden, s.covered = s.hideCells(cf)
	}
	if s.hidden == nil {
		return nil, nil
	}
	hidden := *s.hidden
	hidden.At = cf.At
	img := image.NewNRGBA(f.Image.Rect)
	s.rast.Draw(img, hidden.Screen, hidden.State, cf.At)
	if s.mode == redactBlur && s.covered != nil {
		s.blur(img, hidden.Screen, s.covered)
	}
	return &hidden, img
}

// hideCells returns cf with what the region covers hidden, and the cells
// it hid, or nil when it covers nothing. The screen is copied only when
// cells are hidden, under a new generation, so no renderer takes it for
// the one it drew.
func (s *redactStage) hideCells(cf *reel.CellFrame) (hidden *reel.CellFrame, covered []bool) {
	sec := secretsOf(s.region, s.matching)
	covered = coveredCells(cf, sec.rects(cf))
	st := cf.State
	st.Title = sec.scrub(st.Title)
	var scrubbed bool
	st.ProgramStatuses, scrubbed = sec.scrubStatuses(st.ProgramStatuses)
	if covered == nil && !scrubbed && st.Title == cf.State.Title {
		return nil, nil
	}
	scr := cf.Screen
	if covered != nil {
		scr = scr.Clone()
		s.edit(scr, covered)
		st.Generation = scr.Generation()
	}
	return &reel.CellFrame{Screen: scr, State: st, At: cf.At}, covered
}

// edit hides the cells of scr that covered holds, a grid of its size. A
// cell of a wide character is hidden as a cell of its own, in the style
// of the character.
func (s *redactStage) edit(scr *vt.Buffer, covered []bool) {
	b := scr.Bounds()
	w := b.Dx()
	mask := string(s.mask)
	for y := range b.Dy() {
		var style vt.Style
		for x := range w {
			c := scr.CellAt(b.Min.X+x, b.Min.Y+y)
			if c.Width > 0 {
				style = c.Style
			}
			if !covered[y*w+x] {
				continue
			}
			hidden := vt.Cell{Style: style, Width: 1}
			switch s.mode {
			case redactBlock:
				hidden.Content, hidden.Style = " ", vt.Style{Bg: style.Bg}
				if style.Attrs&vt.AttrReverse != 0 {
					hidden.Style.Fg, hidden.Style.Attrs = style.Fg, vt.AttrReverse
				}
			case redactBlur:
				hidden.Content = redactBlurred
			case redactReplace:
				hidden.Content = mask
			}
			scr.SetRaw(b.Min.X+x, b.Min.Y+y, hidden)
		}
	}
}

// blur blurs the pixels of the cells covered holds in img, an image of scr,
// each run of cells on its own, so no color bleeds in or out of them.
func (s *redactStage) blur(img *image.NRGBA, scr *vt.Buffer, covered []bool) {
	size := scr.Bounds().Size()
	if size != s.size {
		s.layout, s.size = s.rast.Layout(size.X, size.Y), size
	}
	radius := max(1, s.layout.Cell.Y/8)
	for _, c := range cellRuns(covered, size.X, size.Y) {
		r := s.layout.CellRect(c.Min.X, c.Min.Y).Union(s.layout.CellRect(c.Max.X-1, c.Max.Y-1))
		blurNRGBA(img, r.Add(img.Rect.Min).Intersect(img.Rect), radius)
	}
}

// blurNRGBA blurs the pixels of img in r, three passes of a box blur radius
// pixels each way, along each axis, which is close to a Gaussian blur. The
// box is clamped to r.
func blurNRGBA(img *image.NRGBA, r image.Rectangle, radius int) {
	if r.Empty() {
		return
	}
	line := make([][4]int, max(r.Dx(), r.Dy()))
	for range 3 {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			boxLine(img, line[:r.Dx()], img.PixOffset(r.Min.X, y), 4, radius)
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			boxLine(img, line[:r.Dy()], img.PixOffset(x, r.Min.Y), img.Stride, radius)
		}
	}
}

// boxLine box blurs the len(line) pixels of img from offset start, stride
// bytes apart, with a box radius pixels each way clamped to the line. line
// is scratch space.
func boxLine(img *image.NRGBA, line [][4]int, start, stride, radius int) {
	n := len(line)
	for i := range line {
		o := start + i*stride
		line[i] = [4]int{int(img.Pix[o]), int(img.Pix[o+1]), int(img.Pix[o+2]), int(img.Pix[o+3])}
	}
	for i := range n {
		lo, hi := max(0, i-radius), min(n-1, i+radius)
		var sum [4]int
		for k := lo; k <= hi; k++ {
			for c := range 4 {
				sum[c] += line[k][c]
			}
		}
		count := hi - lo + 1
		o := start + i*stride
		for c := range 4 {
			img.Pix[o+c] = uint8((sum[c] + count/2) / count) //nolint:gosec // An average of bytes fits in one.
		}
	}
}
