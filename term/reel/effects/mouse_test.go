// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// Button codes of SGR mouse reports: a press of left or right, motion
// with left held, and motion with no button held.
const (
	sgrLeft       = 0
	sgrRight      = 2
	sgrLeftMotion = 32
	sgrHover      = 35
)

// sgrMouse is the SGR mouse report of button code b at zero-based cell
// (x, y), a release when release is set.
func sgrMouse(b, x, y int, release bool) string {
	final := "M"
	if release {
		final = "m"
	}
	return fmt.Sprintf("\x1b[<%d;%d;%d%s", b, x+1, y+1, final)
}

// mouseScreen is the output the mouse tests draw over.
const mouseScreen = "$ ls\r\nREADME  main.go\r\n$ "

// mouseFrames exports tl with a renderer at 20 FPS through effects, and
// returns the frames the effects were given, and those they emitted.
func mouseFrames(t *testing.T, tl *reel.Timeline, effects ...reel.Effect) (in, out []reel.Frame) {
	t.Helper()
	return zoomFrames(t, tl, 20, zoomRenderer(t, zoomRenderOpts...), zoomRenderOpts, effects...)
}

// assertMouseFrames checks that out plays as long as in, and that the
// frames out from offset hidden on are in's own, the mouse having hidden.
func assertMouseFrames(t *testing.T, in, out []reel.Frame, hidden time.Duration) {
	t.Helper()
	if got, want := totalDelay(out), totalDelay(in); got != want {
		t.Fatalf("frames play for %v, want %v", got, want)
	}
	for i, f := range out {
		if f.At >= hidden && !slices.ContainsFunc(in, func(g reel.Frame) bool { return g.Image == f.Image }) {
			t.Fatalf("frame %d at %v still shows the mouse", i, f.At)
		}
	}
}

// TestMouseClick checks a left click: the pointer appears on its cell with
// a blue ripple that spreads and fades on every step, then hides.
func TestMouseClick(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 16, 3, "",
		output(0, mouseScreen),
		input(100, sgrMouse(sgrLeft, 3, 1, false)+sgrMouse(sgrLeft, 3, 1, true)),
		output(3000, " "),
	)
	in, out := mouseFrames(t, tl, Mouse(WithPointerHide(time.Second)))
	if out[0].Image != in[0].Image || out[0].Delay != 100*ms {
		t.Fatalf("first frame %+v, want the terminal's own until the click", infoOf(out[:1]))
	}
	// The pointer hides a second after the click, and fades for 250ms.
	assertMouseFrames(t, in, out, 1350*ms)
	assertGolden(t, "mouse-click", out[1:10])
}

// TestMouseDrag checks a left drag down and across, one cell per step: the
// trail follows the pointer through the cells sent, smoothed where they
// step on the grid, the held disc under the pointer at its end, and the
// trail fades once the button is released.
func TestMouseDrag(t *testing.T) {
	t.Parallel()
	events := []event{output(0, mouseScreen), input(100, sgrMouse(sgrLeft, 1, 0, false))}
	for x := 2; x <= 12; x++ {
		// The cells of the line to (12, 2), as reel.Session.Drag sends them.
		y := int(math.Round(float64(x-1) * 2 / 11))
		events = append(events, input(100+40*(x-1), sgrMouse(sgrLeftMotion, x, y, false)))
	}
	events = append(events, input(600, sgrMouse(sgrLeft, 12, 2, true)), output(4000, " "))
	tl := buildTimeline(t, 16, 3, "", events...)
	in, out := mouseFrames(t, tl, Mouse())
	// The pointer hides a second after the release, and fades for 250ms.
	assertMouseFrames(t, in, out, 1850*ms)
	assertGolden(t, "mouse-drag", slices.Concat(out[1:12], out[14:16]))
}

// TestMouseFastDrag checks that, with the defaults, the pointer keeps up
// with a drag of motion quicker than its glide, as [reel.Session.Drag]
// sends it: it reaches the press's cell first, even with motion at the
// same offset waiting for it, then stays within a cell of the latest
// event's cell, at the end of the trail, and reaches the release cell as
// soon after a quicker release as the release came after the motion.
func TestMouseFastDrag(t *testing.T) {
	t.Parallel()
	events := []event{
		output(0, mouseScreen),
		input(0, sgrMouse(sgrHover, 20, 8, false)),
		input(100, sgrMouse(sgrLeft, 2, 4, false)),
	}
	for x := 3; x <= 50; x++ {
		events = append(events, input(100+40*(x-3), sgrMouse(sgrLeftMotion, x, 4, false)))
		if x == 6 {
			events = append(events, input(220, sgrMouse(sgrLeftMotion, 7, 4, false)))
		}
	}
	events = append(events, input(2000, sgrMouse(sgrLeft, 50, 4, true)))
	tl := buildTimeline(t, 60, 10, "", events...)
	s := &mouseStage{
		ctx: &reel.Context{Timeline: tl}, parts: MouseAll,
		glide: mouseGlide, hide: mouseHide, fade: mouseFade, ripple: mouseRipple,
	}
	s.readEvents()
	if l := s.look(250 * ms); l.pos != (vec{2.5, 4.5}) {
		t.Errorf("once the glide to the press ends: pointer at %v, want on its cell", l.pos)
	}
	for _, at := range []time.Duration{300 * ms, 500 * ms, 1010 * ms, 1555 * ms, 1999 * ms} {
		latest := s.events[sort.Search(len(s.events), func(i int) bool { return s.events[i].at > at })-1].cell()
		l := s.look(at)
		if math.Abs(l.pos.x-latest.x) > 1 || l.pos.y != latest.y {
			t.Errorf("at %v: pointer at %v, want within a cell of %v", at, l.pos, latest)
		}
		if path := s.trailPath(l.trail); path[len(path)-1] != l.pos {
			t.Errorf("at %v: trail ends at %v, want at the pointer, %v", at, path[len(path)-1], l.pos)
		}
	}
	if l := s.look(2020 * ms); l.pos != (vec{50.5, 4.5}) {
		t.Errorf("20ms after the release: pointer at %v, want on its cell", l.pos)
	}
}

// TestSmoothPath checks that a smoothed path keeps its ends, keeps the
// points of a straight line where they are, even with a leg of no length,
// and curves through the steps of a path that steps from cell to cell
// instead of jumping at them, and that a path is resampled up to its end,
// even when its last leg is shorter than a step.
func TestSmoothPath(t *testing.T) {
	t.Parallel()
	if got, want := resample([]vec{{0, 0}, {2.5, 0}}, 1), []vec{{0, 0}, {1, 0}, {2, 0}, {2.5, 0}}; !slices.Equal(got, want) {
		t.Errorf("resampled: %v, want %v", got, want)
	}
	if got := smoothPath([]vec{{1, 1}, {1, 1}}, 1, 4); !slices.Equal(got, []vec{{1, 1}}) {
		t.Errorf("no length: %v, want its start", got)
	}
	line := smoothPath([]vec{{0, 0}, {4, 3}, {4, 3}, {10.4, 7.8}}, 1, 3)
	if len(line) != 14 || line[13] != (vec{10.4, 7.8}) {
		t.Fatalf("line: %v, want 14 points to (10.4, 7.8)", line)
	}
	for i, p := range line[:13] {
		if want := (vec{0.8 * float64(i), 0.6 * float64(i)}); math.Hypot(p.x-want.x, p.y-want.y) > 1e-9 {
			t.Errorf("line: point %d at %v, want %v", i, p, want)
		}
	}
	// Steps of 10 across, then 2 down.
	var steps []vec
	for i := range 4 {
		steps = append(steps, vec{float64(10 * i), float64(2 * i)}, vec{float64(10*i + 10), float64(2 * i)})
	}
	got := smoothPath(steps, 0.5, 6)
	if got[0] != steps[0] || got[len(got)-1] != steps[len(steps)-1] {
		t.Errorf("steps: from %v to %v, want from %v to %v", got[0], got[len(got)-1], steps[0], steps[len(steps)-1])
	}
	for k := 1; k < len(got); k++ {
		// Resampled unsmoothed, a step drops 0.5 over no width.
		if dx, dy := got[k].x-got[k-1].x, got[k].y-got[k-1].y; dy > 0.5*math.Abs(dx)+1e-9 {
			t.Errorf("steps: drops %v over %v at %v, want a gentle curve", dy, dx, got[k])
		}
	}
}

// TestPaintMask checks that paintMask blends exactly as [draw.DrawMask] does,
// over pixels of every opacity, into a piece of a larger image, with masks
// partly or wholly off the piece, and leaves the rest of the image alone.
func TestPaintMask(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	byteOf := func() uint8 {
		// Favor the ends, where blending takes its special cases.
		switch rng.IntN(4) {
		case 0:
			return 0
		case 1:
			return 0xff
		}
		return uint8(rng.IntN(0x100))
	}
	for range 200 {
		got := image.NewNRGBA(image.Rect(-3, -2, 12, 10))
		for i := range got.Pix {
			got.Pix[i] = byteOf()
		}
		want := image.NewNRGBA(got.Rect)
		copy(want.Pix, got.Pix)
		piece := image.Rect(1, 1, 9, 7)
		at := image.Pt(rng.IntN(16)-8, rng.IntN(14)-7)
		mask := image.NewAlpha(image.Rect(0, 0, 6, 5).Add(at))
		for i := range mask.Pix {
			mask.Pix[i] = byteOf()
		}
		c := color.NRGBA{byteOf(), byteOf(), byteOf(), byteOf()}
		draw.DrawMask(want.SubImage(piece).(*image.NRGBA), mask.Rect, image.NewUniform(c), image.Point{}, mask, mask.Rect.Min, draw.Over)
		paintMask(got.SubImage(piece).(*image.NRGBA), mask, c)
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("color %v, mask at %v: pixels %v, want %v", c, at, got.Pix, want.Pix)
		}
	}
}

// TestMouseTrailMask checks that a trail's mask is drawn again for another
// end or view, and not for another alpha.
func TestMouseTrailMask(t *testing.T) {
	t.Parallel()
	s := &mouseStage{glide: 100 * ms, ripple: 400 * ms, unit: 20}
	s.add(0, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 0, Y: 0})
	s.add(10*ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 5, Y: 0})
	s.add(20*ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 5, Y: 3})
	tr := trailLook{first: 0, last: 2, end: vec{5.5, 2}, alpha: 0xff}
	view, grid := cellView{w: 10, h: 20}, image.Rect(0, 0, 100, 100)
	mask := s.trailMask(tr, view, grid)
	tr.alpha = 0x40
	if s.trailMask(tr, view, grid) != mask {
		t.Error("another alpha: drawn again, want the same mask")
	}
	tr.end = vec{5.5, 3.5}
	longer := s.trailMask(tr, view, grid)
	if longer == mask || longer.Rect.Max.Y <= mask.Rect.Max.Y {
		t.Errorf("another end: mask %v, want a new one past %v", longer.Rect, mask.Rect)
	}
	if zoomed := s.trailMask(tr, cellView{w: 20, h: 40}, grid); zoomed == longer || zoomed.Rect == longer.Rect {
		t.Errorf("another view: mask %v, want a new one, not %v", zoomed.Rect, longer.Rect)
	}
}

// TestMouseRaw checks single mouse events: motion with no button held moves
// the pointer, a right press shows the orange ripple and keeps the disc
// under the pointer until its release, and a wheel turn only moves it.
func TestMouseRaw(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 16, 3, "",
		output(0, mouseScreen),
		input(100, sgrMouse(sgrHover, 1, 0, false)),
		input(300, sgrMouse(sgrHover, 8, 1, false)),
		input(600, sgrMouse(sgrRight, 8, 1, false)),
		input(1200, sgrMouse(sgrRight, 8, 1, true)),
		input(1500, sgrMouse(64, 12, 2, false)),
		output(5000, " "),
	)
	in, out := mouseFrames(t, tl, Mouse(WithPointerStyle(PointerDot), WithPointerGlide(100*ms)))
	assertMouseFrames(t, in, out, 2750*ms)
	at := func(d time.Duration) reel.Frame {
		i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At >= d })
		return out[i]
	}
	assertGolden(t, "mouse-raw", []reel.Frame{
		at(100 * ms), at(350 * ms), at(400 * ms), at(650 * ms), at(1000 * ms), at(1250 * ms), at(1550 * ms), at(1650 * ms),
	})
}

// TestMouseZoom checks that the pointer and ripple are drawn on the cell a
// [Zoom] before Mouse shows, as it eases out, and that a pointer the zoom
// puts outside the grid is left out.
func TestMouseZoom(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 16, 3, "",
		output(0, mouseScreen),
		input(100, sgrMouse(sgrLeft, 8, 1, false)+sgrMouse(sgrLeft, 8, 1, true)),
		input(300, sgrMouse(sgrHover, 0, 0, false)),
		input(400, sgrMouse(sgrHover, 9, 1, false)),
		mark(600, "out"),
		output(2000, " "),
	)
	zoom := Zoom(Cells(image.Rect(7, 1, 13, 2)), Scale(2), Between("", "out"), EaseOut(150*ms))
	_, out := mouseFrames(t, tl, zoom, Mouse())
	at := func(d time.Duration) reel.Frame {
		i := slices.IndexFunc(out, func(f reel.Frame) bool { return f.At >= d })
		return out[i]
	}
	assertGolden(t, "mouse-zoom", []reel.Frame{
		at(150 * ms), at(350 * ms), at(550 * ms), at(650 * ms), at(700 * ms), at(800 * ms),
	})
}

// TestMouseNothing checks that frames are left alone without mouse input,
// and when no part of the mouse is drawn.
func TestMouseNothing(t *testing.T) {
	t.Parallel()
	clicked := []event{output(0, mouseScreen), input(100, sgrMouse(sgrLeft, 3, 1, false)), output(500, " ")}
	for _, tc := range []struct {
		name   string
		events []event
		effect reel.Effect
	}{
		{"no mouse input", []event{output(0, mouseScreen), input(100, "ab\r"), output(500, " ")}, Mouse()},
		{"no parts", clicked, Mouse(WithMouseParts(0))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, out := highlightFrames(t, buildTimeline(t, 16, 3, "", tc.events...), tc.effect)
			if len(out) != len(in) {
				t.Fatalf("%d frames, want %d", len(out), len(in))
			}
			for i := range out {
				if out[i].Image != in[i].Image || out[i].Delay != in[i].Delay {
					t.Fatalf("frame %d changed", i)
				}
			}
		})
	}
}

// TestMouseErrors checks the options Start rejects.
func TestMouseErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "", output(0, "a"))
	ctx := &reel.Context{Timeline: tl, Rasterizer: highlightRast, Layout: highlightRast.Layout(4, 1), Step: 10 * ms, Speed: 1}
	for name, e := range map[string]reel.Effect{
		"style":    Mouse(WithPointerStyle(PointerDot + 1)),
		"negative": Mouse(WithPointerStyle(-1)),
		"size":     Mouse(WithPointerSize(-1)),
	} {
		if _, err := e.Start(ctx); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := e.(reel.Describer).Describe(); err == nil {
			t.Errorf("%s: no error from Describe", name)
		}
	}
}

// TestMouseEvents checks what each mouse event leaves the mouse doing, and
// where the pointer glides from.
func TestMouseEvents(t *testing.T) {
	t.Parallel()
	s := &mouseStage{glide: 100 * ms, hide: time.Second, fade: 100 * ms, ripple: 400 * ms}
	for _, ev := range []struct {
		at time.Duration
		m  vt.MouseEvent
	}{
		{0, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseUnknown, X: 0, Y: 0}},
		{50 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 2, Y: 0}},
		{200 * ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 3, Y: 0}},
		{200 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseWheelUp, X: 3, Y: 0}},
		{300 * ms, vt.MouseEvent{Action: vt.MouseRelease, Button: vt.MouseLeft, X: 3, Y: 0}},
		{400 * ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseRight, X: 4, Y: 0}},
		{2 * time.Second, vt.MouseEvent{Action: vt.MouseRelease, Button: vt.MouseUnknown, X: 6, Y: 0}},
		{4 * time.Second, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseUnknown, X: 1, Y: 0}},
	} {
		s.add(ev.at, ev.m)
	}
	want := []struct {
		fromX float64
		held  vt.MouseButton
		drag  int
	}{
		{0.5, vt.MouseUnknown, -1},
		{0.5, vt.MouseLeft, 1},
		{2.5, vt.MouseLeft, 1},
		// The motion at the same offset had not started gliding.
		{2.5, vt.MouseLeft, 1},
		{3.5, vt.MouseUnknown, 1},
		// Motion with a button held drags without a press.
		{3.5, vt.MouseRight, 5},
		{4.5, vt.MouseUnknown, 5},
		// The pointer had hidden, so it shows at the cell.
		{1.5, vt.MouseUnknown, -1},
	}
	for i, w := range want {
		ev := s.events[i]
		if ev.from.x != w.fromX || ev.held != w.held || ev.drag != w.drag {
			t.Errorf("event %d: from %v, held %v, drag %d; want from %v, held %v, drag %d", i, ev.from.x, ev.held, ev.drag, w.fromX, w.held, w.drag)
		}
	}
}

// TestMousePressReached checks that the pointer reaches the cell of a press
// before the drag motion after it moves it on, and that a press where the
// pointer already is keeps nothing waiting.
func TestMousePressReached(t *testing.T) {
	t.Parallel()
	s := &mouseStage{glide: 150 * ms, hide: time.Second, fade: 100 * ms, ripple: 400 * ms}
	for _, ev := range []struct {
		at time.Duration
		m  vt.MouseEvent
	}{
		{0, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseUnknown, X: 0, Y: 0}},
		{500 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 4, Y: 0}},
		{540 * ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 5, Y: 0}},
		{580 * ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 6, Y: 0}},
		{700 * ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 7, Y: 0}},
		{1500 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 7, Y: 0}},
		{1520 * ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 8, Y: 0}},
		// Presses quicker than the pointer reaches them wait a glide at most.
		{2000 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseRight, X: 0, Y: 3}},
		{2010 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseRight, X: 4, Y: 3}},
		{2020 * ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseRight, X: 8, Y: 3}},
	} {
		s.add(ev.at, ev.m)
	}
	// pos is where the pointer is at t, from the last event by then.
	pos := func(t time.Duration) float64 {
		return s.pos(sort.Search(len(s.events), func(i int) bool { return s.events[i].at > t })-1, t).x
	}
	if x := pos(560 * ms); x <= 0.5 || x >= 4.5 {
		t.Errorf("at 560ms, gliding to the press, x = %v, want between 0.5 and 4.5", x)
	}
	if x := pos(650 * ms); x != 4.5 {
		t.Errorf("at 650ms, once the glide to the press ends, x = %v, want 4.5", x)
	}
	if x := pos(675 * ms); x <= 4.5 || x >= 6.5 {
		t.Errorf("at 675ms, gliding on from the press, x = %v, want between 4.5 and 6.5", x)
	}
	if x := pos(850 * ms); x != 7.5 {
		t.Errorf("at 850ms, x = %v, want 7.5", x)
	}
	// The trail waits for the pointer too.
	if tr := s.trail(3, 600*ms); tr.alpha != 0 {
		t.Errorf("trail at 600ms, before the pointer reaches the press: %+v, want none", tr)
	}
	if tr := s.trail(3, 700*ms); tr.alpha == 0 {
		t.Error("no trail at 700ms, once the pointer left the press")
	}
	for i, want := range []time.Duration{0, 500 * ms, 650 * ms, 650 * ms, 700 * ms, 1500 * ms, 1520 * ms, 2000 * ms, 2150 * ms, 2170 * ms} {
		if got := s.events[i].start; got != want {
			t.Errorf("event %d starts gliding at %v, want %v", i, got, want)
		}
	}
}

// TestMouseLook checks what the mouse shows over a drag: the ripple of the
// press, the held button, the trail while it is held and as it fades, and
// the pointer hiding.
func TestMouseLook(t *testing.T) {
	t.Parallel()
	s := &mouseStage{parts: MouseAll, glide: 100 * ms, hide: time.Second, fade: 100 * ms, ripple: 400 * ms}
	s.add(0, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 1, Y: 1})
	s.add(100*ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseLeft, X: 2, Y: 1})
	s.add(200*ms, vt.MouseEvent{Action: vt.MouseRelease, Button: vt.MouseLeft, X: 2, Y: 1})
	ripples := func(l mouseLook) int {
		return slices.IndexFunc(l.ripples[:], func(r rippleLook) bool { return r.button == vt.MouseUnknown })
	}
	for _, tc := range []struct {
		t       time.Duration
		x       float64
		alpha   uint8
		held    vt.MouseButton
		ripples int
		trail   trailLook
	}{
		{0, 1.5, 0xff, vt.MouseLeft, 1, trailLook{}},
		{150 * ms, 2, 0xff, vt.MouseLeft, 1, trailLook{first: 0, last: 1, end: vec{2, 1.5}, alpha: 0xff}},
		{300 * ms, 2.5, 0xff, vt.MouseUnknown, 1, trailLook{first: 0, last: 2, end: vec{2.5, 1.5}, alpha: 0xd7}},
		{500 * ms, 2.5, 0xff, vt.MouseUnknown, 0, trailLook{first: 0, last: 2, end: vec{2.5, 1.5}, alpha: 0x28}},
		{1250 * ms, 2.5, 0x80, vt.MouseUnknown, 0, trailLook{}},
	} {
		l := s.look(tc.t)
		if l.pos.x != tc.x || l.alpha != tc.alpha || l.held != tc.held || ripples(l) != tc.ripples || l.trail != tc.trail {
			t.Errorf("at %v: pos %v, alpha %#x, held %v, %d ripples, trail %+v; want pos %v, alpha %#x, held %v, %d ripples, trail %+v",
				tc.t, l.pos.x, l.alpha, l.held, ripples(l), l.trail, tc.x, tc.alpha, tc.held, tc.ripples, tc.trail)
		}
	}
	if l := s.look(1400 * ms); l != (mouseLook{}) {
		t.Errorf("after hiding, look %+v, want none", l)
	}
	if l := s.look(-ms); l != (mouseLook{}) {
		t.Errorf("before the first event, look %+v, want none", l)
	}
}

// TestMouseExtraButtons checks that the extra buttons, and the release of
// another button, leave a drag going on.
func TestMouseExtraButtons(t *testing.T) {
	t.Parallel()
	s := &mouseStage{glide: 100 * ms, hide: time.Second, fade: 100 * ms, ripple: 400 * ms}
	s.add(0, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseLeft, X: 1, Y: 1})
	s.add(10*ms, vt.MouseEvent{Action: vt.MousePress, Button: vt.MouseBackward, X: 2, Y: 1})
	s.add(20*ms, vt.MouseEvent{Action: vt.MouseMotion, Button: vt.MouseBackward, X: 3, Y: 1})
	s.add(30*ms, vt.MouseEvent{Action: vt.MouseRelease, Button: vt.MouseBackward, X: 3, Y: 1})
	s.add(40*ms, vt.MouseEvent{Action: vt.MouseRelease, Button: vt.MouseRight, X: 3, Y: 1})
	for i, ev := range s.events {
		if ev.held != vt.MouseLeft || ev.drag != 0 {
			t.Errorf("event %d: held %v, drag %d; want left held, drag 0", i, ev.held, ev.drag)
		}
	}
}

// TestMouseTrailPath checks where a trail ends: behind a pointer still on
// its way to the press, along the last leg as far as the pointer, and with
// the legs before it whole when the pointer cuts back across them.
func TestMouseTrailPath(t *testing.T) {
	t.Parallel()
	s := &mouseStage{glide: 100 * ms, ripple: 400 * ms}
	for i, c := range []image.Point{{0, 0}, {10, 0}, {10, 10}, {0, 0}} {
		action := vt.MouseMotion
		if i == 0 {
			action = vt.MousePress
		}
		s.add(time.Duration(i)*10*ms, vt.MouseEvent{Action: action, Button: vt.MouseLeft, X: c.X, Y: c.Y})
	}
	for _, tc := range []struct {
		name string
		tr   trailLook
		want []vec
	}{
		{"before the press", trailLook{first: 0, last: 1, end: vec{-3, 0.5}}, []vec{{0.5, 0.5}, {0.5, 0.5}}},
		{"along the leg", trailLook{first: 0, last: 1, end: vec{4.5, 1}}, []vec{{0.5, 0.5}, {4.5, 0.5}}},
		{"cutting back", trailLook{first: 0, last: 3, end: vec{5.5, 0.5}}, []vec{{0.5, 0.5}, {10.5, 0.5}, {10.5, 10.5}, {3, 3}}},
	} {
		if got := s.trailPath(tc.tr); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
