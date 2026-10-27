// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// TestIntro checks that an intro is a card of its own GIF palette before
// the first frame, the size of the first frame, playing for its duration
// whatever the speed, and that without lines it needs a title.
func TestIntro(t *testing.T) {
	t.Parallel()
	timeline := func(title string) *reel.Timeline {
		return buildTimeline(t, 20, 4, title, output(0, "a"), output(100, "b"), mark(500, "end"))
	}
	for _, tc := range []struct {
		name   string
		effect reel.Effect
		tl     *reel.Timeline
		card   bool
	}{
		{"lines", Intro(700*ms, Line{Text: "hello"}), timeline(""), true},
		{"title", Intro(700 * ms), timeline("title"), true},
		{"no duration", Intro(0, Line{Text: "hello"}), timeline("title"), false},
		{"no title", Intro(700 * ms), timeline(""), false},
	} {
		var in []reel.Frame
		out := exportFrames(t, tc.tl, reel.Profile{
			Effects: []reel.Effect{recordFrames{&in}, tc.effect},
			Speed:   2,
		})
		if !tc.card {
			if len(out) != len(in) {
				t.Errorf("%s: %d frames, want the %d terminal frames", tc.name, len(out), len(in))
			}
			continue
		}
		if len(out) != len(in)+1 {
			t.Fatalf("%s: %d frames, want a card and %d terminal frames", tc.name, len(out), len(in))
		}
		card := out[0]
		if card.Cells != nil || !card.Isolated || card.Delay != 700*ms || card.Image.Rect != in[0].Image.Rect {
			t.Errorf("%s: card %+v, want an isolated card of 700ms the size of the first frame", tc.name, card)
		}
		for i, f := range out[1:] {
			if f.Image != in[i].Image || f.Delay != in[i].Delay {
				t.Errorf("%s: frame %d changed", tc.name, i)
			}
		}
	}
}

// TestIntroTitle checks that an intro without lines shows the title in
// bold.
func TestIntroTitle(t *testing.T) {
	t.Parallel()
	ctx := cardContext(t)
	ctx.Title = "my title"
	stage, err := Intro(time.Second).Start(ctx)
	if err != nil || stage == nil {
		t.Fatalf("Start = %v, %v", stage, err)
	}
	var out []reel.Frame
	stage(reel.Frame{Image: image.NewNRGBA(ctx.Layout.Image), Cells: cellsOf(80, 24, ""), First: true}, func(f reel.Frame) {
		out = append(out, f)
	})
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := cards.Card(cellsOf(80, 24, ""), Line{Text: "my title", Style: vt.Style{Attrs: vt.AttrBold}})
	if len(out) != 2 || !bytes.Equal(out[0].Image.Pix, want.Pix) {
		t.Fatalf("intro emitted %d frames, want the bold title card and the frame", len(out))
	}
}
