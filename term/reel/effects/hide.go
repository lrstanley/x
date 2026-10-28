// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// cutsKey is the [reel.Context] value [Hide] stores the []*cuts of every
// Hide started so far under, for [Keys].
type cutsKey struct{}

// HideRule is a rule of [Hide]: when it hides the recording.
type HideRule func(*hide)

// HideBetween hides each span of the timeline from a mark labeled
// hideLabel to the next mark labeled showLabel after it, so a recording
// can cut out setup, as a VHS tape does with Hide and Show. A hide mark
// inside a span, and a show mark outside one, change nothing. An empty
// hideLabel starts the recording hidden, until the first show mark, so
// nothing of the setup shows, not even a first frame. An empty showLabel
// never ends a span, so the first hide mark hides the rest of the
// recording. Both empty is an error from Start.
//
// A [reel.Record] looks for the marks as they are appended, and an
// [reel.Export] when the effect starts. A mark the recording never reaches
// is treated the same way by both: without a hide mark nothing is hidden,
// and without a show mark after the first span starts, the span runs to the
// end. The mark is reported as a [reel.NoMarkError], from
// [reel.Recording.Stop] or Export, which still write the outputs, as
// [Between] reports it.
func HideBetween(hideLabel, showLabel string) HideRule {
	return func(h *hide) { h.marks = append(h.marks, markRule{hide: hideLabel, show: showLabel}) }
}

// HideWhile hides each terminal frame on which region covers a cell, such
// as a [Text] of a prompt to enter a password. A nil region is an error
// from Start.
func HideWhile(region Region) HideRule {
	return func(h *hide) { h.screens = append(h.screens, screenRule{region: region, while: true}) }
}

// HideUnless hides each terminal frame on which region covers no cell,
// such as until a [Text] of the program's first prompt shows. A nil region
// is an error from Start.
func HideUnless(region Region) HideRule {
	return func(h *hide) { h.screens = append(h.screens, screenRule{region: region}) }
}

// Hide returns an effect that cuts the parts of the recording its rules
// hide out of playback, so the recording is shorter by them: [HideBetween]
// hides the spans between marks, and [HideWhile] and [HideUnless] the
// frames that show, or do not show, a [Region]. A frame is hidden while any
// rule hides it. A frame a span starts or ends in is cut on the delay grid:
// the screen before the span plays until the span starts, and the screen
// at its end plays from there, as the terminal showed it when the span
// ended. Frames without a terminal, such as cards, are left alone. Without
// rules, Hide is an error from Start.
//
// List Hide before [Keys] and [Pause], so they leave out the keys pressed
// and the marks in what Hide cuts. Effects listed after Hide see the frames
// left, and time jumps over each cut. The screen rules match the cells of
// the frames as the effects before Hide leave them, so after [Redact],
// which comes first, they match what it hides as hidden. A recording that
// Hide cuts entirely is an error from the export.
//
// Hide is a [reel.Cutter]: asciicast outputs hold the timeline as
// recorded, cuts and all, so a profile with Hide and an asciicast output
// is an error wrapping [reel.ErrRawOutput]. Stills of [reel.File.Marks]
// show their marks whether Hide cuts them or not.
func Hide(rules ...HideRule) reel.Effect {
	var h hide
	for _, rule := range rules {
		if rule != nil {
			rule(&h)
		}
	}
	return h
}

type hide struct {
	marks   []markRule
	screens []screenRule
}

// markRule is a [HideBetween] rule.
type markRule struct{ hide, show string }

// screenRule is a [HideWhile] rule, or a [HideUnless] rule without while.
type screenRule struct {
	region Region
	while  bool
}

// hides reports whether the rule hides the terminal of f.
func (r screenRule) hides(f *reel.CellFrame) bool {
	b := f.Screen.Bounds()
	grid := image.Rect(0, 0, b.Dx(), b.Dy())
	covers := slices.ContainsFunc(r.region.Rects(f), func(rect image.Rectangle) bool {
		return rect.Canon().Overlaps(grid)
	})
	return covers == r.while
}

// Cuts implements [reel.Cutter].
func (hide) Cuts() bool { return true }

func (e hide) Start(ctx *reel.Context) (reel.Stage, error) {
	if len(e.marks) == 0 && len(e.screens) == 0 {
		return nil, errors.New("effects: hide without rules")
	}
	for _, r := range e.marks {
		if r.hide == "" && r.show == "" {
			return nil, errors.New("effects: hide between the start and end hides everything")
		}
	}
	for _, r := range e.screens {
		if r.region == nil {
			return nil, errors.New("effects: hide without a region")
		}
	}
	s := &hideStage{ctx: ctx, screens: e.screens, cuts: &cuts{}}
	for _, r := range e.marks {
		sp := &spans{rule: r}
		if r.hide == "" {
			sp.list, sp.open = []span{{from: openFrom, to: openTo}}, true
		}
		s.spans = append(s.spans, sp)
	}
	s.readMarks()
	// A mark missing as the recording closes was never reached.
	ctx.OnClose(func() error {
		s.readMarks()
		return s.missing()
	})
	prev, _ := ctx.Value(cutsKey{}).([]*cuts)
	ctx.SetValue(cutsKey{}, append(slices.Clip(prev), s.cuts))
	return s.run, nil
}

// span is a stretch of the timeline, from up to but not including to.
type span struct{ from, to time.Duration }

// spans are the spans of a [HideBetween] rule, in timeline order, read from
// the marks of the timeline so far. The last runs to openTo while open.
type spans struct {
	rule markRule
	list []span
	open bool
	// hid and showed are set once a hide mark, and a show mark that ends a
	// span, are read.
	hid, showed bool
}

// add reads mark m.
func (sp *spans) add(m reel.Mark) {
	switch {
	case !sp.open && sp.rule.hide != "" && m.Label == sp.rule.hide:
		sp.list = append(sp.list, span{from: m.At(), to: openTo})
		sp.open, sp.hid = true, true
	case sp.open && sp.rule.show != "" && m.Label == sp.rule.show:
		sp.list[len(sp.list)-1].to = m.At()
		sp.open, sp.showed = false, true
	}
}

// missing returns a [reel.NoMarkError] for a label of the rule that names
// no mark it read, or nil.
func (sp *spans) missing() error {
	r := sp.rule
	switch {
	case r.hide != "" && !sp.hid:
		return fmt.Errorf("effects: hide between %q and %q: %w", r.hide, r.show, &reel.NoMarkError{Label: r.hide})
	case r.show != "" && !sp.showed:
		return fmt.Errorf("effects: hide between %q and %q: %w after the first span starts", r.hide, r.show, &reel.NoMarkError{Label: r.show})
	}
	return nil
}

// cuts is the timeline time a [Hide] cut from a recording so far, for
// [Keys] to leave the keys pressed in it out. Each cut runs from where Hide
// stopped playing the timeline to where it played it again, and the last
// to openTo while Hide plays nothing.
type cuts struct {
	list    []span
	cutting bool
}

// contains reports whether offset at is in a cut.
func (c *cuts) contains(at time.Duration) bool {
	return slices.ContainsFunc(c.list, func(sp span) bool { return sp.from <= at && at < sp.to })
}

// cut starts a cut at offset at, unless one runs already.
func (c *cuts) cut(at time.Duration) {
	if !c.cutting {
		c.cutting = true
		c.list = append(c.list, span{from: at, to: openTo})
	}
}

// play ends the cut that runs at offset at, leaving it out when it is
// empty.
func (c *cuts) play(at time.Duration) {
	if !c.cutting {
		return
	}
	c.cutting = false
	if last := &c.list[len(c.list)-1]; at > last.from {
		last.to = at
	} else {
		c.list = c.list[:len(c.list)-1]
	}
}

// hidden reports whether any Hide among those of cs cut offset at, for the
// effects after them.
func hidden(cs []*cuts, at time.Duration) bool {
	return slices.ContainsFunc(cs, func(c *cuts) bool { return c.contains(at) })
}

// hideStage is the cuts of one recording.
type hideStage struct {
	ctx     *reel.Context
	screens []screenRule
	spans   []*spans
	// read is the number of events of the timeline read for marks, which
	// grows while a [reel.Record] runs.
	read int
	cuts *cuts
	// screen is the screen the screen rules looked at last, and
	// screenHides whether they hid it, which the frames of one screen
	// share.
	screen      *vt.Buffer
	screenHides bool
}

// hidesScreen reports whether a screen rule hides the terminal of f.
func (s *hideStage) hidesScreen(f *reel.CellFrame) bool {
	if f.Screen != s.screen {
		s.screen = f.Screen
		s.screenHides = slices.ContainsFunc(s.screens, func(r screenRule) bool { return r.hides(f) })
	}
	return s.screenHides
}

// readMarks reads the marks appended to the timeline since it last read.
func (s *hideStage) readMarks() {
	for ev := range s.ctx.Timeline.EventsFrom(s.read) {
		s.read++
		if m, ok := ev.(reel.Mark); ok {
			for _, sp := range s.spans {
				sp.add(m)
			}
		}
	}
}

// missing returns a [reel.NoMarkError] for each label of a rule that names
// no mark read, or nil.
func (s *hideStage) missing() error {
	var errs []error
	for _, sp := range s.spans {
		errs = append(errs, sp.missing())
	}
	return errors.Join(errs...)
}

// within returns the spans of every rule that overlap the timeline from
// offset from up to to, in order, those that overlap one another merged.
func (s *hideStage) within(from, to time.Duration) []span {
	var out []span
	for _, sp := range s.spans {
		for _, x := range sp.list {
			if x.from < to && from < x.to {
				out = append(out, x)
			}
		}
	}
	slices.SortFunc(out, func(a, b span) int { return cmp.Compare(a.from, b.from) })
	merged := out[:0]
	for _, x := range out {
		if n := len(merged); n > 0 && x.from <= merged[n-1].to {
			merged[n-1].to = max(merged[n-1].to, x.to)
			continue
		}
		merged = append(merged, x)
	}
	return merged
}

func (s *hideStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	if s.ctx.Live {
		s.readMarks()
	}
	if s.hidesScreen(f.Cells) {
		s.cuts.cut(f.At)
		return
	}
	if f.Paused {
		// A paused frame shows one moment, so it plays or is cut whole.
		if len(s.within(f.At, f.At+1)) > 0 {
			s.cuts.cut(f.At)
			return
		}
		s.cuts.play(f.At)
		emit(f)
		return
	}

	end := f.At + time.Duration(float64(f.Delay)*s.ctx.Speed)
	off := func(at time.Duration) time.Duration { return delayOffset(s.ctx, f, at) }
	// play emits the part of f from offset at into the timeline, from
	// offset start up to stop into its delay.
	play := func(at, start, stop time.Duration) {
		if stop <= start {
			return
		}
		s.cuts.play(at)
		piece := f
		piece.At, piece.Delay = at, stop-start
		emit(piece)
	}
	at, played := f.At, time.Duration(0)
	for _, sp := range s.within(f.At, end) {
		play(at, played, off(sp.from))
		s.cuts.cut(max(sp.from, f.At))
		at, played = sp.to, max(played, off(sp.to))
	}
	if at < end {
		play(at, played, f.Delay)
	}
}
