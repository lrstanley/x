// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"image"
	"regexp"
	"slices"
	"testing"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
)

// TestTypedMask checks the presses of typed text, as RedactInputs hides
// them from Keys, across input events.
func TestTypedMask(t *testing.T) {
	t.Parallel()
	abcdef := Text("abcdef")
	for _, tc := range []struct {
		name   string
		region Region
		// min is the MinPartial, zero for the default.
		min int
		// inputs are typed one event each, and those of before are typed
		// before the window.
		before []string
		inputs []string
		want   []string
	}{
		{"whole", abcdef, 0, nil, []string{"abcdef\r"}, []string{"••••••⏎"}},
		{"inside", abcdef, 0, nil, []string{"xabcdefx"}, []string{"x••••••x"}},
		{"too short", abcdef, 0, nil, []string{"a", "b"}, []string{"a", "b"}},
		{"a key at a time", abcdef, 0, nil, []string{"a", "b", "c", "d"}, []string{"•", "•", "•", "•"}},
		{"prefix cut off", abcdef, 0, nil, []string{"abc-x abcx"}, []string{"•••-x abcx"}},
		{"suffix", abcdef, 0, nil, []string{"x def xdef"}, []string{"x ••• xdef"}},
		{"backspace", abcdef, 0, nil, []string{"a", "b", "x", "\x7f", "c"}, []string{"•", "•", "x", "⌫", "•"}},
		// cde is neither the start nor the end of abcdef.
		{"enter ends", abcdef, 0, nil, []string{"ab\r", "cde"}, []string{"ab⏎", "cde"}},
		{"key ends", abcdef, 0, nil, []string{"ab\x1b[Acde"}, []string{"ab", "↑", "cde"}},
		{"paste ends", abcdef, 0, nil, []string{"ab\x1b[200~x\x1b[201~cde"}, []string{"ab", "cde"}},
		{"before the window", abcdef, 0, []string{"abc"}, []string{"de"}, []string{"••"}},
		{"regexp", Regexp(regexp.MustCompile(`\d{3}`)), 0, nil, []string{"pin 1234\r"}, []string{"pin •••4⏎"}},
		{"any", Any(Text("one"), Text("two")), 0, nil, []string{"one two\t"}, []string{"••• •••⇥"}},
		{"min 1", abcdef, 1, nil, []string{"a", "b", "x a. xf f\r"}, []string{"•", "•", "x •. xf •⏎"}},
		{"min 5", abcdef, 5, nil, []string{"a", "b", "c", "d", "e", " abcd bcdef"}, []string{"•", "•", "•", "•", "•", " abcd •••••"}},
		{"min longer", abcdef, 6, nil, []string{"abcde", " bcdef", " abcdef"}, []string{"abcde", " bcdef", " ••••••"}},
	} {
		m := defaultMatching
		if tc.min > 0 {
			MinPartial(tc.min)(&m)
		}
		mask := &typedMask{redactions: []typedRedaction{{tc.region, m}}}
		var presses []keyPress
		for _, in := range tc.before {
			presses = addPresses(presses, reel.Input{Data: []byte(in)}, mask)
		}
		shown := len(presses)
		for _, in := range tc.inputs {
			presses = addPresses(presses, reel.Input{Data: []byte(in)}, mask)
		}
		var got []string
		for _, p := range presses[shown:] {
			got = append(got, p.shownLabel())
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: labels %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestRedactInputsKeys checks that Keys after RedactInputs draws a typed
// secret as it draws •s typed instead.
func TestRedactInputsKeys(t *testing.T) {
	t.Parallel()
	timeline := func(typed string) *reel.Timeline {
		return buildTimeline(t, 40, 8, "", output(0, "x"), input(100, typed), mark(1000, "end"))
	}
	// Cells the size of the font's, so the chips fit on the frames.
	rast := testutil.Rasterizer{Cell: image.Pt(9, 20)}
	redacted := exportFrames(t, timeline("abcd\r"), reel.Profile{
		Rasterizer: rast,
		Effects:    []reel.Effect{RedactInputs(Text("abcd")), Keys(300 * ms)},
	})
	bullets := exportFrames(t, timeline("••••\r"), reel.Profile{
		Rasterizer: rast,
		Effects:    []reel.Effect{Keys(300 * ms)},
	})
	plain := exportFrames(t, timeline("abcd\r"), reel.Profile{
		Rasterizer: rast,
		Effects:    []reel.Effect{Keys(300 * ms)},
	})
	if len(redacted) != len(bullets) || len(redacted) < 2 {
		t.Fatalf("%d and %d frames", len(redacted), len(bullets))
	}
	for i := range redacted {
		if !bytes.Equal(redacted[i].Image.Pix, bullets[i].Image.Pix) {
			t.Fatalf("frame %d differs from the one with •••• typed", i)
		}
	}
	if slices.EqualFunc(redacted, plain, func(a, b reel.Frame) bool { return bytes.Equal(a.Image.Pix, b.Image.Pix) }) {
		t.Fatal("the frames look like those that show the secret")
	}
}
