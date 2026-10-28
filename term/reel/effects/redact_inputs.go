// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel"
)

// typedRedactionKey is the [reel.Context] value key of the
// []typedRedaction of every [RedactInputs] started so far, for [Keys].
type typedRedactionKey struct{}

// typedRedaction is the region of a [RedactInputs], with its matching.
type typedRedaction struct {
	region Region
	matching
}

// RedactInputs returns an effect that hides the text region matches from
// the typed text [Keys] shows, as a • for each character, so a password
// typed with "hunter2\n" shows as "•••••••⏎". It changes no frame and only
// affects the Keys listed after it; an effect of a caller's own that reads
// the timeline's input still sees what was typed. It is a
// [reel.Concealer].
//
// region is a [Text], [Regexp], or [Env] region, or [Any] of them. Regions
// that find no text, such as [Cells], and a nil region, are errors from
// Start. It is matched against the text typed since the last key that is
// not text, such as an arrow key, across input events. Backspace takes back
// a character, and enter and tab end the text. A [Text] or [Env] region
// also hides a partly typed secret once [MinPartial] characters of it are
// typed. [reel.Export] reads every input before the
// first frame, so it hides those first characters too, where a live
// [reel.Record] shows them until enough are typed.
//
// The text is matched as [vt.DecodeInput] decodes it, so under a keyboard
// mode that reports keys without their text, such as some kitty keyboard
// protocol modes, a shifted symbol can decode as its key and no longer
// match.
func RedactInputs(region Region, opts ...Matching) reel.Effect {
	r := redactInputs{region: region, matching: defaultMatching}
	for _, opt := range opts {
		if opt != nil {
			opt(&r.matching)
		}
	}
	return r
}

type redactInputs typedRedaction

// Conceals implements [reel.Concealer].
func (redactInputs) Conceals() bool { return true }

func (e redactInputs) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.region == nil {
		return nil, errors.New("effects: redact inputs without a region")
	}
	if err := e.check(); err != nil {
		return nil, err
	}
	if others := secretsOf(e.region, e.matching).others; len(others) > 0 {
		return nil, fmt.Errorf("effects: redact inputs: region %T finds no text", others[0])
	}
	prev, _ := ctx.Value(typedRedactionKey{}).([]typedRedaction)
	ctx.SetValue(typedRedactionKey{}, append(slices.Clip(prev), typedRedaction(e)))
	return nil, nil //nolint:nilnil // It changes no frame.
}

// typedMask hides the text the [RedactInputs] regions match in the presses
// [Keys] shows. It holds the text typed since the run of text was last
// broken, and where each of its characters is shown. A nil mask hides
// nothing.
type typedMask struct {
	redactions []typedRedaction
	text       []byte
	refs       []typedRef
}

// typedRef is where a character of a typedMask's text is: at offset off of
// the text, and shown as character idx of the press with index press.
type typedRef struct {
	off, press, idx int
}

func (m *typedMask) add(r rune, press, idx int) {
	if m == nil {
		return
	}
	m.refs = append(m.refs, typedRef{off: len(m.text), press: press, idx: idx})
	m.text = utf8.AppendRune(m.text, r)
}

// backspace takes back the last character of the text.
func (m *typedMask) backspace() {
	if m == nil || len(m.refs) == 0 {
		return
	}
	n := len(m.refs) - 1
	m.text, m.refs = m.text[:m.refs[n].off], m.refs[:n]
}

// hide masks the characters of the text the region matches in presses.
func (m *typedMask) hide(presses []keyPress) {
	if m == nil || len(m.refs) == 0 {
		return
	}
	var ranges [][2]int
	for _, r := range m.redactions {
		ranges = append(ranges, secretsOf(r.region, r.matching).ranges(string(m.text), -1)...)
	}
	if len(ranges) == 0 {
		return
	}
	hidden := make([]bool, len(m.text))
	for _, rg := range ranges {
		for i := rg[0]; i < rg[1]; i++ {
			hidden[i] = true
		}
	}
	for i, ref := range m.refs {
		end := len(m.text)
		if i+1 < len(m.refs) {
			end = m.refs[i+1].off
		}
		if slices.Contains(hidden[ref.off:end], true) {
			presses[ref.press].typed[ref.idx] = redactMask
		}
	}
}

// end hides what the text matches, and starts a new run of text.
func (m *typedMask) end(presses []keyPress) {
	if m == nil {
		return
	}
	m.hide(presses)
	m.text, m.refs = m.text[:0], m.refs[:0]
}
