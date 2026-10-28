// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"regexp"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel"
)

const (
	// badgeMinSize is the smallest badge, in pixels, the glyph fits in.
	badgeMinSize = 8
	// pauseEnd is how long [Pause] pauses at the end of a recording
	// without pause points.
	pauseEnd = time.Second
)

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

// PauseOption configures [Pause]: where it pauses, and the badge it
// draws.
type PauseOption func(*pause)

// PauseAtEnd pauses for d after the last terminal frame, so a looping
// recording rests on its final screen.
func PauseAtEnd(d time.Duration) PauseOption {
	return func(p *pause) { p.points = append(p.points, pausePoint{d: d, end: true}) }
}

// PauseAtMarks pauses for d at each mark labeled one of labels, or at every
// mark without labels. A label that names no mark in the timeline's window
// is an error wrapping [reel.ErrNoMark], from Start for an [reel.Export],
// and from [reel.Recording.Stop] for a [reel.Record], as [Between] reports
// it.
func PauseAtMarks(d time.Duration, labels ...string) PauseOption {
	labels = slices.Clone(labels)
	match := func(string) bool { return true }
	if len(labels) > 0 {
		match = func(label string) bool { return slices.Contains(labels, label) }
	}
	return func(p *pause) { p.points = append(p.points, pausePoint{d: d, labels: labels, match: match}) }
}

// PauseAtMatch pauses for d at each mark whose label re matches. A nil re
// is an error from Start.
func PauseAtMatch(d time.Duration, re *regexp.Regexp) PauseOption {
	var match func(string) bool
	if re != nil {
		match = re.MatchString
	}
	return func(p *pause) { p.points = append(p.points, pausePoint{d: d, match: match}) }
}

// WithBadgeCorner puts the badge in corner c of the terminal's cell grid,
// inset by a third of its size. The default, [TopRight], sits below any
// window chrome and clear of the [Keys] overlay, which is bottom right:
// with [BottomRight], keys shown when the pause starts overlap it.
func WithBadgeCorner(c Corner) PauseOption {
	return func(p *pause) { p.corner = c }
}

// WithBadgeSize sets the badge's diameter in pixels. Zero, the default, is
// one and a half times the height of a terminal cell
// ([reel.Context.Layout]), and at least 8. Any other size below 8, which
// the glyph does not fit, is an error from Start.
func WithBadgeSize(px int) PauseOption {
	return func(p *pause) { p.size = px }
}

// WithBadgeColor sets the color of the pause bars. The default, and a nil
// c, is the light grey of the [Keys] chip text. The disc behind the bars is
// a translucent dark grey for a light color, and a translucent light grey
// for a dark one.
func WithBadgeColor(c color.Color) PauseOption {
	return func(p *pause) {
		p.color = badgeColor
		if c != nil {
			p.color, _ = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
	}
}

// WithoutBadge pauses without drawing the badge.
func WithoutBadge() PauseOption {
	return func(p *pause) { p.noBadge = true }
}

// Pause returns an effect that pauses playback at the points its options
// give: [PauseAtEnd], [PauseAtMarks], and [PauseAtMatch], each for a
// duration of its own, unscaled by [reel.Profile.Speed]. Without any, it
// pauses for a second at the end. Each pause is a frozen copy of the
// terminal frame that shows its point, [reel.Frame.Paused], that plays
// for the duration, so the recording grows by it. A pause at a mark splits
// the frame that shows the mark there, on the delay grid. Pauses that fall
// on one step of a frame play as one, for the longest of them, and a pause
// of zero is none. A duration below zero is an error from Start.
//
// Each pause shows a pause glyph, two bars on a translucent disc, in a
// corner of the frame, so a viewer reads it as paused rather than frozen,
// and the end of a looping recording as finished and about to loop. The
// badge is drawn on copies of the images, so the frames around a pause are
// left alone, and duplicate frame merging still merges them.
//
// The end pause follows the last terminal frame, so a card an effect adds
// after it, before or after Pause in the list, follows the pause. Effects
// listed after Pause draw a paused frame as of its moment, so [Keys] keys
// stay as they were when the pause started; list [Highlight] and [Zoom]
// before Pause, so the badge is drawn over them.
//
// A recording that plays once (sink.WithLoopCount(sink.LoopOnce)) stays on
// its last frame when it ends, badge and all, where the glyph reads as
// paused rather than about to loop, so such a profile may use
// WithoutBadge. Effects change only the frames of recordings: a PNG output,
// which writes the last frame, gets the badge of an end pause, but the
// stills of [reel.File.Marks] get no effects at all.
func Pause(opts ...PauseOption) reel.Effect {
	p := pause{corner: TopRight, color: badgeColor}
	for _, opt := range opts {
		if opt != nil {
			opt(&p)
		}
	}
	if p.points == nil {
		p.points = []pausePoint{{d: pauseEnd, end: true}}
	}
	return p
}

type pause struct {
	points  []pausePoint
	corner  Corner
	size    int
	color   color.NRGBA
	noBadge bool
}

// pausePoint is where [Pause] pauses, and for how long: at the end, or at
// the marks whose labels match reports true for. labels are those it names,
// which must be in the timeline.
type pausePoint struct {
	d      time.Duration
	end    bool
	match  func(label string) bool
	labels []string
}

func (e pause) Start(ctx *reel.Context) (reel.Stage, error) {
	for _, pt := range e.points {
		switch {
		case pt.d < 0:
			return nil, fmt.Errorf("effects: pause of %v is below zero", pt.d)
		case !pt.end && pt.match == nil:
			return nil, errors.New("effects: pause at a nil regexp")
		}
	}
	s := &pauseStage{ctx: ctx}
	for _, pt := range e.points {
		if pt.end {
			s.end = max(s.end, pt.d)
		} else {
			s.points = append(s.points, pt)
		}
	}
	s.from, _ = ctx.Timeline.Window()
	if !e.noBadge {
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
		s.corner, s.size, s.glyph = e.corner, size, pauseGlyph(size, e.color)
	}
	s.readMarks()
	if ctx.Live {
		ctx.OnClose(func() error {
			s.readMarks()
			return s.missing()
		})
	} else if err := s.missing(); err != nil {
		return nil, err
	}
	return s.run, nil
}

// markPause is a pause at a mark, at offset at into the timeline.
type markPause struct {
	at time.Duration
	d  time.Duration
}

// pauseStage is the pauses of one recording.
type pauseStage struct {
	ctx *reel.Context
	// points are the pauses at marks, and end how long the end pause is.
	points []pausePoint
	end    time.Duration
	// from is the start of the timeline's window: marks before it are not
	// paused at.
	from time.Duration
	// glyph is the badge, size pixels in corner, or nil for none.
	corner Corner
	size   int
	glyph  *image.RGBA

	// marks are the pauses at marks not yet played, oldest first, read
	// from the first read events of the timeline, which grows while a
	// [reel.Record] runs. seen holds the labels of the marks read.
	marks []markPause
	read  int
	seen  map[string]bool
	// held is the newest terminal frame, and the frames without a terminal
	// after it, held back until the next terminal frame says it is not the
	// last.
	held []reel.Frame
	// src is the image badged last, and badged its badged copy, which the
	// pauses of one frame share.
	src, badged *image.NRGBA
}

// readMarks adds the pauses at the marks appended to the timeline since it
// last read.
func (s *pauseStage) readMarks() {
	for ev := range s.ctx.Timeline.EventsFrom(s.read) {
		s.read++
		m, ok := ev.(reel.Mark)
		if !ok || m.At() < s.from {
			continue
		}
		if s.seen == nil {
			s.seen = make(map[string]bool)
		}
		s.seen[m.Label] = true
		var d time.Duration
		for _, pt := range s.points {
			if pt.match(m.Label) {
				d = max(d, pt.d)
			}
		}
		if d > 0 {
			s.marks = append(s.marks, markPause{at: m.At(), d: d})
		}
	}
}

// missing returns an error wrapping [reel.ErrNoMark] for each label a
// point names that no mark read has, or nil.
func (s *pauseStage) missing() error {
	var errs []error
	reported := map[string]bool{}
	for _, pt := range s.points {
		for _, label := range pt.labels {
			if !s.seen[label] && !reported[label] {
				reported[label] = true
				errs = append(errs, fmt.Errorf("effects: pause at marks: %w: %q", reel.ErrNoMark, label))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *pauseStage) run(f reel.Frame, emit func(reel.Frame)) {
	switch {
	case f.Cells != nil:
		s.release(f.At, false, emit)
		s.held = append(s.held, f)
	case len(s.held) > 0:
		s.held = append(s.held, f)
	default:
		emit(f)
	}
	if f.Last {
		s.release(openTo, true, emit)
	}
}

// stop is a pause at offset off into a frame's delay, on the delay grid.
type stop struct{ off, d time.Duration }

// release emits the held frames, with the pauses at the marks before offset
// until, where the next terminal frame starts, and when the held one is the
// last, the end pause.
func (s *pauseStage) release(until time.Duration, last bool, emit func(reel.Frame)) {
	if len(s.held) == 0 {
		return
	}
	f, rest := s.held[0], s.held[1:]
	s.held = nil
	if s.ctx.Live {
		s.readMarks()
	}

	// Marks are in timeline order, so the stops come out in order too. A
	// paused frame shows one moment: marks up to it pause before it, and
	// those past it after.
	var stops []stop
	add := func(off, d time.Duration) {
		if n := len(stops); n > 0 && stops[n-1].off == off {
			stops[n-1].d = max(stops[n-1].d, d)
			return
		}
		stops = append(stops, stop{off, d})
	}
	n := 0
	for _, m := range s.marks {
		if m.at >= until {
			break
		}
		var off time.Duration
		switch {
		case !f.Paused:
			off = time.Duration(float64(m.at-f.At) / s.ctx.Speed).Round(s.ctx.Step)
			off = min(max(off, 0), f.Delay)
		case m.at > f.At:
			off = f.Delay
		}
		add(off, m.d)
		n++
	}
	s.marks = s.marks[n:]
	if last && s.end > 0 {
		add(f.Delay, s.end)
	}
	if len(stops) == 0 {
		emit(f)
		for _, r := range rest {
			emit(r)
		}
		return
	}

	// at is the offset into the timeline of the moment off into f.
	at := func(off time.Duration) time.Duration {
		if f.Paused {
			return f.At
		}
		return f.At + time.Duration(float64(off)*s.ctx.Speed)
	}
	var played time.Duration
	playTo := func(off time.Duration) {
		if off > played {
			piece := f
			piece.At, piece.Delay = at(played), off-played
			emit(piece)
			played = off
		}
	}
	for _, st := range stops {
		playTo(st.off)
		paused := f
		paused.At, paused.Paused = at(st.off), true
		paused.Delay = max(st.d.Round(s.ctx.Step), s.ctx.Step)
		paused.Image = s.badge(f)
		emit(paused)
	}
	playTo(f.Delay)
	for _, r := range rest {
		emit(r)
	}
}

// badge returns f's image with the badge drawn on a copy, or the image
// itself without a badge, or for a frame paused already, which may have
// one.
func (s *pauseStage) badge(f reel.Frame) *image.NRGBA {
	if s.glyph == nil || f.Paused {
		return f.Image
	}
	if f.Image != s.src {
		b := f.Cells.Screen.Bounds()
		grid := s.ctx.Rasterizer.Layout(b.Dx(), b.Dy()).Grid
		s.src, s.badged = f.Image, cloneNRGBA(f.Image)
		draw.Draw(s.badged, s.glyph.Rect.Add(badgeAt(grid, s.size, s.corner)), s.glyph, image.Point{}, draw.Over)
	}
	return s.badged
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
