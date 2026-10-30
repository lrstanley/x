// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"encoding/base64"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// osc9 and osc777 are output that sends a desktop notification.
func osc9(body string) string          { return "\x1b]9;" + body + "\x07" }
func osc777(title, body string) string { return "\x1b]777;notify;" + title + ";" + body + "\x07" }

// osc52 is output that writes text to the clipboard.
func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

func TestToastText(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                          "",
		" \t\r\n":                   "",
		"build done":                "build done",
		"  two\n\nlines\t here  ":   "two lines here",
		"bell\x07 and \x1b escape":  "bell and escape",
		strings.Repeat("ab", 30):    strings.Repeat("ab", 19) + "a…",
		"日本語のテキスト":                  "日本語のテキスト",
		strings.Repeat("日", 30):     strings.Repeat("日", 19) + "…",
		"line\u2028separated\u0085": "line separated",
		"\u202egnp.exe\u200b":       "gnp.exe",
	} {
		if got := toastText(in); got != want {
			t.Errorf("toastText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestToastLook checks a toast's look on a 50ms grid: it fades and slides
// in over 150ms in thirds, holds, and fades out over the last 300ms of
// 600ms in quarters.
func TestToastLook(t *testing.T) {
	t.Parallel()
	s := &toastStage{ctx: &reel.Context{Step: 50 * ms, Speed: 1}, shown: 600 * ms}
	for _, tc := range []struct {
		p         time.Duration
		alpha, in uint8
	}{
		{50 * ms, 0, 0},
		{100 * ms, 85, 85},
		{150 * ms, 170, 170},
		{200 * ms, 255, 255},
		{400 * ms, 255, 255},
		{450 * ms, 191, 255},
		{500 * ms, 191, 255},
		{550 * ms, 128, 255},
		{650 * ms, 64, 255},
		{700 * ms, 0, 0},
	} {
		if got := s.look(100*ms, tc.p); got != (toastLook{tc.alpha, tc.in}) {
			t.Errorf("look at %v = %+v, want {%d %d}", tc.p, got, tc.alpha, tc.in)
		}
	}
}

// TestToastsTiming checks where a toast splits the frames it shows on:
// every step of its fades, on the delay grid, without changing how long
// the recording plays.
func TestToastsTiming(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "",
		output(0, "x"),
		output(100, osc9("hi")),
		mark(2000, "end"),
	)
	var in []reel.Frame
	out := exportFrames(t, tl, reel.Profile{
		Rasterizer: toastRast,
		Effects:    []reel.Effect{recordFrames{&in}, Toasts(WithToastDuration(600 * ms))},
	})
	if got, want := totalDelay(out), totalDelay(in); got != want {
		t.Errorf("toasts changed the recording length from %v to %v", want, got)
	}
	type piece struct {
		at, delay time.Duration
		toast     bool
	}
	// See TestToastLook for the look of a toast at 100ms.
	want := []piece{
		{0, 100 * ms, false},
		{100 * ms, 50 * ms, true},
		{150 * ms, 50 * ms, true},
		{200 * ms, 250 * ms, true},
		{450 * ms, 100 * ms, true},
		{550 * ms, 100 * ms, true},
		{650 * ms, 50 * ms, true},
		{700 * ms, 1300 * ms, false},
	}
	var got []piece
	for i, shown := range overlaid(in, out) {
		got = append(got, piece{out[i].At, out[i].Delay, shown})
	}
	if !slices.Equal(got, want) {
		t.Fatalf("pieces = %+v, want %+v", got, want)
	}
	for i := 1; i < len(out)-2; i++ {
		if bytes.Equal(out[i].Image.Pix, out[i+1].Image.Pix) {
			t.Errorf("pieces %d and %d look the same; want a fade", i, i+1)
		}
	}

	// At double speed, and at 30fps, whose step does not divide the fades,
	// the toast plays the same 600ms, and fades all the way in.
	for _, p := range []reel.Profile{{Speed: 2}, {FPS: 30}} {
		in = nil
		p.Rasterizer = toastRast
		p.Effects = []reel.Effect{recordFrames{&in}, Toasts(WithToastDuration(600 * ms))}
		out = exportFrames(t, tl, p)
		if got, want := totalDelay(out), totalDelay(in); got != want {
			t.Errorf("x%v %dfps: toasts changed the recording length from %v to %v", p.Speed, p.FPS, want, got)
		}
		var shownFor, longest time.Duration
		for i, shown := range overlaid(in, out) {
			if shown {
				shownFor += out[i].Delay
				longest = max(longest, out[i].Delay)
			}
		}
		if shownFor < 590*ms || shownFor > 610*ms {
			t.Errorf("x%v %dfps: toast shown for %v, want 600ms", p.Speed, p.FPS, shownFor)
		}
		// The opaque hold is the longest piece: 600ms less the fades.
		if longest < 200*ms || longest > 300*ms {
			t.Errorf("x%v %dfps: longest toast piece %v, want the hold of about 250ms", p.Speed, p.FPS, longest)
		}
	}
}

// toastRast is a fake rasterizer with cells about the size of the card
// text, so toasts fit on small terminals.
var toastRast = testutil.Rasterizer{Cell: image.Pt(10, 20)}

// TestToastsNarrow checks that a toast too wide for the grid is cut to fit
// it, and that one with no room is not shown.
func TestToastsNarrow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cols  int
		shown bool
	}{
		{14, true},
		{2, false},
	} {
		tl := buildTimeline(t, tc.cols, 4, "",
			output(0, "x"),
			output(100, osc777("Disk almost full", strings.Repeat("x", 40))),
			mark(1000, "end"),
		)
		var in []reel.Frame
		out := exportFrames(t, tl, reel.Profile{
			Rasterizer: toastRast,
			Effects:    []reel.Effect{recordFrames{&in}, Toasts()},
		})
		shown := overlaid(in, out)
		if slices.Contains(shown, true) != tc.shown {
			t.Errorf("%d cols: frames overlaid %v, want a toast %v", tc.cols, shown, tc.shown)
		}
		if !tc.shown {
			continue
		}
		// Once it has slid in, the toast is inside the grid.
		i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At == 200*ms })
		if i < 0 {
			t.Fatalf("%d cols: no frame at 200ms in %+v", tc.cols, infoOf(out))
		}
		assertOutsideGrid(t, in[len(in)-1].Image, out[i].Image, toastRast.Layout(tc.cols, 4).Grid)
	}
}

// TestToastsLeavesAlone checks what Toasts does not show: notifications
// before the window, in what a Hide cuts, and without text, and frames
// without a terminal.
func TestToastsLeavesAlone(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "title",
		output(0, "x"),
		output(100, osc9("before")),
		mark(200, "start"),
		mark(300, "hide"),
		output(400, osc9("hidden")),
		mark(500, "show"),
		output(600, osc777("", " \t")),
		mark(1000, "end"),
	)
	cut, err := tl.Between("start", "")
	if err != nil {
		t.Fatal(err)
	}
	var in []reel.Frame
	out := exportFrames(t, cut, reel.Profile{
		Rasterizer: toastRast,
		Effects:    []reel.Effect{Intro(500 * ms), Hide(HideBetween("hide", "show")), recordFrames{&in}, Toasts(WithToastDuration(5 * time.Second))},
	})
	if out[0].Cells != nil {
		t.Fatal("first frame is not the intro card")
	}
	if shown := overlaid(in, out); slices.Contains(shown, true) {
		t.Errorf("frames overlaid %v, want none", shown)
	}
}

// TestToastsPaused checks that a paused frame shows the toasts of its
// moment, unsplit, and that the pause does not use up their time.
func TestToastsPaused(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "",
		output(0, "x"),
		output(100, osc9("hi")),
		mark(200, "pause"),
		mark(1000, "end"),
	)
	var in []reel.Frame
	out := exportFrames(t, tl, reel.Profile{
		Rasterizer: toastRast,
		Effects:    []reel.Effect{Pause(PauseAtMarks(time.Second, "pause"), WithoutBadge()), recordFrames{&in}, Toasts(WithToastDuration(600 * ms))},
	})
	var paused, after time.Duration
	for i, shown := range overlaid(in, out) {
		switch {
		case out[i].Paused:
			if !shown || out[i].Delay != time.Second {
				t.Errorf("paused piece %d plays %v, toast shown %v; want 1s with the toast", i, out[i].Delay, shown)
			}
			paused++
		case shown && out[i].At >= 200*ms:
			after += out[i].Delay
		}
	}
	if paused != 1 {
		t.Errorf("%d paused pieces, want 1", paused)
	}
	if after != 500*ms {
		t.Errorf("toast shown for %v after the pause, want the 500ms left", after)
	}
}

// TestToastNotifications checks that a clipboard write shows among the
// notifications by when it arrived, without its text, and only with
// clipboard toasts.
func TestToastNotifications(t *testing.T) {
	t.Parallel()
	st := vt.State{
		Notifications: []vt.Notification{{At: 100 * ms, Body: "a"}, {At: 300 * ms, Body: "b"}},
		Clipboard:     vt.ClipboardWrite{Count: 1, Text: "hunter2", At: 200 * ms},
	}
	off := &toastStage{}
	if got := off.notifications(st); !slices.Equal(got, st.Notifications) {
		t.Errorf("without clipboard toasts = %+v, want the notifications", got)
	}
	on := &toastStage{clipboard: true}
	want := []vt.Notification{st.Notifications[0], {At: 200 * ms, Title: toastCopied}, st.Notifications[1]}
	if got := on.notifications(st); !slices.Equal(got, want) {
		t.Errorf("with clipboard toasts = %+v, want %+v", got, want)
	}
	if got := on.notifications(vt.State{Notifications: st.Notifications}); !slices.Equal(got, st.Notifications) {
		t.Errorf("with no clipboard write = %+v, want the notifications", got)
	}
	if st.Notifications[1].Body != "b" {
		t.Error("notifications modified")
	}
}

// TestToastsClipboard checks that a clipboard write shows a toast only with
// clipboard toasts, and like a notification, not before the window.
func TestToastsClipboard(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 30, 4, "",
		output(0, "x"),
		output(100, osc52("before")),
		mark(200, "start"),
		output(300, osc52("hunter2")),
		mark(1000, "end"),
	)
	cut, err := tl.Between("start", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		opts []ToastOption
		want time.Duration
	}{
		{nil, 0},
		{[]ToastOption{WithClipboardToasts()}, 500 * ms},
	} {
		var in []reel.Frame
		out := exportFrames(t, cut, reel.Profile{
			Rasterizer: toastRast,
			Effects:    []reel.Effect{recordFrames{&in}, Toasts(append(tc.opts, WithToastDuration(500*ms))...)},
		})
		var shownFor time.Duration
		for i, shown := range overlaid(in, out) {
			if shown {
				shownFor += out[i].Delay
			}
		}
		if shownFor != tc.want {
			t.Errorf("%d options: toast shown for %v, want %v", len(tc.opts), shownFor, tc.want)
		}
	}
}

// toastRenderOpts are the options of the renderer the golden tests draw
// with.
var toastRenderOpts = []render.Option{render.WithWindowChrome(true), render.WithPadding(4)}

// toastFrames exports tl with a renderer and Toasts(opts...), and returns
// the frames.
func toastFrames(t *testing.T, tl *reel.Timeline, opts ...ToastOption) []reel.Frame {
	t.Helper()
	return exportFrames(t, tl, reel.Profile{
		Rasterizer: zoomRenderer(t, toastRenderOpts...),
		Render:     toastRenderOpts,
		Effects:    []reel.Effect{Toasts(opts...)},
	})
}

// TestToastsStack checks the look of toasts: a title over a body, a body
// alone, stacked newest first, at most three, sliding and fading in, and
// the oldest fading out first.
func TestToastsStack(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 50, 12, "toasts",
		output(0, "$ make"),
		output(100, osc777("make", "build finished in 3.2s")),
		output(200, osc9("tests passed")),
		output(300, osc777("deploy", "")),
		output(400, osc777("Disk almost full", "/dev/sda1 is at 97% capacity, clean up soon")),
		mark(3000, "end"),
	)
	out := toastFrames(t, tl)
	// The first toast in full, the fourth fading in, the stack of three, and
	// the oldest two fading out.
	var golden []reel.Frame
	for _, at := range []time.Duration{200 * ms, 450 * ms, 500 * ms, 2050 * ms} {
		i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At == at })
		if i < 0 {
			t.Fatalf("no frame at %v in %+v", at, infoOf(out))
		}
		golden = append(golden, out[i])
	}
	assertGolden(t, "toasts-stack", golden)
}

// TestToastsCorner checks toasts stacked up from a bottom corner.
func TestToastsCorner(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 8, "",
		output(0, "$ ./notify"),
		output(100, osc777("first", "one")),
		output(200, osc777("second", "two")),
		mark(1000, "end"),
	)
	out := toastFrames(t, tl, WithToastCorner(BottomLeft))
	i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At == 300*ms })
	if i < 0 {
		t.Fatalf("no frame at 300ms in %+v", infoOf(out))
	}
	assertGolden(t, "toasts-corner", out[i:i+1])
}

// TestToastsClipboardLook checks the look of a clipboard write's toast, a
// title alone without the text written, stacked over a notification.
func TestToastsClipboardLook(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 8, "",
		output(0, "$ gh auth token | osc-copy"),
		output(100, osc777("make", "build finished")),
		output(200, osc52("hunter2")),
		mark(1000, "end"),
	)
	out := toastFrames(t, tl, WithClipboardToasts())
	i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At == 300*ms })
	if i < 0 {
		t.Fatalf("no frame at 300ms in %+v", infoOf(out))
	}
	assertGolden(t, "toasts-clipboard", out[i:i+1])
}
