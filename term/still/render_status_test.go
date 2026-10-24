// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"bytes"
	"image"
	"image/color"
	"reflect"
	"testing"
	"time"

	"github.com/lrstanley/x/term/vt/screen"
)

var statusMargin = color.NRGBA{R: 0x15, G: 0x16, B: 0x1c, A: 0xff}

// statusRenderer returns a renderer with window chrome, a margin the outline
// fits in, and a clock that reads *at.
func statusRenderer(t *testing.T, at *time.Time, opts ...Option) *Renderer {
	t.Helper()
	base := []Option{
		WithWindowChrome(true), WithMargin(8, statusMargin), WithBorderRadius(8),
		WithClock(func() time.Time { return *at }),
	}
	return newRenderer(t, append(base, opts...)...)
}

// drawStatusImage draws scr with recs as a fresh full draw.
func drawStatusImage(t *testing.T, d *Renderer, scr screen.Screen, recs ...ProgramStatus) *image.NRGBA {
	t.Helper()
	d.UpdateEmulatorState(&EmulatorState{Focused: true, Title: "title", ProgramStatuses: recs})
	return drawIntoNRGBA(d, scr)
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

	at := time.Unix(1000, 0)
	scr := newTestScreen(20, 3)
	recs := []ProgramStatus{{State: ProgramBlocked, Title: "Allow?", Progress: -1}}
	for name, opts := range map[string][]Option{
		"no chrome": {WithWindowChrome(false)},
		"disabled":  {WithProgramStatus(false)},
	} {
		d := statusRenderer(t, &at, opts...)
		if !bytes.Equal(drawStatusImage(t, d, scr).Pix, drawStatusImage(t, d, scr, recs...).Pix) {
			t.Errorf("%s: program status drew something", name)
		}
		if d.IndicatorsAnimate() {
			t.Errorf("%s: IndicatorsAnimate() = true for a status that is not drawn", name)
		}
		if bands := d.layoutSnapshot(t, image.Point{}, scr).statusBands(); bands != [4]image.Rectangle{} {
			t.Errorf("%s: statusBands() = %v, want none", name, bands)
		}
	}
	d := statusRenderer(t, &at)
	if bytes.Equal(drawStatusImage(t, d, scr).Pix, drawStatusImage(t, d, scr, recs...).Pix) {
		t.Fatal("program status did not draw with window chrome")
	}
}

// TestProgramStatusStatesDiffer checks that every state, and working with and
// without progress, draws, and draws differently from the others.
func TestProgramStatusStatesDiffer(t *testing.T) {
	t.Parallel()

	at := time.Unix(1000, 0)
	scr := newTestScreen(24, 3)
	d := statusRenderer(t, &at)
	none := drawStatusImage(t, d, scr)
	seen := map[string]string{}
	for name, rec := range map[string]ProgramStatus{
		"idle":               {State: ProgramIdle, Progress: -1},
		"working":            {State: ProgramWorking, Progress: -1},
		"working 40%":        {State: ProgramWorking, Progress: 40},
		"blocked":            {State: ProgramBlocked, Progress: -1},
		"blocked permission": {State: ProgramBlocked, Kind: ProgramKindPermission, Progress: -1},
		"blocked question":   {State: ProgramBlocked, Kind: ProgramKindQuestion, Progress: -1},
		"blocked auth":       {State: ProgramBlocked, Kind: ProgramKindAuth, Progress: -1},
		"done":               {State: ProgramDone, Progress: -1},
		"error":              {State: ProgramError, Progress: -1},
	} {
		rec.Title = "job"
		img := drawStatusImage(t, d, scr, rec)
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
		if got, icon := statusLead(ProgramStatus{State: ProgramWorking, Progress: pct}, time.Time{}); got != want || icon {
			t.Errorf("statusLead(%d%%) = %q icon=%v, want %q", pct, got, icon, want)
		}
	}

	at := time.Unix(1000, 0)
	scr := newTestScreen(24, 3)
	d := statusRenderer(t, &at)
	f := d.layoutSnapshot(t, image.Point{}, scr)
	working := func(pct int) *image.NRGBA {
		return drawStatusImage(t, d, scr, ProgramStatus{State: ProgramWorking, Title: "build", Progress: pct})
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
	rec := func(id string, s ProgramState) ProgramStatus { return ProgramStatus{ID: id, State: s, Progress: -1} }
	for name, tc := range map[string]struct {
		in   []ProgramStatus
		drv  string
		dots []ProgramState
	}{
		"root only":       {[]ProgramStatus{rec("", ProgramIdle)}, "", nil},
		"child wins":      {[]ProgramStatus{rec("", ProgramIdle), rec("a", ProgramBlocked)}, "a", []ProgramState{ProgramIdle}},
		"root wins ties":  {[]ProgramStatus{rec("", ProgramError), rec("a", ProgramError)}, "", []ProgramState{ProgramError}},
		"then id order":   {[]ProgramStatus{rec("a", ProgramDone), rec("b", ProgramWorking), rec("c", ProgramWorking)}, "b", []ProgramState{ProgramWorking, ProgramDone}},
		"urgency order":   {[]ProgramStatus{rec("", ProgramIdle), rec("a", ProgramDone), rec("b", ProgramWorking), rec("c", ProgramBlocked), rec("d", ProgramError)}, "d", []ProgramState{ProgramBlocked, ProgramWorking, ProgramDone, ProgramIdle}},
		"unknown is idle": {[]ProgramStatus{rec("", ProgramState(99)), rec("a", ProgramIdle)}, "", []ProgramState{ProgramIdle}},
		"dots capped": {
			[]ProgramStatus{rec("", ProgramWorking), rec("1", ProgramIdle), rec("2", ProgramIdle), rec("3", ProgramIdle), rec("4", ProgramIdle), rec("5", ProgramIdle), rec("6", ProgramError), rec("7", ProgramIdle)},
			"6",
			[]ProgramState{ProgramWorking, ProgramIdle, ProgramIdle, ProgramIdle, ProgramIdle, ProgramIdle},
		},
	} {
		v := d.shownStatus(&EmulatorState{ProgramStatuses: tc.in}, time.Time{})
		if !v.shown || v.drv.ID != tc.drv || !reflect.DeepEqual(v.dots[:v.ndots], append([]ProgramState{}, tc.dots...)) {
			t.Errorf("%s: driver %q dots %v, want %q %v", name, v.drv.ID, v.dots[:v.ndots], tc.drv, tc.dots)
		}
	}
	if v := d.shownStatus(&EmulatorState{}, time.Time{}); v != (statusView{}) {
		t.Errorf("no records: %+v, want the zero view", v)
	}
}

// TestProgramStatusDots checks that each record other than the driver adds a
// dot to the title bar, up to statusMaxDots.
func TestProgramStatusDots(t *testing.T) {
	t.Parallel()

	at := time.Unix(1000, 0)
	scr := newTestScreen(40, 3)
	d := statusRenderer(t, &at)
	recs := []ProgramStatus{{State: ProgramError, Title: "lint", Progress: -1}}
	for i := range statusMaxDots + 3 {
		recs = append(recs, ProgramStatus{ID: string(rune('a' + i)), State: ProgramDone, Progress: -1})
	}
	bar := d.layoutSnapshot(t, image.Point{}, scr).chromeBounds
	titleBar := func(n int) []byte {
		return subPix(t, drawStatusImage(t, d, scr, recs[:n]...), bar)
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

	at := time.Unix(1000, 0)
	scr := newTestScreen(60, 3)
	d := statusRenderer(t, &at)
	for _, tc := range []struct{ hostile, clean ProgramStatus }{
		{
			ProgramStatus{State: ProgramBlocked, Title: "Run \u202egnp.exe\u202c now?\u200b", Message: "Open \u2066invoice\u2069\u200d.pdf\u00ad\ufeff ok", Progress: -1},
			ProgramStatus{State: ProgramBlocked, Title: "Run gnp.exe now?", Message: "Open invoice.pdf ok", Progress: -1},
		},
		{
			ProgramStatus{State: ProgramDone, Title: "a\x1b[31mred\x1b[0m\nnext", Progress: -1},
			ProgramStatus{State: ProgramDone, Title: "a [31mred [0m next", Progress: -1},
		},
		{
			ProgramStatus{State: ProgramIdle, App: "\u200b\u202eppa", Progress: -1},
			ProgramStatus{State: ProgramIdle, App: "ppa", Progress: -1},
		},
	} {
		if !bytes.Equal(drawStatusImage(t, d, scr, tc.hostile).Pix, drawStatusImage(t, d, scr, tc.clean).Pix) {
			t.Errorf("%+v draws differently from %+v", tc.hostile, tc.clean)
		}
	}
}

// TestProgramStatusClearsOnNextDraw checks the cached Draw path: records
// appearing on an unchanged screen repaint, clearing a child leaves exactly
// the remaining records, and clearing them all restores the plain frame.
func TestProgramStatusClearsOnNextDraw(t *testing.T) {
	t.Parallel()

	at := time.Unix(1000, 0)
	scr := screen.NewBuffer(24, 3)
	d := statusRenderer(t, &at)
	ref := statusRenderer(t, &at)
	draw := func(recs ...ProgramStatus) []byte {
		d.UpdateEmulatorState(&EmulatorState{Focused: true, Title: "title", ProgramStatuses: recs})
		return bytes.Clone(drawNRGBA(t, d, scr).Pix)
	}
	root := ProgramStatus{State: ProgramWorking, Title: "build", Progress: 30}
	child := ProgramStatus{ID: "test", State: ProgramError, Title: "test", Progress: -1}

	plain := draw()
	if both := draw(root, child); bytes.Equal(plain, both) {
		t.Fatal("records on an unchanged screen did not repaint")
	}
	if got := draw(root); !bytes.Equal(got, drawStatusImage(t, ref, scr, root).Pix) {
		t.Fatal("clearing a child left a different image than drawing the root alone")
	}
	if got := draw(); !bytes.Equal(got, plain) {
		t.Fatal("clearing every record did not restore the plain frame")
	}
}

// TestProgramStatusAnimates checks which states move with the clock, that
// they loop, that the frame cache does not freeze them, and that a draw
// depends only on the clock.
func TestProgramStatusAnimates(t *testing.T) {
	t.Parallel()

	scr := screen.NewBuffer(24, 3)
	for _, tc := range []struct {
		name   string
		rec    ProgramStatus
		period time.Duration
	}{
		{"working", ProgramStatus{State: ProgramWorking, Title: "w", Progress: -1}, statusOrbit},
		{"blocked", ProgramStatus{State: ProgramBlocked, Title: "b", Progress: -1}, statusBreath},
		{"blocked with progress", ProgramStatus{State: ProgramBlocked, Title: "b", Progress: 50}, statusBreath},
		{"working 50%", ProgramStatus{State: ProgramWorking, Title: "p", Progress: 50}, 0},
		{"done", ProgramStatus{State: ProgramDone, Title: "d", Progress: -1}, 0},
		{"error", ProgramStatus{State: ProgramError, Title: "e", Progress: -1}, 0},
		{"idle", ProgramStatus{State: ProgramIdle, Title: "i", Progress: -1}, 0},
	} {
		at := time.Unix(1000, 0).Add(300 * time.Millisecond)
		d, other := statusRenderer(t, &at), statusRenderer(t, &at)
		state := &EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{tc.rec}}
		d.UpdateEmulatorState(state)
		other.UpdateEmulatorState(state)
		if got := d.IndicatorsAnimate(); got != (tc.period > 0) {
			t.Errorf("%s: IndicatorsAnimate() = %v", tc.name, got)
		}
		first := bytes.Clone(drawNRGBA(t, d, scr).Pix)
		if !bytes.Equal(first, drawNRGBA(t, other, scr).Pix) {
			t.Errorf("%s: two renderers at the same time drew differently", tc.name)
		}

		at = at.Add(700 * time.Millisecond)
		moved := !bytes.Equal(first, drawNRGBA(t, d, scr).Pix)
		if moved != (tc.period > 0) {
			t.Errorf("%s: moved with the clock = %v", tc.name, moved)
		}
		if tc.period > 0 {
			at = at.Add(tc.period - 700*time.Millisecond)
			if !bytes.Equal(first, drawNRGBA(t, d, scr).Pix) {
				t.Errorf("%s: does not loop every %v", tc.name, tc.period)
			}
		}
	}
}

// TestProgramStatusBreathSteps checks that the blocked outline breathes in a
// few steps, so held GIF frames repeat and merge.
func TestProgramStatusBreathSteps(t *testing.T) {
	t.Parallel()

	at := time.Unix(1000, 0)
	scr := screen.NewBuffer(12, 2)
	d := statusRenderer(t, &at)
	d.UpdateEmulatorState(&EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{{State: ProgramBlocked, Progress: -1}}})
	seen := map[string]bool{}
	for range int(statusBreath / (50 * time.Millisecond)) {
		seen[string(drawNRGBA(t, d, scr).Pix)] = true
		at = at.Add(50 * time.Millisecond)
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

	at := time.Unix(1000, 0)
	scr := newTestScreen(20, 3)
	done := ProgramStatus{State: ProgramDone, Progress: -1}
	changed := func(d *Renderer, p image.Point) bool {
		return drawStatusImage(t, d, scr).NRGBAAt(p.X, p.Y) != drawStatusImage(t, d, scr, done).NRGBAAt(p.X, p.Y)
	}

	d := statusRenderer(t, &at)
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
		inset := statusRenderer(t, &at, opts...)
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
	d.UpdateEmulatorState(&EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{done}})
	focused := drawNRGBA(t, d, scr).NRGBAAt(p.X, p.Y)
	d.UpdateEmulatorState(&EmulatorState{ProgramStatuses: []ProgramStatus{done}})
	unfocused := drawNRGBA(t, d, scr).NRGBAAt(p.X, p.Y)
	if int(unfocused.G) >= int(focused.G) {
		t.Errorf("unfocused outline %v is not dimmer than focused %v", unfocused, focused)
	}
}

// TestProgramStatusColors checks that the outline uses the palette: the
// progress colors for working, blocked, and error, and ProgramDone for done.
func TestProgramStatusColors(t *testing.T) {
	t.Parallel()

	at := time.Unix(1000, 0).Add(statusBreath / 2) // a blocked outline at its brightest
	progress := color.NRGBA{R: 10, G: 200, B: 30, A: 255}
	pause := color.NRGBA{R: 200, G: 10, B: 200, A: 255}
	failed := color.NRGBA{R: 250, G: 250, B: 10, A: 255}
	done := color.NRGBA{R: 30, G: 30, B: 250, A: 255}
	pal := Palette{Progress: progress, ProgressPause: pause, ProgressError: failed, ProgramDone: done}
	scr := newTestScreen(20, 3)
	d := statusRenderer(t, &at, WithPalette(pal))
	win := d.layoutSnapshot(t, image.Point{}, scr).windowBounds
	p := image.Pt(win.Max.X, win.Min.Y+win.Dy()/2)
	for _, tc := range []struct {
		rec  ProgramStatus
		want color.NRGBA
	}{
		{ProgramStatus{State: ProgramWorking, Progress: 100}, progress},
		{ProgramStatus{State: ProgramBlocked, Progress: -1}, pause},
		{ProgramStatus{State: ProgramError, Progress: -1}, failed},
		{ProgramStatus{State: ProgramDone, Progress: -1}, done},
	} {
		got := drawStatusImage(t, d, scr, tc.rec).NRGBAAt(p.X, p.Y)
		if dist := colorDist(got, tc.want); dist > 160 {
			t.Errorf("state %v: outline %v, want near %v", tc.rec.State, got, tc.want)
		}
	}
}

// TestRepaintIndicatorsMatchesFullDraw checks that repainting the indicators
// of a copy of an earlier image gives exactly the pixels of drawing everything
// again at the later time, for each outline placement and with a progress bar
// as well.
func TestRepaintIndicatorsMatchesFullDraw(t *testing.T) {
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
					scr.SetCell(x, y, &screen.Cell{Content: string(r), Width: 1})
				}
			}
			for _, recs := range [][]ProgramStatus{
				{{State: ProgramWorking, Title: "work", Message: "msg", Progress: -1}},
				{{State: ProgramBlocked, Kind: ProgramKindQuestion, Title: "ask", Progress: -1}, {ID: "x", State: ProgramDone, Progress: -1}},
			} {
				at := time.Unix(1000, 0).Add(130 * time.Millisecond)
				d := statusRenderer(t, &at, opts...)
				state := EmulatorState{Title: "t", ScrollbackCount: 40, ProgramStatuses: recs, Progress: Progress{State: ProgressIndeterminate, Percent: -1}}
				d.UpdateEmulatorState(&state)
				base := drawIntoNRGBA(d, scr)
				at = at.Add(statusBreath / 3)
				want := drawIntoNRGBA(d, scr)

				got := image.NewNRGBA(base.Rect)
				copy(got.Pix, base.Pix)
				d.RepaintIndicators(got, scr)
				if bytes.Equal(got.Pix, base.Pix) {
					t.Fatalf("%v: repainting at a later time changed nothing", recs[0].State)
				}
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("%v: repainted image differs from a full draw at the same time", recs[0].State)
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

	at := time.Unix(1000, 0)
	scr := screen.NewBuffer(20, 6)
	d := statusRenderer(t, &at)
	d.UpdateEmulatorState(&EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{{State: ProgramDone, Progress: -1}}})
	img := drawNRGBA(t, d, scr)

	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	f := d.layoutSnapshot(t, image.Point{}, scr)
	mid := f.cellBounds(10, 3).Min
	img.SetNRGBA(mid.X, mid.Y, poison)

	d.UpdateEmulatorState(&EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{{State: ProgramError, Progress: -1}}})
	got := drawNRGBA(t, d, scr)
	if got.NRGBAAt(mid.X, mid.Y) != poison {
		t.Fatal("a status change repainted the middle of the grid")
	}
	win := f.windowBounds
	if c := got.NRGBAAt(win.Max.X, win.Min.Y+win.Dy()/2); colorDist(c, defaultProgressError) > 24 {
		t.Fatalf("a status change did not repaint the outline: %v", c)
	}
}

// TestEmulatorStateEqual checks that Equal notices a change to every field,
// so a field added later cannot be missed.
func TestEmulatorStateEqual(t *testing.T) {
	t.Parallel()

	base := EmulatorState{ProgramStatuses: []ProgramStatus{{Title: "a"}}}
	if !base.Equal(EmulatorState{ProgramStatuses: []ProgramStatus{{Title: "a"}}}) {
		t.Fatal("equal states are not Equal")
	}
	typ := reflect.TypeFor[EmulatorState]()
	for i := range typ.NumField() {
		other := base
		other.ProgramStatuses = []ProgramStatus{{Title: "a"}}
		v := reflect.ValueOf(&other).Elem().Field(i)
		switch v.Kind() { //nolint:exhaustive // EmulatorState only uses these kinds.
		case reflect.String:
			v.SetString("x")
		case reflect.Bool:
			v.SetBool(true)
		case reflect.Int:
			v.SetInt(1)
		case reflect.Uint8:
			v.SetUint(1)
		case reflect.Interface:
			v.Set(reflect.ValueOf(color.NRGBA{R: 1}))
		case reflect.Struct:
			v.Field(0).SetUint(1)
		case reflect.Slice:
			v.Set(reflect.ValueOf([]ProgramStatus{{Title: "b"}}))
		default:
			t.Fatalf("field %s has kind %v the test does not set", typ.Field(i).Name, v.Kind())
		}
		if base.Equal(other) {
			t.Errorf("Equal ignores %s", typ.Field(i).Name)
		}
	}
}

// TestRepaintIndicatorsProgressOnly checks that with window chrome and
// program status enabled but no records, repainting an animated progress bar
// touches only the bar's rows, not the title bar or the edge strips the
// outline would use.
func TestRepaintIndicatorsProgressOnly(t *testing.T) {
	t.Parallel()

	at := time.Unix(1000, 0)
	scr := newTestScreen(24, 4)
	d := statusRenderer(t, &at)
	d.UpdateEmulatorState(&EmulatorState{Focused: true, Title: "t", Progress: Progress{State: ProgressIndeterminate, Percent: -1}})
	base := drawIntoNRGBA(d, scr)
	at = at.Add(300 * time.Millisecond)
	want := drawIntoNRGBA(d, scr)

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
	d.RepaintIndicators(got, scr)
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
// step changes, while it still reports that it moves with the clock.
func TestProgramStatusBlockedReusesFrame(t *testing.T) {
	t.Parallel()

	start := time.Unix(1000, 0)
	// Two times with the same breath step, and a later one with another.
	same := start.Add(time.Millisecond)
	for breathStep(same) != breathStep(start) {
		start = start.Add(10 * time.Millisecond)
		same = start.Add(time.Millisecond)
	}
	next := same
	for breathStep(next) == breathStep(start) {
		next = next.Add(10 * time.Millisecond)
	}

	at := start
	scr := screen.NewBuffer(20, 3)
	d := statusRenderer(t, &at)
	d.UpdateEmulatorState(&EmulatorState{Focused: true, ProgramStatuses: []ProgramStatus{{State: ProgramBlocked, Title: "ask", Progress: -1}}})
	if !d.IndicatorsAnimate() {
		t.Fatal("IndicatorsAnimate() = false for a blocked status")
	}
	img := drawNRGBA(t, d, scr)
	win := d.layoutSnapshot(t, image.Point{}, scr).windowBounds
	p := image.Pt(win.Max.X, win.Min.Y+win.Dy()/2) // on the outline
	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	img.SetNRGBA(p.X, p.Y, poison)

	at = same
	if got := drawNRGBA(t, d, scr).NRGBAAt(p.X, p.Y); got != poison {
		t.Fatalf("the same breath step repainted the outline: %v", got)
	}
	at = next
	if got := drawNRGBA(t, d, scr).NRGBAAt(p.X, p.Y); got == poison {
		t.Fatal("a new breath step did not repaint the outline")
	}
}
