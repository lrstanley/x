// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt/screen"
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
var keysChip = screen.Style{Fg: screen.Indexed(255), Bg: screen.Indexed(238), Attrs: screen.AttrBold}

// keyGlyphs are the labels for named keys (see [Harness.Key]). Other keys are
// shown by name.
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
	"escape":    "esc",
	"pageup":    "pgup",
	"pagedown":  "pgdown",
}

// keyPress is a key the test sent, as the overlay shows it.
type keyPress struct {
	at    time.Time
	label string
}

// keyLabel is the overlay label for an input hook's metadata, or "" for
// input the overlay does not show (paste and mouse).
func keyLabel(metadata any) string {
	meta, ok := metadata.(InputMeta)
	if !ok {
		return ""
	}
	switch meta.Kind {
	case "key":
		mods, key := "", meta.Key
		if i := strings.LastIndexByte(key, '+'); i > 0 && i < len(key)-1 {
			mods, key = key[:i+1], key[i+1:]
		}
		if glyph := keyGlyphs[key]; glyph != "" {
			key = glyph
		}
		return mods + key
	case "type":
		return ansi.Truncate(typedGlyphs.Replace(meta.Text), keysMaxText, "…")
	}
	return ""
}

// typedGlyphs labels the control characters in typed text like their keys.
var typedGlyphs = strings.NewReplacer("\n", keyGlyphs["enter"], "\t", keyGlyphs["tab"])

// keysOverlay is the stage behind [WithExportKeys]. A recording owns one: it
// collects the keys the test presses and draws them on the frames they were
// pressed during.
type keysOverlay struct {
	// shown is how long keys stay after the last press of a burst.
	shown time.Duration
	// speed scales real time into playback time, like the recording's.
	speed   float64
	presses []keyPress
	// chips is the last chip row drawn and the labels and scale it shows,
	// reused while a burst spans several frames.
	chips      image.Image
	chipsLabel string
	chipsScale float64
}

// press records a key press from an input hook's metadata.
func (k *keysOverlay) press(metadata any, at time.Time) {
	if label := keyLabel(metadata); label != "" {
		k.presses = append(k.presses, keyPress{at: at, label: label})
	}
}

// mutate is the overlay's [ExportMutator]. Inserted frames without an At,
// such as cards, pass through.
func (k *keysOverlay) mutate(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
	if f.At.IsZero() {
		emit(f)
		return
	}
	k.timeline(f, ec.Step).frames(ec, k, f, emit)
}

// playback converts real time since a frame's paint into playback time.
func (k *keysOverlay) playback(d time.Duration) time.Duration {
	return time.Duration(float64(d) / k.speed)
}

// keyTimeline places the keys pressed before a frame ends on that frame's
// playback clock, as offsets from its start snapped to [ExportContext.Step],
// so the overlay changes on the frame rate grid.
type keyTimeline struct {
	at     []time.Duration
	labels []string
	// shown is how long keys stay after the last press of a burst. They stay
	// opaque for the first two thirds, then fade.
	shown time.Duration
	step  time.Duration
}

func (k *keysOverlay) timeline(f ExportFrame, step time.Duration) keyTimeline {
	t := keyTimeline{shown: k.shown, step: step}
	for _, p := range k.presses {
		at := k.playback(p.at.Sub(f.At)).Round(step)
		if at >= f.Delay {
			break
		}
		t.at = append(t.at, at)
		t.labels = append(t.labels, p.label)
	}
	return t
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

// frames splits f wherever the overlay changes and emits each piece with the
// overlay drawn on it.
func (t keyTimeline) frames(ec ExportContext, k *keysOverlay, f ExportFrame, emit func(ExportFrame)) {
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

	var out []ExportFrame
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
		piece.At = f.At.Add(time.Duration(float64(start) * k.speed))
		piece.Delay = end - start
		if st != noKeys {
			piece.Image = overlayKeys(f.Image, k.chipRow(ec, t.labels[st.first:st.last+1]), st.level)
		}
		out = append(out, piece)
	}
	for _, piece := range out {
		emit(piece)
	}
}

// chipRow returns the chips for labels, drawing them only when they differ
// from the last row drawn.
func (k *keysOverlay) chipRow(ec ExportContext, labels []string) image.Image {
	joined, scale := strings.Join(labels, "\x00"), ec.cardScale()
	if k.chips == nil || joined != k.chipsLabel || scale != k.chipsScale {
		k.chips, k.chipsLabel, k.chipsScale = ec.keyChips(labels), joined, scale
	}
	return k.chips
}

// keyChips renders labels as a row of chips, repeats merged and only the
// newest keysMaxChips kept, on a transparent background.
func (ec ExportContext) keyChips(labels []string) image.Image {
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
	groups = groups[max(0, len(groups)-keysMaxChips):]

	state := ec.cardState()
	var chips []image.Image
	var w, h int
	for _, g := range groups {
		chip := ec.cardText(state, []CardLine{{Text: " " + g + " ", Style: keysChip}})
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
func overlayKeys(frame, chips image.Image, level int) image.Image {
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
