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

// RedactOption configures [Redact]: [Block], [Blur], or [Replace], and
// [Between] and [MinPartial]. [Ease], [EaseIn], and [EaseOut] are an error
// from Start.
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
// blurred.
const redactBlurred = "x"

// redactMode is how [Redact] hides a cell.
type redactMode uint8

const (
	redactBlock redactMode = iota
	redactBlur
	redactReplace
)

// Block hides each cell [Redact] covers as a blank cell in the cell's
// background color (its text color, for reverse video), so the cells look
// like the blank part of their row. It is the default.
func Block() RedactOption {
	return redactOption(func(r *redact) { r.mode = redactBlock })
}

// Blur hides each cell [Redact] covers as blurred text: the cell is drawn as
// with [Replace]('x'), then blurred. What the cells held is never drawn, so
// no amount of sharpening brings it back. Effects after Redact that draw
// cells of their own, such as [Zoom], draw the x's unblurred.
func Blur() RedactOption {
	return redactOption(func(r *redact) { r.mode = redactBlur })
}

// Replace hides each cell [Redact] covers as the character c, in the cell's
// own colors and attributes, so "hunter2" shows as "•••••••" with
// Replace('•'). A wide character shows c twice. c must be a printable
// character one cell wide, or Start returns an error.
func Replace(c rune) RedactOption {
	return redactOption(func(r *redact) { r.mode, r.mask = redactReplace, c })
}

// Redact returns an effect that hides the cells of region, such as a
// secret, from every frame it passes on, with [Block], the default, [Blur],
// or [Replace]. It redraws each frame from the edited cells and passes them
// on in [reel.Frame.Cells], so the effects after it never see what it hid.
//
// [Text] and [Env] regions also hide a secret that only partly shows, such
// as while it is typed (see [MinPartial]). They and [Regexp] regions also
// hide their text in the terminal's title, program status (OSC 7501), and
// desktop notifications, as a • for each character, and in
// [reel.Context.Title], which effects after Redact, such as [Intro], read.
// [Cells], [Row], and [Rows] regions hide cells only.
//
// Redact is a [reel.Concealer]. What the effects after it draw themselves,
// such as a card of a caller's own, is not redacted. A still of
// [reel.File.Marks] is redacted as a frame of the recording that starts at
// its mark would be.
//
// Redact can only hide what it finds in the text as the program wrote it.
// It does not match across lines a program lays out itself, by moving the
// cursor (see [Text]), a middle part of a secret cut off on both sides, or
// a secret spelled with characters that draw as nothing, such as zero-width
// spaces. The first characters of a secret typed into a prompt show until
// [MinPartial] of them are typed. Hide such cells with [Cells] or [Row]
// where that matters.
//
// [Between] limits Redact to a window of the timeline; every step of the
// delay grid that shows any of the window is redacted whole. An export of a
// timeline without the from mark, such as of a test that failed before it,
// is redacted whole, since where the secret starts is not known; without the
// to mark, it is redacted to the end. Notifications are scrubbed on every
// frame, whatever the window, since a [Toasts] toast shows one after it
// arrives. A nil region is an error from Start.
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
	// terminal share, hidden is it as hidden, or nil when the region covers
	// nothing of it, and covered is the cells hidden, or nil.
	src      *vt.Buffer
	srcState vt.State
	hidden   *reel.CellFrame
	covered  []bool

	// notes are the notifications scrubbed last, and scrubbedNotes them
	// scrubbed, or nil when that hid nothing.
	notes, scrubbedNotes []vt.Notification
}

func (s *redactStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	f.Cells = s.scrubNotifications(f.Cells)
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

// scrubNotifications returns cf with its notifications scrubbed (see
// [secrets.scrub]). It ignores the window, since a toast ([Toasts]) shows
// a notification after it arrives. The rasterizer does not draw
// notifications, so the frame's image stands.
func (s *redactStage) scrubNotifications(cf *reel.CellFrame) *reel.CellFrame {
	ns := cf.State.Notifications
	if len(ns) == 0 {
		return cf
	}
	// A terminal only appends to its notifications, so it holds the ones
	// scrubbed last while it holds as many from the same slice.
	if len(ns) != len(s.notes) || &ns[0] != &s.notes[0] {
		s.notes, s.scrubbedNotes = ns, nil
		if out, changed := secretsOf(s.region, s.matching).scrubNotifications(ns); changed {
			s.scrubbedNotes = out
		}
	}
	if s.scrubbedNotes == nil {
		return cf
	}
	scrubbed := *cf
	scrubbed.State.Notifications = s.scrubbedNotes
	return &scrubbed
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
// it hid, or nil when it covers nothing. An edited screen gets a new
// generation, so no renderer takes it for the one it drew.
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

// edit hides the cells of scr that covered holds, a grid of its size. Each
// cell of a wide character is hidden on its own, in the character's style.
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

// blurNRGBA blurs the pixels of img in r with three box blur passes along
// each axis, which is close to a Gaussian blur. The box is clamped to r.
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
