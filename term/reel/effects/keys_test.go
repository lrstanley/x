// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
)

func TestKeyLabels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"\x1b[B", []string{"↓"}},
		{"\r", []string{"⏎"}},
		{"\x03", []string{"ctrl+c"}},
		{"\x1b[Z", []string{"shift+⇥"}},
		{"\x1b\r", []string{"alt+⏎"}},
		{"\x1b[5~", []string{"pgup"}},
		{"\x1b", []string{"esc"}},
		{"\x7f", []string{"⌫"}},
		{"+", []string{"+"}},
		{" ", []string{"␣"}},
		{"\u3000", []string{"␣"}},
		{"                    ", []string{"␣␣␣␣␣␣␣␣␣␣␣␣␣␣␣…"}},
		{"a b", []string{"a b"}},
		// Typed text, with enter and tab, is one chip, as typing "hi\n"
		// sends a newline, which decodes as ctrl+j.
		{"hi\n", []string{"hi⏎"}},
		{"a\tb\r", []string{"a⇥b⏎"}},
		{"\n", []string{"ctrl+j"}},
		{"ab\x1b[Ac", []string{"ab", "↑", "c"}},
		{"abcdefghijklmnopqrstuvwxyz", []string{"abcdefghijklmno…"}},
		// Paste and mouse are not shown.
		{"\x1b[200~x\x1b[201~", nil},
		{"\x1b[<0;3;2M", nil},
		// So are modifiers pressed on their own, which the kitty keyboard
		// protocol can report.
		{"\x1b[57441;2u\x1b[97;6u", []string{"ctrl+shift+a"}},
		{"a\x1b[200~x\x1b[201~b", []string{"a", "b"}},
	} {
		if got := keyLabels(reel.Input{Data: []byte(tc.in)}); !slices.Equal(got, tc.want) {
			t.Errorf("keyLabels(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChipGroups(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		labels []string
		want   []string
	}{
		{nil, nil},
		{[]string{"a", "a", "a", "b", "a"}, []string{"a ×3", "b", "a"}},
		// Only the newest six chips are kept.
		{[]string{"1", "2", "3", "4", "4", "5", "6", "7"}, []string{"2", "3", "4 ×2", "5", "6", "7"}},
	} {
		if got := chipGroups(tc.labels); !slices.Equal(got, tc.want) {
			t.Errorf("chipGroups(%q) = %q, want %q", tc.labels, got, tc.want)
		}
	}
}

func TestKeyTimelineState(t *testing.T) {
	t.Parallel()
	// Keys stay 1500ms after a burst: opaque for 1000ms, then three fade
	// levels on the 50ms grid at 1000, 1150 (1166 snapped), and 1350 (1333).
	tl := keyTimeline{
		at:    []time.Duration{0, 100 * ms, 3000 * ms},
		shown: 1500 * ms, step: 50 * ms,
	}
	for _, tc := range []struct {
		p    time.Duration
		want keyState
	}{
		{-50 * ms, noKeys},
		{0, keyState{0, 0, 0}},
		{100 * ms, keyState{0, 1, 0}},
		{1099 * ms, keyState{0, 1, 0}},
		{1100 * ms, keyState{0, 1, 1}},
		{1250 * ms, keyState{0, 1, 2}},
		{1450 * ms, keyState{0, 1, 3}},
		{1600 * ms, noKeys},
		{3000 * ms, keyState{2, 2, 0}},
	} {
		if got := tl.state(tc.p); got != tc.want {
			t.Errorf("state(%v) = %+v, want %+v", tc.p, got, tc.want)
		}
	}
}

// overlaid reports which of out show an image none of in do.
func overlaid(in, out []reel.Frame) []bool {
	shown := make([]bool, len(out))
	for i, f := range out {
		shown[i] = !slices.ContainsFunc(in, func(g reel.Frame) bool { return g.Image == f.Image })
	}
	return shown
}

func TestKeysOverlay(t *testing.T) {
	t.Parallel()
	// The overlay runs between two recorders, so the frames on each side of
	// it can be compared. Nothing echoes the input, so one terminal frame
	// plays for the whole second.
	tl := buildTimeline(t, 80, 24, "",
		output(0, "x"),
		input(100, "abc"),
		input(150, "\r"),
		mark(1000, "end"),
	)
	var in []reel.Frame
	out := exportFrames(t, tl, reel.Profile{
		Effects: []reel.Effect{recordFrames{&in}, Keys(300 * ms)},
		Hold:    -1,
	})
	if len(in) != 1 {
		t.Fatalf("%d terminal frames, want 1", len(in))
	}
	if got, want := totalDelay(out), totalDelay(in); got != want {
		t.Errorf("overlay changed the recording length from %v to %v", want, got)
	}

	// Keys stay 300ms: opaque for 200ms, then fade levels at a third and two
	// thirds of the rest, snapped to the 50ms grid, so "abc" at 100ms and
	// enter at 150ms show from 100 to 350ms, fade at 350 and 400ms, and are
	// gone at 450ms.
	type piece struct {
		at, delay time.Duration
		overlay   bool
	}
	want := []piece{
		{0, 100 * ms, false},
		{100 * ms, 50 * ms, true},
		{150 * ms, 200 * ms, true},
		{350 * ms, 50 * ms, true},
		{400 * ms, 50 * ms, true},
		{450 * ms, 550 * ms, false},
	}
	var got []piece
	for i, shown := range overlaid(in, out) {
		got = append(got, piece{out[i].At, out[i].Delay, shown})
		if out[i].Image.Bounds() != in[0].Image.Bounds() || out[i].Cells != in[0].Cells {
			t.Errorf("piece %d is not the terminal frame overlaid", i)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("pieces = %+v, want %+v", got, want)
	}
	for i := 1; i < 4; i++ {
		if bytes.Equal(out[i].Image.Pix, out[i+1].Image.Pix) {
			t.Errorf("pieces %d and %d look the same; want new chips or a fade", i, i+1)
		}
	}
}

// TestKeysTiming checks that at any speed and hold, and with frames that
// change while keys show, the overlay keeps every frame's length and splits
// it on the delay grid.
func TestKeysTiming(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "",
		output(0, "x"),
		input(100, "a"),
		output(130, "y"),
		input(170, "a"),
		input(260, "\x1b[A"),
		output(420, "z"),
		input(900, "b"),
		mark(1500, "end"),
	)
	for _, p := range []reel.Profile{
		{Speed: 2, Hold: 300 * ms},
		{Speed: 0.5, FPS: 30, Hold: -1},
		{FPS: 100},
	} {
		var in []reel.Frame
		p.Effects = []reel.Effect{recordFrames{&in}, Keys(400 * ms)}
		out := exportFrames(t, tl, p)
		if got, want := totalDelay(out), totalDelay(in); got != want {
			t.Errorf("speed %v fps %d: overlay changed the recording length from %v to %v", p.Speed, p.FPS, want, got)
		}
		step := 50 * ms
		if p.FPS != 0 {
			step = time.Duration(100/p.FPS) * 10 * ms
		}
		for i, f := range out[:len(out)-1] {
			if f.Delay < step || f.Delay%step != 0 {
				t.Errorf("speed %v fps %d: piece %d plays %v, off the %v grid", p.Speed, p.FPS, i, f.Delay, step)
			}
		}
		if !slices.Contains(overlaid(in, out), true) {
			t.Errorf("speed %v fps %d: no frame shows the keys", p.Speed, p.FPS)
		}
	}
}

// TestKeysSkipsGoneBursts checks that skipping the presses of bursts that
// are gone changes nothing a frame shows.
func TestKeysSkipsGoneBursts(t *testing.T) {
	t.Parallel()
	for _, speed := range []float64{0.5, 1, 3} {
		k := &keysStage{ctx: &reel.Context{Step: 50 * ms, Speed: speed}, shown: 300 * ms}
		for i, at := range []int{0, 100, 390, 420, 1000, 1310, 1400, 2000, 2010, 2900, 4600, 4650} {
			k.presses = append(k.presses, keyPress{at: time.Duration(at) * ms, label: string(rune('a' + i))})
		}
		all := *k
		for at := time.Duration(0); at < 6000*ms; at += 70 * ms {
			f := reel.Frame{At: at, Delay: 200 * ms}
			k.skipGone(at)
			skipped, full := k.timeline(f), all.timeline(f)
			for p := time.Duration(0); p < f.Delay; p += 10 * ms {
				s, w := skipped.state(p), full.state(p)
				if s == noKeys != (w == noKeys) || (s != noKeys && (!slices.Equal(skipped.labels[s.first:s.last+1], full.labels[w.first:w.last+1]) || s.level != w.level)) {
					t.Fatalf("x%v frame at %v, offset %v: shows %+v with skipping, %+v without", speed, at, p, s, w)
				}
			}
		}
		if k.start == 0 {
			t.Errorf("x%v: no press was skipped", speed)
		}
	}
}

// TestKeysLeavesAlone checks what the overlay does not show: paste and
// mouse input, input before the window, frames without a terminal, and
// anything with a shown of zero.
func TestKeysLeavesAlone(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "title",
		output(0, "x"),
		input(100, "a"),
		mark(200, "start"),
		input(300, "\x1b[200~pasted\x1b[201~"),
		input(400, "\x1b[<0;3;2M"),
		mark(1000, "end"),
	)
	cut, err := tl.Between("start", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		shown time.Duration
	}{
		{"window", 300 * ms},
		{"zero", 0},
	} {
		var in []reel.Frame
		out := exportFrames(t, cut, reel.Profile{
			Effects: []reel.Effect{Intro(500 * ms), recordFrames{&in}, Keys(tc.shown)},
		})
		if len(out) != 2 || out[0].Cells != nil {
			t.Fatalf("%s: %d frames, want the intro card and the terminal", tc.name, len(out))
		}
		if shown := overlaid(in, out); slices.Contains(shown, true) {
			t.Errorf("%s: frames overlaid %v, want none", tc.name, shown)
		}
	}
}

// TestKeysShared checks that one effect value records the same in parallel
// as alone, so it keeps no state of its own, and that a recording of a
// running session draws what an export of its timeline does.
func TestKeysShared(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "title",
		output(0, "x"),
		input(100, "ab"),
		output(120, "\r\nab"),
		input(300, "\x03"),
		input(320, "\x03"),
		mark(900, "end"),
	)
	dir := t.TempDir()
	profile := func(name string) reel.Profile {
		return reel.Profile{
			Outputs:    reel.Files(filepath.Join(dir, name+".gif")),
			Rasterizer: testutil.Rasterizer{},
			Effects:    []reel.Effect{shared, sharedIntro},
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { errs[i] = reel.Export(t.Context(), tl, profile(strconv.Itoa(i))) })
	}
	wg.Wait()
	want, err := os.ReadFile(filepath.Join(dir, "0.gif"))
	if err != nil {
		t.Fatal(err)
	}
	for i, xerr := range errs {
		got, rerr := os.ReadFile(filepath.Join(dir, strconv.Itoa(i)+".gif"))
		if xerr != nil || rerr != nil || !bytes.Equal(got, want) {
			t.Errorf("export %d differs from export 0 (%v, %v)", i, xerr, rerr)
		}
	}

	s, err := reel.Open(t.Context(), 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := reel.Record(t.Context(), s, profile("record"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(out string) func() error {
		return func() error { _, werr := s.Write([]byte(out)); return werr }
	}
	for _, step := range []func() error{
		write("x"),
		func() error { return s.Type("ab") },
		write("\r\nab"),
		func() error { return s.Key("ctrl+c") },
		func() error { return s.Key("ctrl+c") },
	} {
		if err = step(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * ms)
	}
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = reel.Export(t.Context(), s.Timeline(), profile("export")); err != nil {
		t.Fatal(err)
	}
	recorded, rerr := os.ReadFile(filepath.Join(dir, "record.gif"))
	exported, eerr := os.ReadFile(filepath.Join(dir, "export.gif"))
	if rerr != nil || eerr != nil || !bytes.Equal(recorded, exported) {
		t.Errorf("recorded and exported GIFs differ (%v, %v)", rerr, eerr)
	}
}

// shared and sharedIntro are effect values every test may use at once.
var (
	shared      = Keys(300 * ms)
	sharedIntro = Intro(200 * ms)
)
