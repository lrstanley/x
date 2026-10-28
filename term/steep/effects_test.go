// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"bytes"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
)

// frameLog is an effect that keeps every frame it is given, and passes it
// on.
type frameLog struct{ frames *[]reel.Frame }

func (e frameLog) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		*e.frames = append(*e.frames, f)
		emit(f)
	}, nil
}

// recordEffects runs a harness of cols by rows with effects, failing it when
// fail is set, and returns the frames past the effects.
func recordEffects(t *testing.T, cols, rows int, fail bool, fx ...reel.Effect) []reel.Frame {
	t.Helper()
	var frames []reel.Frame
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, exportOptions(t.TempDir(),
		WithWindowSize(cols, rows),
		WithRecording(reel.Profile{
			Outputs: reel.MustFiles("rec.gif"),
			Effects: append(fx, frameLog{&frames}),
		}),
		WithExportAlways(),
	)...)
	h.Wait("text=")
	if fail {
		h.Assert("missing")
	}
	h.Close()
	return frames
}

// cards returns the frames an effect inserted.
func cards(frames []reel.Frame) []reel.Frame {
	var out []reel.Frame
	for _, f := range frames {
		if f.Cells == nil {
			out = append(out, f)
		}
	}
	return out
}

func TestLogo(t *testing.T) {
	t.Parallel()
	t.Run("animated", func(t *testing.T) {
		t.Parallel()
		frames := recordEffects(t, 80, 24, false, Logo(time.Second))
		got := cards(frames)
		// An empty frame, a frame per pixel row filled, then a steam step
		// every 150ms.
		want := 1 + len(logoBitmap) + int(time.Second/logoSteamStep)
		if len(got) != want {
			t.Fatalf("logo frames = %d, want %d", len(got), want)
		}
		for i, f := range got {
			if !f.Isolated || f.Image == nil {
				t.Fatalf("logo frame %d: isolated %v, image %v", i, f.Isolated, f.Image != nil)
			}
		}
		if frames[len(got)].Cells == nil {
			t.Fatal("the logo is not before the first terminal frame")
		}
		if bytes.Equal(got[0].Image.Pix, got[len(got)-1].Image.Pix) {
			t.Fatal("the logo does not animate")
		}
	})
	t.Run("small terminal", func(t *testing.T) {
		t.Parallel()
		got := cards(recordEffects(t, 20, 4, false, Logo(time.Second)))
		if len(got) != 1 || got[0].Delay != time.Second {
			t.Fatalf("got %d cards, want one text card for 1s", len(got))
		}
	})
	t.Run("off", func(t *testing.T) {
		t.Parallel()
		if st, err := Logo(0).Start(&reel.Context{}); st != nil || err != nil {
			t.Fatalf("Logo(0) started a stage: %v", err)
		}
	})
}

func TestOutro(t *testing.T) {
	t.Parallel()
	outroCard := func(t *testing.T, fail bool, fx ...reel.Effect) reel.Frame {
		t.Helper()
		frames := recordEffects(t, 40, 8, fail, fx...)
		last := frames[len(frames)-1]
		if last.Cells != nil || !last.Isolated || last.Delay != 500*time.Millisecond {
			t.Fatalf("last frame is not an outro card: cells %v isolated %v delay %v", last.Cells != nil, last.Isolated, last.Delay)
		}
		if len(cards(frames)) != 1 {
			t.Fatalf("%d cards, want one", len(cards(frames)))
		}
		return last
	}
	pass := outroCard(t, false, Outro(500*time.Millisecond))
	fail := outroCard(t, true, Outro(500*time.Millisecond))
	lines := outroCard(t, true, Outro(500*time.Millisecond, effects.Line{Text: "custom"}))
	if bytes.Equal(pass.Image.Pix, fail.Image.Pix) || bytes.Equal(fail.Image.Pix, lines.Image.Pix) {
		t.Fatal("PASS, FAIL, and custom outro cards look the same")
	}
	// With Pause before it, the card follows the end pause, which a
	// failure makes 3s.
	frames := recordEffects(t, 40, 8, true, effects.Pause(), Outro(500*time.Millisecond))
	if n := len(frames); n < 2 || frames[n-1].Cells != nil || !frames[n-2].Paused || frames[n-2].Delay != failureHold {
		t.Fatal("the outro card does not follow a 3s end pause")
	}
	// Listed before Pause, the card still follows the pause.
	frames = recordEffects(t, 40, 8, true, Outro(500*time.Millisecond), effects.Pause())
	if n := len(frames); n < 2 || frames[n-1].Cells != nil || !frames[n-2].Paused {
		t.Fatal("with Outro first, the card does not follow the end pause")
	}
	// Without Pause, a failure holds the last terminal frame for 3s.
	frames = recordEffects(t, 40, 8, true, Outro(500*time.Millisecond))
	if n := len(frames); n < 2 || frames[n-2].Cells == nil || frames[n-2].Delay != failureHold {
		t.Fatal("without Pause, the last terminal frame is not held for 3s")
	}
	// A pass holds nothing.
	frames = recordEffects(t, 40, 8, false, Outro(500*time.Millisecond))
	if n := len(frames); n < 2 || frames[n-2].Delay >= failureHold {
		t.Fatal("a pass held its last terminal frame")
	}

	// Without a Result and without lines, there is no card.
	if st, err := Outro(time.Second).Start(&reel.Context{Meta: "not a result"}); st != nil || err != nil {
		t.Fatalf("Outro started a stage without a Result: %v", err)
	}
}

func TestResultLines(t *testing.T) {
	t.Parallel()
	texts := func(r *Result) []string {
		var out []string
		for _, l := range resultLines(r, 40, 24) {
			out = append(out, l.Text)
		}
		return out
	}
	if got := texts(&Result{Name: "TestX"}); len(got) != 2 || got[0] != "PASS" || got[1] != "TestX" {
		t.Fatalf("pass = %q", got)
	}
	got := texts(&Result{Name: "TestX", Failed: true, Failures: []string{"want Tokyo"}})
	if len(got) != 4 || got[0] != "FAIL" || got[3] != "• want Tokyo" {
		t.Fatalf("fail = %q", got)
	}
}
