// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
)

// shown is a frame as the hide tests compare it: the screen's text, without
// trailing blanks, where it is in the timeline, and how long it plays.
type shown struct {
	text      string
	at, delay time.Duration
}

// hideFPS puts the delay grid on steps of 10ms.
const hideFPS = 100

// shownFrames exports tl through effects at speed and returns its frames as
// shown.
func shownFrames(t *testing.T, tl *reel.Timeline, speed float64, effects ...reel.Effect) []shown {
	t.Helper()
	var out []shown
	for _, f := range exportFrames(t, tl, reel.Profile{FPS: hideFPS, Speed: speed, Effects: effects}) {
		out = append(out, shown{screenText(f.Cells), f.At, f.Delay})
	}
	return out
}

// screenText is the text of the screen of f, without trailing blanks.
func screenText(f *reel.CellFrame) string {
	lines := strings.Split(f.Screen.Text(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// TestHide checks the spans of HideBetween: cut out of playback, with the
// screen before a span playing up to it and the screen at its end from
// there.
func TestHide(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 2, "",
		output(0, "a"), output(100, "b"), mark(150, "hide"), output(200, "c"), output(300, "d"),
		mark(350, "show"), output(400, "e"), mark(450, "hide"), output(500, "f"), mark(550, "show"),
		output(600, "g"), mark(700, "end"),
	)
	for _, tc := range []struct {
		name  string
		rules []HideRule
		want  []shown
	}{
		{"middle", []HideRule{HideBetween("hide", "show")}, []shown{
			{"a", 0, 100 * ms},
			{"ab", 100 * ms, 50 * ms},
			{"abcd", 350 * ms, 50 * ms},
			{"abcde", 400 * ms, 50 * ms},
			{"abcdef", 550 * ms, 50 * ms},
			{"abcdefg", 600 * ms, 100 * ms},
		}},
		{"leading", []HideRule{HideBetween("", "show")}, []shown{
			{"abcd", 350 * ms, 50 * ms},
			{"abcde", 400 * ms, 100 * ms},
			{"abcdef", 500 * ms, 100 * ms},
			{"abcdefg", 600 * ms, 100 * ms},
		}},
		{"unclosed", []HideRule{HideBetween("hide", "")}, []shown{
			{"a", 0, 100 * ms}, {"ab", 100 * ms, 50 * ms},
		}},
		{"two rules", []HideRule{HideBetween("", "show"), HideWhile(Text("f"))}, []shown{
			{"abcd", 350 * ms, 50 * ms}, {"abcde", 400 * ms, 100 * ms},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shownFrames(t, tl, 1, Hide(tc.rules...)); !slices.Equal(got, tc.want) {
				t.Errorf("frames = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHideFrameEdges checks spans that start or end inside one frame, or
// on its edges, and a span of no time.
func TestHideFrameEdges(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 2, "",
		output(0, "a"), mark(0, "h"), mark(30, "s"), mark(60, "h"), mark(60, "s"), mark(80, "h"),
		output(100, "b"), mark(130, "s"), mark(200, "end"),
	)
	want := []shown{{"a", 30 * ms, 30 * ms}, {"a", 60 * ms, 20 * ms}, {"ab", 130 * ms, 70 * ms}}
	if got := shownFrames(t, tl, 1, Hide(HideBetween("h", "s"))); !slices.Equal(got, want) {
		t.Errorf("frames = %v, want %v", got, want)
	}
}

// TestHideScreen checks that HideWhile and HideUnless cut the frames that
// show, or do not show, their region, and that Keys after Hide leaves out
// the keys pressed while they are cut.
func TestHideScreen(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 3, "",
		output(0, "boot"), output(100, "\r\n$ "), input(150, "x"), output(200, "secret"),
		output(300, "\r\n$ "), mark(400, "end"),
	)
	for _, tc := range []struct {
		name string
		rule HideRule
		want []string
	}{
		{"while", HideWhile(Text("secret")), []string{"boot", "boot\n$"}},
		{"unless", HideUnless(Text("$")), []string{"boot\n$", "boot\n$ secret", "boot\n$ secret\n$"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var texts []string
			for _, f := range shownFrames(t, tl, 1, Hide(tc.rule)) {
				texts = append(texts, f.text)
			}
			if !slices.Equal(texts, tc.want) {
				t.Errorf("frames = %q, want %q", texts, tc.want)
			}
		})
	}
}

// TestHideKeys checks that Keys after Hide leaves out the keys pressed in
// what Hide cuts, and shows the others.
func TestHideKeys(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 80, 24, "",
		output(0, "a"), mark(100, "hide"), output(100, "\r\nb"), input(150, "x"),
		mark(300, "show"), output(300, "\x1b[2K"), input(400, "y"), mark(1000, "end"),
	)
	for _, tc := range []struct {
		name string
		rule HideRule
	}{
		{"between", HideBetween("hide", "show")},
		{"while", HideWhile(Text("b"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var in []reel.Frame
			out := exportFrames(t, tl, reel.Profile{FPS: hideFPS, Effects: []reel.Effect{
				Hide(tc.rule), recordFrames{&in}, Keys(time.Second),
			}})
			for i, overlay := range overlaid(in, out) {
				if want := out[i].At >= 400*ms; overlay != want {
					t.Errorf("frame at %v has overlay %v, want %v", out[i].At, overlay, want)
				}
			}
		})
	}
}

// TestHidePaused checks that a paused frame plays or is cut whole, by
// whether its moment is hidden.
func TestHidePaused(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 10, "", output(0, "a"), mark(100, "p"), mark(150, "h"), mark(200, "p"), mark(300, "s"), mark(400, "end"))
	out := exportFrames(t, tl, reel.Profile{FPS: hideFPS, Effects: []reel.Effect{
		Pause(PauseAtMarks(time.Second, "p"), WithoutBadge()), Hide(HideBetween("h", "s")),
	}})
	var pauses []time.Duration
	for _, f := range out {
		if f.Paused {
			pauses = append(pauses, f.At)
		}
	}
	if !slices.Equal(pauses, []time.Duration{100 * ms}) {
		t.Errorf("pauses at %v, want only the one outside the span, at 100ms", pauses)
	}
	if total := totalDelay(out); total != 1250*ms {
		t.Errorf("plays for %v, want 1.25s: the timeline less the span, and a pause", total)
	}
}

// TestHideStart checks the rules Start rejects, and that a profile with
// Hide and an asciicast output is an error.
func TestHideStart(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 2, "", output(0, "a"), mark(100, "hide"), mark(200, "show"), mark(300, "hide"))
	for _, tc := range []struct {
		name    string
		effect  reel.Effect
		outputs []reel.File
		is      error
		text    string
	}{
		{"no rules", Hide(), nil, nil, "without rules"},
		{"everything", Hide(HideBetween("", "")), nil, nil, "hides everything"},
		{"nil region", Hide(HideWhile(nil)), nil, nil, "without a region"},
		{"no hide mark", Hide(HideBetween("nope", "show")), nil, reel.ErrNoMark, `"nope"`},
		{"no show mark", Hide(HideBetween("hide", "nope")), nil, reel.ErrNoMark, `"nope"`},
		{"show before hide", Hide(HideBetween("show", "hide")), nil, nil, ""},
		{"every frame", Hide(HideWhile(Text("a"))), nil, nil, "dropped every frame"},
		{"cast", Hide(HideBetween("hide", "show")), reel.Files("x.gif", "x.cast"), reel.ErrRawOutput, "asciicast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p := reel.Profile{Rasterizer: testutil.Rasterizer{}, Effects: []reel.Effect{tc.effect}}
			p.Outputs = tc.outputs
			if p.Outputs == nil {
				p.Outputs = reel.Files("x.gif")
			}
			for i := range p.Outputs {
				p.Outputs[i].Path = filepath.Join(dir, p.Outputs[i].Path)
			}
			err := reel.Export(t.Context(), tl, p)
			switch {
			case tc.text == "":
				if err != nil {
					t.Errorf("Export = %v, want nil", err)
				}
			case err == nil || !strings.Contains(err.Error(), tc.text):
				t.Errorf("Export = %v, want an error with %q", err, tc.text)
			case tc.is != nil && !errors.Is(err, tc.is):
				t.Errorf("Export = %v, want %v", err, tc.is)
			}
		})
	}
}

// TestHideMissingMark checks that an export whose marks never came cuts
// as a live recording would: nothing without a hide mark, and to the end
// without a show mark, and reports the mark once the outputs are written.
func TestHideMissingMark(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 2, "", output(0, "a"), mark(100, "hide"), output(200, "b"), mark(300, "end"))
	for _, tc := range []struct {
		name       string
		hide, show string
		want       []string
	}{
		{"hide", "nope", "end", []string{"a", "ab"}},
		{"show", "hide", "nope", []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "out.gif")
			var frames []reel.Frame
			err := reel.Export(t.Context(), tl, reel.Profile{
				Outputs:    reel.Files(path),
				Rasterizer: testutil.Rasterizer{},
				FPS:        hideFPS,
				Effects:    []reel.Effect{Hide(HideBetween(tc.hide, tc.show)), recordFrames{&frames}},
			})
			if labels, rest := reel.MissingMarks(err); !slices.Equal(labels, []string{"nope"}) || rest != nil {
				t.Errorf("Export = %v, want the missing mark nope", err)
			}
			if _, serr := os.Stat(path); serr != nil {
				t.Error(serr)
			}
			var texts []string
			for _, f := range frames {
				texts = append(texts, screenText(f.Cells))
			}
			if !slices.Equal(texts, tc.want) {
				t.Errorf("frames = %q, want %q", texts, tc.want)
			}
		})
	}
}

// TestHideRecord checks that a live recording cuts what an export of its
// timeline does, Keys leaving out the key typed in the cut, and that a mark
// it never gets is an error from Stop.
func TestHideRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var in, out []reel.Frame
	profile := func(name, show string, frames ...*[]reel.Frame) reel.Profile {
		effects := []reel.Effect{Hide(HideBetween("hide", show)), Keys(time.Second)}
		if frames != nil {
			effects = []reel.Effect{effects[0], recordFrames{frames[0]}, effects[1], recordFrames{frames[1]}}
		}
		return reel.Profile{
			Outputs:    reel.Files(filepath.Join(dir, name+".gif")),
			Rasterizer: testutil.Rasterizer{},
			FPS:        hideFPS,
			Effects:    effects,
		}
	}
	s, err := reel.Open(t.Context(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, err := reel.Record(t.Context(), s, profile("record", "show", &in, &out))
	if err != nil {
		t.Fatal(err)
	}
	missing, err := reel.Record(t.Context(), s, profile("missing", "never"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { _, werr := s.Write([]byte("a")); return werr },
		func() error { return s.Mark("hide") },
		func() error { _, werr := s.Write([]byte("b")); return werr },
		func() error { return s.Type("x") },
		func() error { return s.Mark("show") },
		func() error { _, werr := s.Write([]byte("c")); return werr },
	} {
		if err = step(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * ms)
	}
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = missing.Stop(); !errors.Is(err, reel.ErrNoMark) {
		t.Errorf("Stop = %v, want ErrNoMark", err)
	}

	marks := map[string]time.Duration{}
	for m := range s.Timeline().Marks() {
		marks[m.Label] = m.At()
	}
	var texts []string
	for _, f := range in {
		texts = append(texts, screenText(f.Cells))
		if f.At > marks["hide"] && f.At < marks["show"] {
			t.Errorf("frame at %v, in the span from %v to %v", f.At, marks["hide"], marks["show"])
		}
	}
	if want := []string{"a", "ab", "abc"}; !slices.Equal(texts, want) {
		t.Errorf("frames = %q, want %q", texts, want)
	}
	if slices.Contains(overlaid(in, out), true) {
		t.Error("Keys shows the key typed in the span")
	}

	if err = reel.Export(t.Context(), s.Timeline(), profile("export", "show")); err != nil {
		t.Fatal(err)
	}
	recorded, rerr := os.ReadFile(filepath.Join(dir, "record.gif"))
	exported, eerr := os.ReadFile(filepath.Join(dir, "export.gif"))
	if rerr != nil || eerr != nil || !bytes.Equal(recorded, exported) {
		t.Errorf("recorded and exported GIFs differ (%v, %v)", rerr, eerr)
	}
}

// TestHidePauseAfter checks that Pause after Hide leaves out the marks in
// what Hide cuts, and pauses at the others.
func TestHidePauseAfter(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 10, "",
		output(0, "a"), mark(100, "h"), mark(150, "p"), output(200, "b"), mark(300, "s"), mark(450, "p"), mark(500, "end"),
	)
	out := exportFrames(t, tl, reel.Profile{FPS: hideFPS, Effects: []reel.Effect{
		Hide(HideBetween("h", "s")), Pause(PauseAtMarks(time.Second, "p"), WithoutBadge()),
	}})
	var pauses []time.Duration
	for _, f := range out {
		if f.Paused {
			pauses = append(pauses, f.At)
		}
	}
	if !slices.Equal(pauses, []time.Duration{450 * ms}) {
		t.Errorf("pauses at %v, want only the one outside the span, at 450ms", pauses)
	}
}

// TestHideSpeed checks that the cuts fall on the delay grid of a recording
// played faster.
func TestHideSpeed(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 2, "", output(0, "a"), mark(100, "h"), output(200, "b"), mark(300, "s"), output(400, "c"), mark(600, "end"))
	got := shownFrames(t, tl, 2, Hide(HideBetween("h", "s")))
	want := []shown{{"a", 0, 50 * ms}, {"ab", 300 * ms, 50 * ms}, {"abc", 400 * ms, 100 * ms}}
	if !slices.Equal(got, want) {
		t.Errorf("frames = %v, want %v", got, want)
	}
}
