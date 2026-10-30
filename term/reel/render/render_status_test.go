// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"reflect"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
)

var statusMargin = color.NRGBA{R: 0x15, G: 0x16, B: 0x1c, A: 0xff}

// statusAt is the offset most status tests draw at.
const statusAt = 1000 * time.Second

// statusRenderer returns a renderer with window chrome and a margin the
// outline fits in.
func statusRenderer(t *testing.T, opts ...Option) *Renderer {
	t.Helper()
	base := []Option{WithWindowChrome(true), WithMargin(8, statusMargin), WithBorderRadius(8)}
	return newRenderer(t, append(base, opts...)...)
}

// statusState is a focused state with a title and recs.
func statusState(recs ...vt.ProgramStatus) vt.State {
	return vt.State{Focused: true, Title: "title", ProgramStatuses: recs}
}

// drawStatusImage draws scr with recs as a fresh full draw at at.
func drawStatusImage(d *Renderer, scr vt.Screen, at time.Duration, recs ...vt.ProgramStatus) *image.NRGBA {
	return drawFresh(d, scr, statusState(recs...), at)
}

// subPix returns the pixels of img within r.
func subPix(t *testing.T, img *image.NRGBA, r image.Rectangle) []byte {
	t.Helper()
	sub, ok := img.SubImage(r).(*image.NRGBA)
	if !ok {
		t.Fatalf("SubImage is %T", img.SubImage(r))
	}
	return bytes.Clone(sub.Pix)
}

func colorDist(a, b color.NRGBA) int {
	d := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B) + d(a.A, b.A)
}

// TestProgramStatusNeedsChrome checks that program status draws nothing
// without window chrome or with WithProgramStatus(false), and draws with it.
func TestProgramStatusNeedsChrome(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 3)
	recs := []vt.ProgramStatus{{State: vt.ProgramBlocked, Title: "Allow?", Progress: -1}}
	for name, opts := range map[string][]Option{
		"no chrome": {WithWindowChrome(false)},
		"disabled":  {WithProgramStatus(false)},
	} {
		d := statusRenderer(t, opts...)
		if !bytes.Equal(drawStatusImage(d, scr, statusAt).Pix, drawStatusImage(d, scr, statusAt, recs...).Pix) {
			t.Errorf("%s: program status drew something", name)
		}
		if d.Animates(statusState(recs...), 0) {
			t.Errorf("%s: Animates() = true for a status that is not drawn", name)
		}
		if bands := d.layoutSnapshot(t, image.Point{}, scr).statusBands(); bands != [4]image.Rectangle{} {
			t.Errorf("%s: statusBands() = %v, want none", name, bands)
		}
	}
	d := statusRenderer(t)
	if bytes.Equal(drawStatusImage(d, scr, statusAt).Pix, drawStatusImage(d, scr, statusAt, recs...).Pix) {
		t.Fatal("program status did not draw with window chrome")
	}
}

// TestProgramStatusStatesDiffer checks that every state, and working with and
// without progress, draws, and draws differently from the others.
func TestProgramStatusStatesDiffer(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(24, 3)
	d := statusRenderer(t)
	none := drawStatusImage(d, scr, statusAt)
	seen := map[string]string{}
	for name, rec := range map[string]vt.ProgramStatus{
		"idle":               {State: vt.ProgramIdle, Progress: -1},
		"working":            {State: vt.ProgramWorking, Progress: -1},
		"working 40%":        {State: vt.ProgramWorking, Progress: 40},
		"blocked":            {State: vt.ProgramBlocked, Progress: -1},
		"blocked permission": {State: vt.ProgramBlocked, Kind: vt.ProgramKindPermission, Progress: -1},
		"blocked question":   {State: vt.ProgramBlocked, Kind: vt.ProgramKindQuestion, Progress: -1},
		"blocked auth":       {State: vt.ProgramBlocked, Kind: vt.ProgramKindAuth, Progress: -1},
		"done":               {State: vt.ProgramDone, Progress: -1},
		"error":              {State: vt.ProgramError, Progress: -1},
	} {
		rec.Title = "job"
		img := drawStatusImage(d, scr, statusAt, rec)
		if bytes.Equal(img.Pix, none.Pix) {
			t.Errorf("%s drew nothing", name)
		}
		if prev, ok := seen[string(img.Pix)]; ok {
			t.Errorf("%s draws the same as %s", name, prev)
		}
		seen[string(img.Pix)] = name
	}
}

// TestProgramStatusWorkingPercent checks that a working record at a known
// progress shows that percent in the title bar, with the same width at every
// value, and fills the outline clockwise from the top center to it.
func TestProgramStatusWorkingPercent(t *testing.T) {
	t.Parallel()

	for pct, want := range map[int]string{0: "  0%", 7: "  7%", 42: " 42%", 100: "100%", 250: "100%"} {
		if got, icon := statusLead(vt.ProgramStatus{State: vt.ProgramWorking, Progress: pct}, 0); got != want || icon {
			t.Errorf("statusLead(%d%%) = %q icon=%v, want %q", pct, got, icon, want)
		}
	}

	scr := newTestScreen(24, 3)
	d := statusRenderer(t)
	f := d.layoutSnapshot(t, image.Point{}, scr)
	working := func(pct int) *image.NRGBA {
		return drawStatusImage(d, scr, statusAt, vt.ProgramStatus{State: vt.ProgramWorking, Title: "build", Progress: pct})
	}
	a, b := working(42), working(43)
	bar := f.chromeBounds
	if bytes.Equal(subPix(t, a, bar), subPix(t, b, bar)) {
		t.Error("42% and 43% draw the same title bar")
	}

	// The bottom center is halfway round the outline from the top center.
	win := f.windowBounds
	bottom := image.Pt(win.Min.X+win.Dx()/2, win.Max.Y)
	topRight := image.Pt(win.Min.X+win.Dx()/2+4, win.Min.Y-1)
	under, over := working(40), working(60)
	if d40, d60 := colorDist(under.NRGBAAt(bottom.X, bottom.Y), statusMargin), colorDist(over.NRGBAAt(bottom.X, bottom.Y), statusMargin); d60 <= 2*d40 {
		t.Errorf("bottom of the outline at 40%% is %d from the margin and at 60%% %d, want 60%% to have filled it", d40, d60)
	}
	if got := under.NRGBAAt(topRight.X, topRight.Y); colorDist(got, defaultProgress) > 24 {
		t.Errorf("outline just past the top center at 40%% = %v, want the progress color %v", got, defaultProgress)
	}
}

func TestShownStatusMostUrgent(t *testing.T) {
	t.Parallel()

	d := newRenderer(t, WithWindowChrome(true))
	rec := func(id string, s vt.ProgramState) vt.ProgramStatus {
		return vt.ProgramStatus{ID: id, State: s, Progress: -1}
	}
	for name, tc := range map[string]struct {
		in   []vt.ProgramStatus
		drv  string
		dots []vt.ProgramState
	}{
		"root only":       {[]vt.ProgramStatus{rec("", vt.ProgramIdle)}, "", nil},
		"child wins":      {[]vt.ProgramStatus{rec("", vt.ProgramIdle), rec("a", vt.ProgramBlocked)}, "a", []vt.ProgramState{vt.ProgramIdle}},
		"root wins ties":  {[]vt.ProgramStatus{rec("", vt.ProgramError), rec("a", vt.ProgramError)}, "", []vt.ProgramState{vt.ProgramError}},
		"then id order":   {[]vt.ProgramStatus{rec("a", vt.ProgramDone), rec("b", vt.ProgramWorking), rec("c", vt.ProgramWorking)}, "b", []vt.ProgramState{vt.ProgramWorking, vt.ProgramDone}},
		"urgency order":   {[]vt.ProgramStatus{rec("", vt.ProgramIdle), rec("a", vt.ProgramDone), rec("b", vt.ProgramWorking), rec("c", vt.ProgramBlocked), rec("d", vt.ProgramError)}, "d", []vt.ProgramState{vt.ProgramBlocked, vt.ProgramWorking, vt.ProgramDone, vt.ProgramIdle}},
		"unknown is idle": {[]vt.ProgramStatus{rec("", vt.ProgramState(99)), rec("a", vt.ProgramIdle)}, "", []vt.ProgramState{vt.ProgramIdle}},
		"dots capped": {
			[]vt.ProgramStatus{rec("", vt.ProgramWorking), rec("1", vt.ProgramIdle), rec("2", vt.ProgramIdle), rec("3", vt.ProgramIdle), rec("4", vt.ProgramIdle), rec("5", vt.ProgramIdle), rec("6", vt.ProgramError), rec("7", vt.ProgramIdle)},
			"6",
			[]vt.ProgramState{vt.ProgramWorking, vt.ProgramIdle, vt.ProgramIdle, vt.ProgramIdle, vt.ProgramIdle, vt.ProgramIdle},
		},
	} {
		v := d.shownStatus(vt.State{ProgramStatuses: tc.in}, 0)
		if !v.shown || v.drv.ID != tc.drv || !reflect.DeepEqual(v.dots[:v.ndots], append([]vt.ProgramState{}, tc.dots...)) {
			t.Errorf("%s: driver %q dots %v, want %q %v", name, v.drv.ID, v.dots[:v.ndots], tc.drv, tc.dots)
		}
	}
	if v := d.shownStatus(vt.State{}, 0); v != (statusView{}) {
		t.Errorf("no records: %+v, want the zero view", v)
	}
}

// TestProgramStatusDots checks that each record other than the driver adds a
// dot to the title bar, up to statusMaxDots.
func TestProgramStatusDots(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(40, 3)
	d := statusRenderer(t)
	recs := []vt.ProgramStatus{{State: vt.ProgramError, Title: "lint", Progress: -1}}
	for i := range statusMaxDots + 3 {
		recs = append(recs, vt.ProgramStatus{ID: string(rune('a' + i)), State: vt.ProgramDone, Progress: -1})
	}
	bar := d.layoutSnapshot(t, image.Point{}, scr).chromeBounds
	titleBar := func(n int) []byte {
		return subPix(t, drawStatusImage(d, scr, statusAt, recs[:n]...), bar)
	}
	for n := 1; n <= statusMaxDots; n++ {
		if bytes.Equal(titleBar(n), titleBar(n+1)) {
			t.Errorf("%d records draw the same title bar as %d", n+1, n)
		}
	}
	if !bytes.Equal(titleBar(statusMaxDots+1), titleBar(statusMaxDots+3)) {
		t.Errorf("more than %d dots were drawn", statusMaxDots)
	}
}

// TestProgramStatusHostileTextDrawsSafely checks that a title, message, or
// app holding bidi overrides, invisible characters, controls, or escapes
// draws exactly like the visible text, in the order it was sent.
func TestProgramStatusHostileTextDrawsSafely(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(60, 3)
	d := statusRenderer(t)
	for _, tc := range []struct{ hostile, clean vt.ProgramStatus }{
		{
			vt.ProgramStatus{State: vt.ProgramBlocked, Title: "Run \u202egnp.exe\u202c now?\u200b", Message: "Open \u2066invoice\u2069\u200d.pdf\u00ad\ufeff ok", Progress: -1},
			vt.ProgramStatus{State: vt.ProgramBlocked, Title: "Run gnp.exe now?", Message: "Open invoice.pdf ok", Progress: -1},
		},
		{
			vt.ProgramStatus{State: vt.ProgramDone, Title: "a\x1b[31mred\x1b[0m\nnext", Progress: -1},
			vt.ProgramStatus{State: vt.ProgramDone, Title: "a [31mred [0m next", Progress: -1},
		},
		{
			vt.ProgramStatus{State: vt.ProgramIdle, App: "\u200b\u202eppa", Progress: -1},
			vt.ProgramStatus{State: vt.ProgramIdle, App: "ppa", Progress: -1},
		},
	} {
		if !bytes.Equal(drawStatusImage(d, scr, statusAt, tc.hostile).Pix, drawStatusImage(d, scr, statusAt, tc.clean).Pix) {
			t.Errorf("%+v draws differently from %+v", tc.hostile, tc.clean)
		}
	}
}

// TestProgramStatusClearsOnNextDraw checks the cached Draw path: records
// appearing on an unchanged screen repaint, clearing a child leaves exactly
// the remaining records, and clearing them all restores the plain frame.
func TestProgramStatusClearsOnNextDraw(t *testing.T) {
	t.Parallel()

	scr := vt.NewBuffer(24, 3)
	d := statusRenderer(t)
	ref := statusRenderer(t)
	draw := func(recs ...vt.ProgramStatus) []byte {
		return drawAt(t, d, scr, statusState(recs...), statusAt).Pix
	}
	root := vt.ProgramStatus{State: vt.ProgramWorking, Title: "build", Progress: 30}
	child := vt.ProgramStatus{ID: "test", State: vt.ProgramError, Title: "test", Progress: -1}

	plain := draw()
	if both := draw(root, child); bytes.Equal(plain, both) {
		t.Fatal("records on an unchanged screen did not repaint")
	}
	if got := draw(root); !bytes.Equal(got, drawStatusImage(ref, scr, statusAt, root).Pix) {
		t.Fatal("clearing a child left a different image than drawing the root alone")
	}
	if got := draw(); !bytes.Equal(got, plain) {
		t.Fatal("clearing every record did not restore the plain frame")
	}
}

// TestProgramStatusAnimates checks which states move with the offset, that
// they loop, that the frame cache does not freeze them, and that a draw
// depends only on the offset.
func TestProgramStatusAnimates(t *testing.T) {
	t.Parallel()

	scr := vt.NewBuffer(24, 3)
	for _, tc := range []struct {
		name   string
		rec    vt.ProgramStatus
		period time.Duration
	}{
		{"working", vt.ProgramStatus{State: vt.ProgramWorking, Title: "w", Progress: -1}, statusOrbit},
		{"blocked", vt.ProgramStatus{State: vt.ProgramBlocked, Title: "b", Progress: -1}, statusBreath},
		{"blocked with progress", vt.ProgramStatus{State: vt.ProgramBlocked, Title: "b", Progress: 50}, statusBreath},
		{"working 50%", vt.ProgramStatus{State: vt.ProgramWorking, Title: "p", Progress: 50}, 0},
		{"done", vt.ProgramStatus{State: vt.ProgramDone, Title: "d", Progress: -1}, 0},
		{"error", vt.ProgramStatus{State: vt.ProgramError, Title: "e", Progress: -1}, 0},
		{"idle", vt.ProgramStatus{State: vt.ProgramIdle, Title: "i", Progress: -1}, 0},
	} {
		at := statusAt + 300*time.Millisecond
		d, other := statusRenderer(t), statusRenderer(t)
		st := vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{tc.rec}}
		if got := d.Animates(st, 0); got != (tc.period > 0) {
			t.Errorf("%s: Animates() = %v", tc.name, got)
		}
		first := drawAt(t, d, scr, st, at).Pix
		if !bytes.Equal(first, drawAt(t, other, scr, st, at).Pix) {
			t.Errorf("%s: two renderers at the same offset drew differently", tc.name)
		}

		at += 700 * time.Millisecond
		moved := !bytes.Equal(first, drawAt(t, d, scr, st, at).Pix)
		if moved != (tc.period > 0) {
			t.Errorf("%s: moved with the offset = %v", tc.name, moved)
		}
		if tc.period > 0 {
			at += tc.period - 700*time.Millisecond
			if !bytes.Equal(first, drawAt(t, d, scr, st, at).Pix) {
				t.Errorf("%s: does not loop every %v", tc.name, tc.period)
			}
		}
	}
}

// TestProgramStatusBreathSteps checks that the blocked outline breathes in a
// few steps, so held GIF frames repeat and merge.
func TestProgramStatusBreathSteps(t *testing.T) {
	t.Parallel()

	scr := vt.NewBuffer(12, 2)
	d := statusRenderer(t)
	st := vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramBlocked, Progress: -1}}}
	seen := map[string]bool{}
	for i := range int(statusBreath / (50 * time.Millisecond)) {
		seen[string(drawAt(t, d, scr, st, statusAt+time.Duration(i)*50*time.Millisecond).Pix)] = true
	}
	if len(seen) < 3 || len(seen) > statusBreathLevels+1 {
		t.Fatalf("one breath drew %d distinct frames, want 3..%d", len(seen), statusBreathLevels+1)
	}
}

// TestProgramStatusOutline checks where the outline goes: just outside the
// window, following the border radius, inset over the window edge when the
// margin is too small, and dimmed with an unfocused window.
func TestProgramStatusOutline(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 3)
	done := vt.ProgramStatus{State: vt.ProgramDone, Progress: -1}
	changed := func(d *Renderer, p image.Point) bool {
		return drawStatusImage(d, scr, statusAt).NRGBAAt(p.X, p.Y) != drawStatusImage(d, scr, statusAt, done).NRGBAAt(p.X, p.Y)
	}

	d := statusRenderer(t)
	f := d.layoutSnapshot(t, image.Point{}, scr)
	win := f.windowBounds
	if f.status.ring.inset() {
		t.Fatal("outline is inset with an 8px margin")
	}
	if !changed(d, image.Pt(win.Min.X+win.Dx()/2, win.Min.Y-1)) || !changed(d, image.Pt(win.Max.X, win.Min.Y+win.Dy()/2)) {
		t.Error("no outline just outside the window edge")
	}
	if changed(d, image.Pt(win.Min.X+win.Dx()/2, win.Min.Y+1)) {
		t.Error("the outline reached into the window")
	}
	if changed(d, image.Pt(win.Min.X, win.Min.Y)) || changed(d, image.Pt(win.Min.X-1, win.Min.Y-1)) {
		t.Error("the outline does not follow the rounded corner")
	}
	if changed(d, image.Pt(win.Min.X+win.Dx()/2, win.Min.Y-6)) {
		t.Error("the outline reached far into the margin")
	}

	for name, opts := range map[string][]Option{
		"no margin":    {WithMargin(0, nil), WithBorderRadius(0)},
		"thin margin":  {WithMargin(3, statusMargin), WithBorderRadius(10)},
		"transparent":  {WithMargin(0, nil), WithBorderRadius(10)},
		"with padding": {WithMargin(0, nil), WithPadding(6)},
	} {
		inset := statusRenderer(t, opts...)
		lf := inset.layoutSnapshot(t, image.Point{}, scr)
		iw := lf.windowBounds
		if !lf.status.ring.inset() {
			t.Errorf("%s: outline is not inset", name)
		}
		if !changed(inset, image.Pt(iw.Min.X+iw.Dx()/2, iw.Min.Y)) || !changed(inset, image.Pt(iw.Min.X, iw.Min.Y+iw.Dy()/2)) {
			t.Errorf("%s: inset outline does not cover the window edge", name)
		}
		if iw.Min.Y > 0 && changed(inset, image.Pt(iw.Min.X+iw.Dx()/2, iw.Min.Y-1)) {
			t.Errorf("%s: inset outline painted the margin", name)
		}
	}

	p := image.Pt(win.Min.X+win.Dx()/2, win.Min.Y-1)
	on := drawAt(t, d, scr, vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{done}}, statusAt).NRGBAAt(p.X, p.Y)
	off := drawAt(t, d, scr, vt.State{ProgramStatuses: []vt.ProgramStatus{done}}, statusAt).NRGBAAt(p.X, p.Y)
	if int(off.G) >= int(on.G) {
		t.Errorf("unfocused outline %v is not dimmer than focused %v", off, on)
	}
}

// TestProgramStatusColors checks that the outline uses the palette: the
// progress colors for working, blocked, and error, and ProgramDone for done.
func TestProgramStatusColors(t *testing.T) {
	t.Parallel()

	at := statusAt + statusBreath/2 // a blocked outline at its brightest
	progress := color.NRGBA{R: 10, G: 200, B: 30, A: 255}
	pause := color.NRGBA{R: 200, G: 10, B: 200, A: 255}
	failed := color.NRGBA{R: 250, G: 250, B: 10, A: 255}
	done := color.NRGBA{R: 30, G: 30, B: 250, A: 255}
	pal := Palette{Progress: progress, ProgressPause: pause, ProgressError: failed, ProgramDone: done}
	scr := newTestScreen(20, 3)
	d := statusRenderer(t, WithPalette(pal))
	win := d.layoutSnapshot(t, image.Point{}, scr).windowBounds
	p := image.Pt(win.Max.X, win.Min.Y+win.Dy()/2)
	for _, tc := range []struct {
		rec  vt.ProgramStatus
		want color.NRGBA
	}{
		{vt.ProgramStatus{State: vt.ProgramWorking, Progress: 100}, progress},
		{vt.ProgramStatus{State: vt.ProgramBlocked, Progress: -1}, pause},
		{vt.ProgramStatus{State: vt.ProgramError, Progress: -1}, failed},
		{vt.ProgramStatus{State: vt.ProgramDone, Progress: -1}, done},
	} {
		got := drawStatusImage(d, scr, at, tc.rec).NRGBAAt(p.X, p.Y)
		if dist := colorDist(got, tc.want); dist > 160 {
			t.Errorf("state %v: outline %v, want near %v", tc.rec.State, got, tc.want)
		}
	}
}

// TestRepaintMatchesFullDraw checks that repainting the indicators of a copy
// of an earlier image gives exactly the pixels of drawing everything again at
// the later offset, for each outline placement and with a progress bar as
// well.
func TestRepaintMatchesFullDraw(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string][]Option{
		"margin":      nil,
		"no margin":   {WithMargin(0, nil), WithBorderRadius(0)},
		"inset round": {WithMargin(2, color.NRGBA{R: 40, G: 60, B: 90, A: 255}), WithBorderRadius(12), WithPadding(3)},
		"no padding":  {WithMargin(5, nil), WithPadding(0), WithBorderRadius(30)},
		"everything":  {WithMargin(6, color.NRGBA{R: 30, A: 200}), WithPadding(3), WithBorderRadius(14), WithBackgroundOpacity(0.5), WithScrollbar(true), WithFocusDimming(0.4)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scr := newTestScreen(24, 4)
			for y := range 4 {
				for x, r := range "status repaint test 漢字 ab" {
					scr.SetCell(x, y, &vt.Cell{Content: string(r), Width: 1})
				}
			}
			for _, recs := range [][]vt.ProgramStatus{
				{{State: vt.ProgramWorking, Title: "work", Message: "msg", Progress: -1}},
				{{State: vt.ProgramBlocked, Kind: vt.ProgramKindQuestion, Title: "ask", Progress: -1}, {ID: "x", State: vt.ProgramDone, Progress: -1}},
			} {
				at := statusAt + 130*time.Millisecond
				d := statusRenderer(t, opts...)
				st := vt.State{Title: "t", ScrollbackLen: 40, ProgramStatuses: recs, Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}
				base := drawAt(t, d, scr, st, at)
				later := at + statusBreath/3
				want := drawFresh(d, scr, st, later)

				got := image.NewNRGBA(base.Rect)
				copy(got.Pix, base.Pix)
				d.Repaint(got, scr, st, at, later)
				if bytes.Equal(got.Pix, base.Pix) {
					t.Fatalf("%v: repainting at a later offset changed nothing", recs[0].State)
				}
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("%v: repainted image differs from a full draw at the same offset", recs[0].State)
				}
			}
		})
	}
}

// TestProgramStatusRepaintsOnlyBands checks that a status change on an
// unchanged screen repaints the title bar and the outline, and leaves the
// middle of the grid alone.
func TestProgramStatusRepaintsOnlyBands(t *testing.T) {
	t.Parallel()

	scr := vt.NewBuffer(20, 6)
	d := statusRenderer(t)
	drawAt(t, d, scr, vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramDone, Progress: -1}}}, statusAt)

	// Poison in the renderer's own frame shows whether Draw painted it again.
	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	f := d.layoutSnapshot(t, image.Point{}, scr)
	mid := f.cellBounds(10, 3).Min
	d.frame.SetNRGBA(mid.X, mid.Y, poison)

	got := drawAt(t, d, scr, vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramError, Progress: -1}}}, statusAt)
	if got.NRGBAAt(mid.X, mid.Y) != poison {
		t.Fatal("a status change repainted the middle of the grid")
	}
	win := f.windowBounds
	if c := got.NRGBAAt(win.Max.X, win.Min.Y+win.Dy()/2); colorDist(c, defaultProgressError) > 24 {
		t.Fatalf("a status change did not repaint the outline: %v", c)
	}
}

// TestRepaintProgressOnly checks that with window chrome and program status
// enabled but no records, repainting an animated progress bar touches only
// the bar's rows, not the title bar or the edge strips the outline would use.
func TestRepaintProgressOnly(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(24, 4)
	d := statusRenderer(t)
	st := vt.State{Focused: true, Title: "t", Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}
	base := drawFresh(d, scr, st, statusAt)
	later := statusAt + 300*time.Millisecond
	want := drawFresh(d, scr, st, later)

	band := d.layoutSnapshot(t, image.Point{}, scr).progressBand()
	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 4}
	got := image.NewNRGBA(base.Rect)
	copy(got.Pix, base.Pix)
	for y := got.Rect.Min.Y; y < got.Rect.Max.Y; y++ {
		for x := got.Rect.Min.X; x < got.Rect.Max.X; x++ {
			if !image.Pt(x, y).In(band) {
				got.SetNRGBA(x, y, poison)
			}
		}
	}
	d.Repaint(got, scr, st, statusAt, later)
	for y := got.Rect.Min.Y; y < got.Rect.Max.Y; y++ {
		for x := got.Rect.Min.X; x < got.Rect.Max.X; x++ {
			in := image.Pt(x, y).In(band)
			if c := got.NRGBAAt(x, y); (!in && c != poison) || (in && c != want.NRGBAAt(x, y)) {
				t.Fatalf("pixel (%d, %d) in bar band %v = %v: repainted outside the bar, or not as a full draw", x, y, in, c)
			}
		}
	}
}

// TestProgramStatusBlockedReusesFrame checks that a breathing blocked status
// reuses the cached frame while its breath step holds, and repaints when the
// step changes, while it still reports that it moves with the offset.
func TestProgramStatusBlockedReusesFrame(t *testing.T) {
	t.Parallel()

	start, same, next := breathOffsets()
	scr := vt.NewBuffer(20, 3)
	d := statusRenderer(t)
	st := vt.State{Focused: true, ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramBlocked, Title: "ask", Progress: -1}}}
	if !d.Animates(st, 0) {
		t.Fatal("Animates() = false for a blocked status")
	}
	drawAt(t, d, scr, st, start)
	win := d.layoutSnapshot(t, image.Point{}, scr).windowBounds
	p := image.Pt(win.Max.X, win.Min.Y+win.Dy()/2) // on the outline
	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	d.frame.SetNRGBA(p.X, p.Y, poison)

	if got := drawAt(t, d, scr, st, same).NRGBAAt(p.X, p.Y); got != poison {
		t.Fatalf("the same breath step repainted the outline: %v", got)
	}
	if got := drawAt(t, d, scr, st, next).NRGBAAt(p.X, p.Y); got == poison {
		t.Fatal("a new breath step did not repaint the outline")
	}
}

// breathOffsets returns two offsets with the same breath step, start and
// same, and a later one with another, next.
func breathOffsets() (start, same, next time.Duration) {
	start = statusAt
	same = start + time.Millisecond
	for breathStep(same) != breathStep(start) {
		start += 10 * time.Millisecond
		same = start + time.Millisecond
	}
	next = same
	for breathStep(next) == breathStep(start) {
		next += 10 * time.Millisecond
	}
	return start, same, next
}

// TestChanges checks that the indicators report a change between two offsets
// exactly when a draw at each could differ: always while they animate, on a
// new breath step for a blocked status, and never when they hold still.
// Where they report none, full draws at both offsets are the same.
func TestChanges(t *testing.T) {
	t.Parallel()

	start, same, next := breathOffsets()
	scr := vt.NewBuffer(20, 3)
	for _, tc := range []struct {
		name       string
		state      vt.State
		same, next bool
	}{
		{"blocked", vt.State{ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramBlocked, Title: "ask", Progress: -1}}}, false, true},
		{"working", vt.State{ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramWorking, Title: "w", Progress: -1}}}, true, true},
		{"working 50%", vt.State{ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramWorking, Title: "p", Progress: 50}}}, false, false},
		{"done", vt.State{ProgramStatuses: []vt.ProgramStatus{{State: vt.ProgramDone, Title: "d", Progress: -1}}}, false, false},
		{"indeterminate progress", vt.State{Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}, true, true},
		{"nothing", vt.State{}, false, false},
	} {
		d := statusRenderer(t)
		tc.state.Focused = true
		if got := d.Changes(tc.state, start, same); got != tc.same {
			t.Errorf("%s: Changes within a breath step = %v, want %v", tc.name, got, tc.same)
		}
		if got := d.Changes(tc.state, start, next); got != tc.next {
			t.Errorf("%s: Changes across breath steps = %v, want %v", tc.name, got, tc.next)
		}
		first := drawFresh(d, scr, tc.state, start).Pix
		for _, to := range []struct {
			at      time.Duration
			changes bool
		}{{same, tc.same}, {next, tc.next}} {
			if !to.changes && !bytes.Equal(first, drawFresh(d, scr, tc.state, to.at).Pix) {
				t.Errorf("%s: drew differently at %v with no change reported", tc.name, to.at-start)
			}
		}
	}
}
