// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// smooth is the ease-in-out curve the window eases on.
func smooth(x float64) float64 { return x * x * (3 - 2*x) }

func TestWindowPieces(t *testing.T) {
	t.Parallel()
	const step = 10 * ms
	for _, tc := range []struct {
		name   string
		w      window
		at     time.Duration
		delay  time.Duration
		paused bool
		want   []piece
	}{
		{
			name: "no window", w: window{from: openFrom, to: openTo},
			at: 50 * ms, delay: 200 * ms,
			want: []piece{{0, 200 * ms, 1}},
		},
		{
			// Steps are on whenever their middle is in the window, from
			// 100ms to 200ms of the timeline.
			name: "window", w: window{from: 100 * ms, to: 200 * ms},
			at: 50 * ms, delay: 200 * ms,
			want: []piece{{0, 50 * ms, 0}, {50 * ms, 100 * ms, 1}, {150 * ms, 50 * ms, 0}},
		},
		{
			name: "before the window", w: window{from: 500 * ms, to: openTo},
			at: 50 * ms, delay: 200 * ms,
			want: []piece{{0, 200 * ms, 0}},
		},
		{
			// A step's middle on the edge is in the window.
			name: "edge on a middle", w: window{from: 75 * ms, to: 95 * ms},
			at: 50 * ms, delay: 60 * ms,
			want: []piece{{0, 20 * ms, 0}, {20 * ms, 30 * ms, 1}, {50 * ms, 10 * ms, 0}},
		},
		{
			// Easing in over 30ms before 100ms is a step each at 75ms,
			// 85ms, and 95ms, 25ms, 15ms, and 5ms out, and out over 30ms
			// after 150ms is 155ms, 165ms, and 175ms.
			name: "ease", w: window{from: 100 * ms, to: 150 * ms, easeIn: 30 * ms, easeOut: 30 * ms},
			at: 50 * ms, delay: 200 * ms,
			want: []piece{
				{0, 20 * ms, 0},
				{20 * ms, 10 * ms, smooth(1 - 25.0/30)},
				{30 * ms, 10 * ms, smooth(1 - 15.0/30)},
				{40 * ms, 10 * ms, smooth(1 - 5.0/30)},
				{50 * ms, 50 * ms, 1},
				{100 * ms, 10 * ms, smooth(1 - 5.0/30)},
				{110 * ms, 10 * ms, smooth(1 - 15.0/30)},
				{120 * ms, 10 * ms, smooth(1 - 25.0/30)},
				{130 * ms, 70 * ms, 0},
			},
		},
		{
			// Easing in over 20ms before 100ms is a step each at 85ms and
			// 95ms, and out over 40ms after 150ms is 155ms to 185ms.
			name: "ease in and out apart", w: window{from: 100 * ms, to: 150 * ms, easeIn: 20 * ms, easeOut: 40 * ms},
			at: 50 * ms, delay: 200 * ms,
			want: []piece{
				{0, 30 * ms, 0},
				{30 * ms, 10 * ms, smooth(1 - 15.0/20)},
				{40 * ms, 10 * ms, smooth(1 - 5.0/20)},
				{50 * ms, 50 * ms, 1},
				{100 * ms, 10 * ms, smooth(1 - 5.0/40)},
				{110 * ms, 10 * ms, smooth(1 - 15.0/40)},
				{120 * ms, 10 * ms, smooth(1 - 25.0/40)},
				{130 * ms, 10 * ms, smooth(1 - 35.0/40)},
				{140 * ms, 60 * ms, 0},
			},
		},
		{
			// At twice the speed, a step is 20ms of the timeline, and a
			// 20ms ease is 40ms of it: middles at 110ms and 130ms.
			name: "speed", w: window{from: openFrom, to: 100 * ms, easeOut: 40 * ms, speed: 2},
			at: 0, delay: 100 * ms,
			want: []piece{
				{0, 50 * ms, 1},
				{50 * ms, 10 * ms, smooth(1 - 10.0/40)},
				{60 * ms, 10 * ms, smooth(1 - 30.0/40)},
				{70 * ms, 30 * ms, 0},
			},
		},
		{
			// The last piece keeps what is past the last step.
			name: "remainder", w: window{from: openFrom, to: 75 * ms},
			at: 50 * ms, delay: 1005 * ms,
			want: []piece{{0, 30 * ms, 1}, {30 * ms, 975 * ms, 0}},
		},
		{
			// A paused frame shows one moment, 110ms, the end of the
			// window, so it is covered, and the easing does not play in it.
			name: "paused", w: window{from: openFrom, to: 110 * ms, easeOut: 20 * ms},
			at: 110 * ms, delay: 360 * ms, paused: true,
			want: []piece{{0, 360 * ms, 1}},
		},
		{
			// A frame shorter than a step is one piece.
			name: "short", w: window{from: 50 * ms, to: openTo},
			at: 50 * ms, delay: 5 * ms,
			want: []piece{{0, 5 * ms, 1}},
		},
	} {
		w := tc.w
		w.step = step
		if w.speed == 0 {
			w.speed = 1
		}
		got := w.pieces(reel.Frame{At: tc.at, Delay: tc.delay, Paused: tc.paused})
		if !slices.EqualFunc(got, tc.want, func(a, b piece) bool {
			return a.start == b.start && a.delay == b.delay && math.Abs(a.level-b.level) < 1e-9
		}) {
			t.Errorf("%s: pieces\n got %v\nwant %v", tc.name, got, tc.want)
		}
		var total time.Duration
		for _, p := range got {
			total += p.delay
		}
		if total != tc.delay {
			t.Errorf("%s: pieces play %v, want %v", tc.name, total, tc.delay)
		}
	}
}

// TestWindowCover checks that a window that must be covered covers every
// step that shows any of it, where steps take the strength at their middle
// otherwise.
func TestWindowCover(t *testing.T) {
	t.Parallel()
	const step = 100 * ms
	for _, tc := range []struct {
		name     string
		from, to time.Duration
		want     []piece
	}{
		// The middle of the first step, 355ms, is past the end of the
		// window, at 329ms, though the frame is drawn in it.
		{"end", 293 * ms, 329 * ms, []piece{{0, 100 * ms, 1}, {100 * ms, 200 * ms, 0}}},
		{"instant", 305 * ms, 305 * ms, []piece{{0, 100 * ms, 1}, {100 * ms, 200 * ms, 0}}},
		// A step that ends before the window starts stays off, and the
		// one it starts in is covered.
		{"start", 450 * ms, openTo, []piece{{0, 100 * ms, 0}, {100 * ms, 200 * ms, 1}}},
		{"after", openFrom, 300 * ms, []piece{{0, 300 * ms, 0}}},
	} {
		w := window{from: tc.from, to: tc.to, step: step, speed: 1, cover: true}
		got := w.pieces(reel.Frame{At: 305 * ms, Delay: 300 * ms})
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: pieces\n got %v\nwant %v", tc.name, got, tc.want)
		}
		w.cover = false
		if mid := w.pieces(reel.Frame{At: 305 * ms, Delay: 300 * ms}); tc.name == "end" && mid[0].level != 0 {
			t.Errorf("%s: a window that need not be covered is on at the first step", tc.name)
		}
	}
}

// TestWindowLongHold checks that a long frame splits only where the
// strength changes, not at every step.
func TestWindowLongHold(t *testing.T) {
	t.Parallel()
	w := window{from: time.Hour, to: 2 * time.Hour, easeIn: 50 * ms, easeOut: 50 * ms, step: 10 * ms, speed: 1}
	got := w.pieces(reel.Frame{At: 0, Delay: 3 * time.Hour})
	if len(got) != 1+5+1+5+1 {
		t.Fatalf("%d pieces, want 13: off, 5 easing in, on, 5 easing out, off", len(got))
	}
}

func TestTimingResolve(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "", output(0, "a"), mark(100, "on"), mark(200, "off"), mark(300, "on"), output(400, "b"))
	ctx := &reel.Context{Timeline: tl, Step: 10 * ms, Speed: 2}
	for _, tc := range []struct {
		opts            []Timing
		from, to        time.Duration
		easeIn, easeOut time.Duration
		// fails is set for an error, which wraps err when it is set.
		fails bool
		err   error
	}{
		{nil, openFrom, openTo, 0, 0, false, nil},
		{[]Timing{Between("on", "off"), Ease(50 * ms)}, 100 * ms, 200 * ms, 100 * ms, 100 * ms, false, nil},
		{[]Timing{EaseIn(10 * ms), EaseOut(30 * ms)}, openFrom, openTo, 20 * ms, 60 * ms, false, nil},
		// A later option replaces either easing.
		{[]Timing{Ease(50 * ms), EaseOut(0)}, openFrom, openTo, 100 * ms, 0, false, nil},
		{[]Timing{EaseIn(10 * ms), Ease(50 * ms)}, openFrom, openTo, 100 * ms, 100 * ms, false, nil},
		{[]Timing{Between("on", "")}, 100 * ms, openTo, 0, 0, false, nil},
		{[]Timing{Between("", "off")}, openFrom, 200 * ms, 0, 0, false, nil},
		// The first mark labeled to after the first labeled from.
		{[]Timing{Between("off", "on")}, 200 * ms, 300 * ms, 0, 0, false, nil},
		// A mark the recording never reached: without its to mark, the
		// window runs to the end, and without its from mark, it never
		// opens.
		{[]Timing{Between("on", "nope")}, 100 * ms, openTo, 0, 0, false, nil},
		{[]Timing{Between("nope", "")}, openTo, openTo, 0, 0, false, nil},
		{[]Timing{Between("nope", "on")}, openTo, openTo, 0, 0, false, nil},
		{[]Timing{Ease(-ms)}, 0, 0, 0, 0, true, nil},
		{[]Timing{EaseIn(-ms)}, 0, 0, 0, 0, true, nil},
		{[]Timing{EaseOut(-ms)}, 0, 0, 0, 0, true, nil},
	} {
		var tm timing
		for _, o := range tc.opts {
			o(&tm)
		}
		w, err := tm.start(ctx, easeAfterMark)
		switch {
		case tc.fails:
			if err == nil || tc.err != nil && !errors.Is(err, tc.err) {
				t.Errorf("%+v: err = %v, want an error wrapping %v", tm, err, tc.err)
			}
			continue
		case err != nil:
			t.Errorf("%+v: %v", tm, err)
			continue
		}
		if w.from != tc.from || w.to != tc.to || w.easeIn != tc.easeIn || w.easeOut != tc.easeOut {
			t.Errorf("%+v: window %v to %v easing in %v and out %v, want %v to %v easing in %v and out %v",
				tm, w.from, w.to, w.easeIn, w.easeOut, tc.from, tc.to, tc.easeIn, tc.easeOut)
		}
	}
}

// TestTimingLive checks a window on a live timeline, whose marks are
// appended after it starts.
func TestTimingLive(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, openErr := reel.Open(t.Context(), 4, 1)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer s.Close()
		ctx := &reel.Context{Timeline: s.Timeline(), Live: true, Step: 10 * ms, Speed: 1}
		start := func(cov coverage, opts ...Timing) *window {
			t.Helper()
			var tm timing
			for _, o := range opts {
				o(&tm)
			}
			w, err := tm.start(ctx, cov)
			if err != nil {
				t.Fatal(err)
			}
			return w
		}
		levels := func(w *window, at ...time.Duration) []float64 {
			w.update()
			out := make([]float64, len(at))
			for i, a := range at {
				out[i] = w.level(a)
			}
			return out
		}
		mustMark := func(label string) {
			t.Helper()
			if err := s.Mark(label); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := s.Write([]byte("a")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * ms)
		mustMark("early")
		known := start(easeAfterMark, Between("early", "off"), Ease(20*ms))
		late := start(easeAfterMark, Between("on", "off"), Ease(20*ms))
		apart := start(easeAfterMark, Between("on", "off"), EaseIn(20*ms), EaseOut(40*ms))
		full := start(coverWindow, Between("on", "off"))
		never := start(easeAfterMark, Between("on", "never"))
		same := start(easeAfterMark, Between("m", "m"))
		ends := start(easeAfterMark, Between("", "off"))
		unseen := start(easeAfterMark, Between("on", "off"))

		// Before its mark, a late window is off, wherever the mark
		// comes.
		if got := levels(late, 0, 100*ms, time.Hour); slices.ContainsFunc(got, func(l float64) bool { return l != 0 }) {
			t.Fatalf("late window before its mark: levels %v, want 0", got)
		}
		// A known start eases in before it, and the end is open.
		if got, want := levels(known, 85*ms, 100*ms, time.Hour), []float64{smooth(0.25), 1, 1}; !slices.Equal(got, want) {
			t.Fatalf("known window: levels %v, want %v", got, want)
		}

		time.Sleep(200 * ms)
		mustMark("on")
		mustMark("m")
		time.Sleep(100 * ms)
		mustMark("off")
		mustMark("m")
		// Marks no frame reached are found when the recording closes.
		if err := unseen.missing(); err != nil || unseen.from != 300*ms || unseen.to != 400*ms {
			t.Fatalf("closing with no frame given: window %v to %v, err %v, want 300ms to 400ms", unseen.from, unseen.to, err)
		}
		// on at 300ms, off at 400ms. The late window eases in after on.
		if got, want := levels(late, 299*ms, 300*ms, 310*ms, 320*ms, 400*ms, 410*ms, 420*ms), []float64{0, 0, smooth(0.5), 1, 1, smooth(0.5), 0}; !slices.Equal(got, want) {
			t.Fatalf("late window: levels %v, want %v", got, want)
		}
		// Easing in and out apart, it eases in after on over 20ms, and out
		// after off over 40ms.
		if got, want := levels(apart, 300*ms, 310*ms, 320*ms, 400*ms, 410*ms, 420*ms, 440*ms), []float64{0, smooth(0.5), 1, 1, smooth(0.75), smooth(0.5), 0}; !slices.Equal(got, want) {
			t.Fatalf("late window easing apart: levels %v, want %v", got, want)
		}
		// One that must cover its window is at full strength on the mark.
		if got, want := levels(full, 299*ms, 300*ms, 400*ms, 401*ms), []float64{0, 1, 1, 0}; !slices.Equal(got, want) {
			t.Fatalf("full window: levels %v, want %v", got, want)
		}
		for _, w := range []*window{known, same, ends, never} {
			w.update()
		}
		if known.to != 400*ms || ends.from != openFrom || ends.to != 400*ms {
			t.Fatalf("ends: known to %v, open start %v to %v, want 400ms, open, 400ms", known.to, ends.from, ends.to)
		}
		if same.from != 300*ms || same.to != 400*ms {
			t.Fatalf("same label: %v to %v, want 300ms to 400ms", same.from, same.to)
		}
		for _, w := range []*window{known, late, apart, full, same, ends} {
			if err := w.missing(); err != nil {
				t.Errorf("missing() = %v", err)
			}
		}
		if err := never.missing(); !errors.Is(err, reel.ErrNoMark) || !strings.Contains(err.Error(), `"never"`) {
			t.Fatalf("missing() = %v, want an error wrapping reel.ErrNoMark for never", err)
		}
	})
}

// TestBetweenRecord checks a highlight between marks placed while a
// session is recorded: without easing, it writes what exporting the
// timeline afterwards does, a mark appended after the last frame is found,
// and a mark that never comes is an error from Stop, which still writes the
// recording.
func TestBetweenRecord(t *testing.T) {
	t.Parallel()
	s, err := reel.Open(t.Context(), 6, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	profile := func(name string, to string) reel.Profile {
		return reel.Profile{
			Outputs:    reel.MustFiles(filepath.Join(dir, name+".gif")),
			Rasterizer: highlightRast,
			FPS:        100,
			Effects:    []reel.Effect{Highlight(Row(0), Between("on", to))},
		}
	}
	rec, err := reel.Record(t.Context(), s, profile("record", "off"))
	if err != nil {
		t.Fatal(err)
	}
	never, err := reel.Record(t.Context(), s, profile("never", "never"))
	if err != nil {
		t.Fatal(err)
	}
	last, err := reel.Record(t.Context(), s, profile("last", "last"))
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range []string{"a", "on", "b", "c", "off", "d"} {
		if len(step) > 1 {
			err = s.Mark(step)
		} else {
			_, err = s.Write([]byte(step))
		}
		if err != nil {
			t.Fatal(i, err)
		}
		time.Sleep(30 * ms)
	}
	// Past where the last frame's end rounds down to on the 10ms grid.
	if _, err = s.Write([]byte("e")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(14 * ms)
	if err = s.Mark("last"); err != nil {
		t.Fatal(err)
	}
	if err = last.Stop(); err != nil {
		t.Fatalf("Stop with a mark after the last frame: %v", err)
	}
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = never.Stop(); !errors.Is(err, reel.ErrNoMark) {
		t.Fatalf("Stop = %v, want an error wrapping reel.ErrNoMark", err)
	}
	if fi, serr := os.Stat(filepath.Join(dir, "never.gif")); serr != nil || fi.Size() == 0 {
		t.Fatalf("the recording with a missing mark was not written: %v", serr)
	}
	if err = reel.Export(t.Context(), s.Timeline(), profile("export", "off")); err != nil {
		t.Fatal(err)
	}
	// An export of a timeline that never reached a mark writes what the
	// recording that waited for it did, and reports the same label.
	err = reel.Export(t.Context(), s.Timeline(), profile("export-never", "never"))
	if labels, rest := reel.MissingMarks(err); rest != nil || !slices.Equal(labels, []string{"never"}) {
		t.Fatalf("Export = %v, want a NoMarkError for \"never\"", err)
	}
	for _, pair := range [][2]string{{"record", "export"}, {"never", "export-never"}} {
		a, aerr := os.ReadFile(filepath.Join(dir, pair[0]+".gif"))
		b, berr := os.ReadFile(filepath.Join(dir, pair[1]+".gif"))
		if aerr != nil || berr != nil || !slices.Equal(a, b) {
			t.Fatalf("%s and %s differ (%v, %v)", pair[0], pair[1], aerr, berr)
		}
	}
}
