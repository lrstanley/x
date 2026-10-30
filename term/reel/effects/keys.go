// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"fmt"
	"image"
	"image/draw"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	// keysMaxChips is how many keys the overlay shows at once, newest last.
	// Repeats of one key share a chip.
	keysMaxChips = 6
	// keysMaxText is the longest typed text a chip shows before cutting it.
	keysMaxText = 16
)

// keysChip is the style of a key in the overlay: bold white on dark grey,
// xterm 255 and 238.
var keysChip = vt.Style{Fg: vt.Indexed(255), Bg: vt.Indexed(238), Attrs: vt.AttrBold}

// keyGlyphs are the labels for keys by their [vt.Key] code. Other keys are
// shown by name.
var keyGlyphs = map[string]string{
	"ARROW_UP":    "↑",
	"ARROW_DOWN":  "↓",
	"ARROW_LEFT":  "←",
	"ARROW_RIGHT": "→",
	"ENTER":       "⏎",
	"TAB":         "⇥",
	"BACKSPACE":   "⌫",
	"DELETE":      "⌦",
	"SPACE":       "␣",
}

// ctrlJ is a typed "\n", which decodes as ctrl+j.
var ctrlJ = vt.Key{Code: "J", Mods: vt.ModCtrl}

// Keys returns an effect that overlays the keys pressed in the timeline
// ([reel.Input] events) on the terminal frames, in the bottom right corner.
// Keys pressed within shown of each other are shown together, repeats as
// one key with a count, and stay for shown after the last of them, fading
// out over its final third. Paste and mouse input are not shown. A shown of
// zero or less is no overlay.
//
// The text typed by one input event, with enter and tab, is one chip, so
// typing "hi\n" shows "hi⏎". Only the input in the timeline's window is
// shown.
//
// The overlay adds a few frames per burst of keys. A [RedactInputs] listed
// before Keys hides typed text from it, and a [Hide] listed before it the
// keys pressed in what it cuts.
func Keys(shown time.Duration) reel.Effect {
	return keys{shown: shown}
}

type keys struct{ shown time.Duration }

func (e keys) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.shown <= 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	from, _ := ctx.Timeline.Window()
	k := &keysStage{ctx: ctx, fade: fade{shown: e.shown, step: ctx.Step}, cards: cards, from: from}
	if rs, ok := ctx.Value(typedRedactionKey{}).([]typedRedaction); ok {
		k.mask = &typedMask{redactions: rs}
	}
	k.cuts, _ = ctx.Value(cutsKey{}).([]*cuts)
	return k.run, nil
}

// keyPress is a key pressed, as the overlay shows it.
type keyPress struct {
	at time.Duration // Offset into the timeline.
	// label is the label of a key, and typed is nil for one. A run of typed
	// text has its characters in typed instead, with the glyphs of enter
	// and tab, and a • for each that [RedactInputs] hides.
	label string
	typed []rune
}

// shownLabel is the label the overlay shows for p.
func (p keyPress) shownLabel() string {
	if p.typed != nil {
		return typedLabel(string(p.typed))
	}
	return p.label
}

// keysStage is the overlay for one recording.
type keysStage struct {
	ctx   *reel.Context
	fade  fade
	cards *CardRenderer
	// from is the start of the timeline's window: input before it is not
	// shown.
	from time.Duration

	presses []keyPress
	// read is the number of events of the timeline read into presses.
	read int
	// mask hides typed text, for [RedactInputs], or is nil.
	mask *typedMask
	// cuts are those of each [Hide] before Keys, whose keys it leaves out.
	cuts []*cuts
	// start is the first press a frame can show.
	start int

	// chips is the last chip row drawn and what it was drawn for.
	chips    *image.NRGBA
	chipsFor chipsKey
}

// chipsKey is what a chip row depends on.
type chipsKey struct {
	labels     string
	cols, rows int
	focused    bool
}

func (k *keysStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	k.readPresses()
	k.skipGone(f.At)
	k.frames(k.timeline(f), f, emit)
}

// readPresses adds the presses of the inputs appended to the timeline since
// it last read, and moves start past those before the window, which are
// read only so the mask sees text that goes on into the window.
func (k *keysStage) readPresses() {
	for ev := range k.ctx.Timeline.EventsFrom(k.read) {
		k.read++
		if in, ok := ev.(reel.Input); ok {
			k.presses = addPresses(k.presses, in, k.mask)
		}
	}
	for k.start < len(k.presses) && k.presses[k.start].at < k.from {
		k.start++
	}
}

// addPresses appends the presses of in to presses: one per key, and one per
// run of typed text (see [Keys]). mask, which may be nil, hides what it
// matches of the typed text.
func addPresses(presses []keyPress, in reel.Input, mask *typedMask) []keyPress {
	// run is the index of the press of the typed text being added to, or
	// -1.
	run := -1
	typed := func(r rune) {
		if run < 0 {
			run = len(presses)
			presses = append(presses, keyPress{at: in.At()})
		}
		presses[run].typed = append(presses[run].typed, r)
	}
	for _, s := range vt.DecodeInput(in.Data) {
		switch s.Kind {
		case vt.StrokeText:
			for _, r := range s.Text {
				typed(r)
				mask.add(r, run, len(presses[run].typed)-1)
			}
		case vt.StrokeKey:
			k := s.Key
			switch {
			case k == vt.Key{Code: "ENTER"} || k == vt.Key{Code: "TAB"} || (k == ctrlJ && run >= 0):
				mask.end(presses)
				glyph := keyGlyphs[k.Code]
				if k == ctrlJ {
					glyph = keyGlyphs["ENTER"]
				}
				for _, r := range glyph {
					typed(r)
				}
			case k.IsModifier():
				// Modifiers show with the keys they modify.
			default:
				if k == (vt.Key{Code: "BACKSPACE"}) {
					mask.backspace()
				} else {
					mask.end(presses)
				}
				run = -1
				presses = append(presses, keyPress{at: in.At(), label: keyLabel(k)})
			}
		case vt.StrokePaste, vt.StrokeMouse:
			mask.end(presses)
			run = -1
		}
	}
	mask.hide(presses)
	return presses
}

// keyLabel is the label of a key: its name, with the key's glyph in place
// of the key for a key that has one.
func keyLabel(k vt.Key) string {
	name := k.String()
	if glyph, ok := keyGlyphs[k.Code]; ok {
		// A key with a glyph is named by its code alone, after its
		// modifiers.
		return strings.TrimSuffix(name, vt.Key{Code: k.Code}.String()) + glyph
	}
	return name
}

// typedLabel is the label of typed text, cut to keysMaxText. Text of only
// spaces shows a ␣ for each, rather than an empty chip.
func typedLabel(text string) string {
	if strings.TrimFunc(text, unicode.IsSpace) == "" {
		text = strings.Repeat(keyGlyphs["SPACE"], utf8.RuneCountInString(text))
	}
	return ansi.Truncate(text, keysMaxText, "…")
}

// keyTimeline places the keys pressed before a frame ends on that frame's
// playback clock, as offsets from its start snapped to [reel.Context.Step],
// so the overlay changes on the delay grid.
type keyTimeline struct {
	at     []time.Duration
	labels []string
	// fade is how keys fade after the last press of a burst.
	fade fade
}

func (k *keysStage) timeline(f reel.Frame) keyTimeline {
	t := keyTimeline{fade: k.fade}
	for _, p := range k.presses[k.start:] {
		at := playOffset(k.ctx, f, p.at)
		if at >= f.Delay {
			break
		}
		if hidden(k.cuts, p.at) {
			continue
		}
		t.at = append(t.at, at)
		t.labels = append(t.labels, p.shownLabel())
	}
	return t
}

// skipGone moves start past the bursts no frame from offset at on shows.
// A burst is only gone once the gap to the next key is more than shown and
// a step, so the next burst can not join it on the delay grid.
func (k *keysStage) skipGone(at time.Duration) {
	gone := time.Duration(float64(k.fade.shown+k.ctx.Step) * k.ctx.Speed)
	for i := k.start + 1; i < len(k.presses) && k.presses[i-1].at+gone <= at; i++ {
		if k.presses[i].at-k.presses[i-1].at >= gone {
			k.start = i
		}
	}
}

// keyState is what the overlay shows at one moment: keys first through last,
// at a fade level (see [fade.level]).
type keyState struct {
	first, last, level int
}

// noKeys is the state with nothing shown.
var noKeys = keyState{last: -1}

// state is the overlay at playback offset p. A key pressed before the one
// before it disappears joins its burst, and a burst stays until its last
// key disappears.
func (t keyTimeline) state(p time.Duration) keyState {
	last := -1
	for i, at := range t.at {
		if at <= p {
			last = i
		}
	}
	if last < 0 {
		return noKeys
	}
	level := t.fade.level(t.at[last], p)
	if level < 0 {
		return noKeys
	}
	first := last
	for first > 0 && t.at[first] < t.fade.start(t.at[first-1], fadeLevels+1) {
		first--
	}
	return keyState{first: first, last: last, level: level}
}

// frames splits f wherever t changes and emits each piece with the overlay
// drawn on it. A paused frame ([reel.Frame.Paused]) is not split: it keeps
// the overlay as of its start.
func (k *keysStage) frames(t keyTimeline, f reel.Frame, emit func(reel.Frame)) {
	splitFrame(k.ctx, f, t.fade.cuts(f.Delay, t.at...), t.state, func(st keyState) *image.NRGBA {
		if st == noKeys {
			return f.Image
		}
		return overlayKeys(f.Image, k.chipRow(f.Cells, t.labels[st.first:st.last+1]), st.level)
	}, emit)
}

// chipRow returns the chips for labels on the terminal of src, reusing the
// last row drawn when it is the same.
func (k *keysStage) chipRow(src *reel.CellFrame, labels []string) *image.NRGBA {
	b := src.Screen.Bounds()
	key := chipsKey{labels: strings.Join(labels, "\x00"), cols: b.Dx(), rows: b.Dy(), focused: src.State.Focused}
	if k.chips == nil || key != k.chipsFor {
		k.chips, k.chipsFor = keyChips(k.cards, src, labels), key
	}
	return k.chips
}

// chipGroups returns the chips of labels: repeats merged into one with a
// count, and only the newest keysMaxChips kept.
func chipGroups(labels []string) []string {
	var groups []string
	for i := 0; i < len(labels); {
		n := 1
		for i+n < len(labels) && labels[i+n] == labels[i] {
			n++
		}
		g := labels[i]
		if n > 1 {
			g += fmt.Sprintf(" ×%d", n)
		}
		groups = append(groups, g)
		i += n
	}
	return groups[max(0, len(groups)-keysMaxChips):]
}

// keyChips draws labels as a row of chips (see [chipGroups]) on a
// transparent background, or returns nil when there is nothing to draw.
func keyChips(cards *CardRenderer, src *reel.CellFrame, labels []string) *image.NRGBA {
	var chips []*image.NRGBA
	var w, h int
	for _, g := range chipGroups(labels) {
		chip := cards.Text(src, Line{Text: " " + g + " ", Style: keysChip})
		if chip == nil {
			continue
		}
		chips = append(chips, chip)
		w += chip.Bounds().Dx()
		h = max(h, chip.Bounds().Dy())
	}
	if len(chips) == 0 {
		return nil
	}
	gap := h / 3
	row := image.NewNRGBA(image.Rect(0, 0, w+gap*(len(chips)-1), h))
	x := 0
	for _, chip := range chips {
		b := chip.Bounds()
		draw.Draw(row, image.Rect(x, 0, x+b.Dx(), b.Dy()), chip, b.Min, draw.Src)
		x += b.Dx() + gap
	}
	return row
}

// overlayKeys draws chips on a copy of frame, in its bottom right corner a
// chip's height from the edges, at a fade level (see [keyState]).
func overlayKeys(frame, chips *image.NRGBA, level int) *image.NRGBA {
	if chips == nil {
		return frame
	}
	b, cb := frame.Bounds(), chips.Bounds()
	img := cloneNRGBA(frame)
	at := image.Pt(b.Max.X-cb.Dx()-cb.Dy(), b.Max.Y-2*cb.Dy())
	drawFaded(img, chips, at, fadeOpacity[level])
	return img
}
