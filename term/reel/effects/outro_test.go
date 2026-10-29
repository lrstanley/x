// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// TestOutro checks that an outro is a card after the last frame, the size of
// that frame, playing for its duration whatever the speed, with lines from
// the grid for OutroFunc, and no card without lines or a duration.
func TestOutro(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "", output(0, "a"), output(100, "b"), mark(500, "end"))
	var grid [2]int
	for _, tc := range []struct {
		name   string
		effect reel.Effect
		card   bool
	}{
		{"lines", Outro(700*ms, Line{Text: "bye"}), true},
		{"func", OutroFunc(700*ms, func(cols, rows int) []Line {
			grid = [2]int{cols, rows}
			return []Line{{Text: "bye"}}
		}), true},
		{"no lines", Outro(700 * ms), false},
		{"func without lines", OutroFunc(700*ms, func(int, int) []Line { return nil }), false},
		{"no duration", Outro(0, Line{Text: "bye"}), false},
	} {
		var in []reel.Frame
		out := exportFrames(t, tl, reel.Profile{
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
			t.Fatalf("%s: %d frames, want %d terminal frames and a card", tc.name, len(out), len(in))
		}
		card := out[len(out)-1]
		if card.Cells != nil || !card.Isolated || card.Delay != 700*ms || card.Image.Rect != in[len(in)-1].Image.Rect {
			t.Errorf("%s: card %+v, want an isolated card of 700ms the size of the last frame", tc.name, card)
		}
	}
	if grid[0] <= 0 || grid[1] <= 0 {
		t.Errorf("OutroFunc got grid %v", grid)
	}
}

// TestHoldLast checks that the terminal's last frame plays for at least the
// hold, before a card, and that a longer frame or a card is left alone.
func TestHoldLast(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "", output(0, "a"), output(100, "b"), mark(500, "end"))
	last := func(fx ...reel.Effect) (screen, final reel.Frame) {
		out := exportFrames(t, tl, reel.Profile{Effects: fx})
		for _, f := range out {
			if f.Cells != nil {
				screen = f
			}
		}
		return screen, out[len(out)-1]
	}
	plain, _ := last()
	held, _ := last(HoldLast(3 * time.Second))
	if held.Delay != 3*time.Second || plain.Delay >= 3*time.Second {
		t.Fatalf("held last frame plays %v (unheld %v), want 3s", held.Delay, plain.Delay)
	}
	if short, _ := last(HoldLast(ms)); short.Delay != plain.Delay {
		t.Fatalf("a shorter hold changed the last frame to %v, want %v", short.Delay, plain.Delay)
	}
	screen, card := last(HoldLast(3*time.Second), Outro(700*ms, Line{Text: "bye"}))
	if screen.Delay != 3*time.Second || card.Cells != nil || card.Delay != 700*ms {
		t.Fatalf("screen %v then card %v, want the screen held before a 700ms card", screen.Delay, card.Delay)
	}
	if _, card = last(Outro(700*ms, Line{Text: "bye"}), HoldLast(3*time.Second)); card.Delay != 700*ms {
		t.Fatalf("a hold after the outro changed the card to %v", card.Delay)
	}
}
