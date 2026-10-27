// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	// keysFadeLevels is how many steps the key overlay fades out in. Each step
	// is a frame, and a fade defeats duplicate frame merging, so keep it low.
	keysFadeLevels = 3
	// keysMaxChips is how many keys the overlay shows at once, newest last.
	// Repeats of one key share a chip.
	keysMaxChips = 6
	// keysMaxText is the longest typed text a chip shows before cutting it.
	keysMaxText = 16
)

// keysOpacity is the overlay's opacity at each fade level, opaque first.
var keysOpacity = [keysFadeLevels + 1]uint8{255, 192, 128, 64}

// keysChip is the style of a key in the overlay: bold white on dark grey,
// xterm 255 and 238.
var keysChip = vt.Style{Fg: vt.Indexed(255), Bg: vt.Indexed(238), Attrs: vt.AttrBold}

// keyGlyphs are the labels for the named keys of [vt.Stroke.Key]. Other
// keys are shown by name.
var keyGlyphs = map[string]string{
	"up":        "↑",
	"down":      "↓",
	"left":      "←",
	"right":     "→",
	"enter":     "⏎",
	"tab":       "⇥",
	"backspace": "⌫",
	"delete":    "⌦",
	"space":     "␣",
}

// Keys returns an effect that overlays the keys pressed in the timeline
// ([reel.Input] events) on the terminal frames, in the bottom right corner.
// Keys pressed within shown of each other are shown together, repeats as
// one key with a count, and stay for shown after the last of them, fading
// out over its final third. Paste and mouse input are not shown. A shown of
// zero or less is no overlay.
//
// The keys of one input event that type text, with enter and tab among them,
// are one chip, so typing "hi\n" shows "hi⏎". Only the input in the
// timeline's window is shown.
//
// The overlay splits frames on the delay grid wherever it changes, which
// adds a few frames per burst of keys. Frames without a terminal, such as
// cards, are left alone. A [RedactInputs] listed before Keys hides typed
// text from it.
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
	k := &keysStage{ctx: ctx, shown: e.shown, cards: cards, from: from}
	if rs, ok := ctx.Value(typedRedactionKey{}).([]typedRedaction); ok {
		k.mask = &typedMask{redactions: rs}
	}
	return k.run, nil
}

// keyPress is a key pressed, as the overlay shows it.
type keyPress struct {
	// at is the offset into the timeline.
	at time.Duration
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
	shown time.Duration
	cards *CardRenderer
	// from is the start of the timeline's window: input before it is not
	// shown.
	from time.Duration

	presses []keyPress
	// read is the number of events of the timeline read into presses, which
	// grows while a [reel.Record] runs.
	read int
	// mask hides typed text, for [RedactInputs], or is nil.
	mask *typedMask
	// start is the first press a frame can show: the ones before it ended
	// a burst that is gone.
	start int

	// chips is the last chip row drawn and what it was drawn for, reused
	// while a burst spans several frames.
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
// read only for the mask, so it can hide text typed on in the window.
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

// addPresses appends the presses of the strokes of in to presses, as the
// overlay shows them: one per key, and one per run of typed text (see
// [Keys]). Paste and mouse are left out. mask, which may be nil, is given
// the typed text, and hides what it matches.
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
			switch {
			case s.Key == "enter" || s.Key == "tab" || (s.Key == "ctrl+j" && run >= 0):
				mask.end(presses)
				glyph := keyGlyphs[s.Key]
				if s.Key == "ctrl+j" {
					// A typed "\n", which decodes as ctrl+j.
					glyph = keyGlyphs["enter"]
				}
				for _, r := range glyph {
					typed(r)
				}
			case modifierKey(s.Key):
			default:
				if s.Key == "backspace" {
					mask.backspace()
				} else {
					mask.end(presses)
				}
				run = -1
				presses = append(presses, keyPress{at: in.At(), label: keyLabel(s.Key)})
			}
		case vt.StrokePaste, vt.StrokeMouse:
			mask.end(presses)
			run = -1
		}
	}
	mask.hide(presses)
	return presses
}

// modifierKeys are the keys whose press only holds a modifier, which the
// kitty keyboard protocol can report, and the overlay leaves out: the
// modifiers show with the keys they modify.
var modifierKeys = map[string]bool{
	"leftshift": true, "rightshift": true,
	"leftctrl": true, "rightctrl": true,
	"leftalt": true, "rightalt": true,
	"leftsuper": true, "rightsuper": true,
	"lefthyper": true, "righthyper": true,
	"leftmeta": true, "rightmeta": true,
	"isolevel3shift": true, "isolevel5shift": true,
}

// modifierKey reports whether name, as in [vt.Stroke.Key], is the press
// of a modifier on its own.
func modifierKey(name string) bool {
	return modifierKeys[name[strings.LastIndexByte(name, '+')+1:]]
}

// keyLabel is the label of a key named as in [vt.Stroke.Key]: its
// modifiers, then its glyph or name.
func keyLabel(name string) string {
	mods, key := "", name
	if i := strings.LastIndexByte(key, '+'); i > 0 && i < len(key)-1 {
		mods, key = key[:i+1], key[i+1:]
	}
	if glyph := keyGlyphs[key]; glyph != "" {
		key = glyph
	}
	return mods + key
}

// typedLabel is the label of typed text: the text, or ␣ for each character
// when it is only spaces, which would show an empty chip, cut to
// keysMaxText.
func typedLabel(text string) string {
	if strings.TrimFunc(text, unicode.IsSpace) == "" {
		text = strings.Repeat(keyGlyphs["space"], utf8.RuneCountInString(text))
	}
	return ansi.Truncate(text, keysMaxText, "…")
}

// keyTimeline places the keys pressed before a frame ends on that frame's
// playback clock, as offsets from its start snapped to [reel.Context.Step],
// so the overlay changes on the delay grid.
type keyTimeline struct {
	at     []time.Duration
	labels []string
	// shown is how long keys stay after the last press of a burst. They stay
	// opaque for the first two thirds, then fade.
	shown time.Duration
	step  time.Duration
}

func (k *keysStage) timeline(f reel.Frame) keyTimeline {
	t := keyTimeline{shown: k.shown, step: k.ctx.Step}
	for _, p := range k.presses[k.start:] {
		at := time.Duration(float64(p.at-f.At) / k.ctx.Speed).Round(t.step)
		if at >= f.Delay {
			break
		}
		t.at = append(t.at, at)
		t.labels = append(t.labels, p.shownLabel())
	}
	return t
}

// skipGone moves start past the bursts gone by offset at, which no frame
// from there on shows: those whose last key disappeared a step before at,
// with a gap of more than shown and a step to the next key, so the next
// burst's keys can not join them on the delay grid either.
func (k *keysStage) skipGone(at time.Duration) {
	gone := time.Duration(float64(k.shown+k.ctx.Step) * k.ctx.Speed)
	for i := k.start + 1; i < len(k.presses) && k.presses[i-1].at+gone <= at; i++ {
		if k.presses[i].at-k.presses[i-1].at >= gone {
			k.start = i
		}
	}
}

// fade is when the key pressed at at starts fade level (see [keyState]), on
// the step grid. Level keysFadeLevels+1 is when it disappears.
func (t keyTimeline) fade(at time.Duration, level int) time.Duration {
	hold := t.shown * 2 / 3
	return (at + hold + (t.shown-hold)*time.Duration(level-1)/keysFadeLevels).Round(t.step)
}

// keyState is what the overlay shows at one moment: keys first through last,
// at a fade level, where 0 is opaque and keysFadeLevels the faintest.
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
	if last < 0 || p >= t.fade(t.at[last], keysFadeLevels+1) {
		return noKeys
	}
	first := last
	for first > 0 && t.at[first] < t.fade(t.at[first-1], keysFadeLevels+1) {
		first--
	}
	level := 0
	for level < keysFadeLevels && p >= t.fade(t.at[last], level+1) {
		level++
	}
	return keyState{first: first, last: last, level: level}
}

// frames splits f wherever t changes and emits each piece with the overlay
// drawn on it.
func (k *keysStage) frames(t keyTimeline, f reel.Frame, emit func(reel.Frame)) {
	cuts := []time.Duration{0}
	for _, at := range t.at {
		cuts = append(cuts, at)
		for level := 1; level <= keysFadeLevels+1; level++ {
			cuts = append(cuts, t.fade(at, level))
		}
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	// Keep cuts inside the frame that leave its last piece at least one step.
	cuts = slices.DeleteFunc(cuts, func(c time.Duration) bool {
		return c < 0 || (c > 0 && f.Delay-c < t.step)
	})

	var out []reel.Frame
	var prev keyState
	for i, start := range cuts {
		end := f.Delay
		if i+1 < len(cuts) {
			end = cuts[i+1]
		}
		st := t.state(start)
		if len(out) > 0 && st == prev {
			out[len(out)-1].Delay += end - start
			continue
		}
		prev = st
		piece := f
		piece.At = f.At + time.Duration(float64(start)*k.ctx.Speed)
		piece.Delay = end - start
		if st != noKeys {
			piece.Image = overlayKeys(f.Image, k.chipRow(f.Cells, t.labels[st.first:st.last+1]), st.level)
		}
		out = append(out, piece)
	}
	for _, piece := range out {
		emit(piece)
	}
}

// chipRow returns the chips for labels on the terminal of src, drawing them
// only when they differ from the last row drawn.
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
// transparent background. It returns nil when there is nothing to draw.
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
	mask := image.NewUniform(color.Alpha{A: keysOpacity[level]})
	draw.DrawMask(img, cb.Sub(cb.Min).Add(at), chips, cb.Min, mask, image.Point{}, draw.Over)
	return img
}
