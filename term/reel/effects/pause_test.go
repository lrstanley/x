// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"cmp"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/vt"
)

// badgeTimeline is a 40x10 terminal that changes twice and holds still to
// 400ms.
func badgeTimeline(t *testing.T) *reel.Timeline {
	t.Helper()
	return buildTimeline(t, 40, 10, "", output(0, "a"), output(100, "b"), output(200, "c"), mark(400, "end"))
}

// badgeRect is where a badge of 8 pixels goes on a frame of badgeTimeline
// drawn by the fake rasterizer, top right, 2 pixels in from the edges.
var badgeRect = image.Rect(30, 2, 38, 10)

// redraw draws the terminal of f again with the fake rasterizer.
func redraw(f reel.Frame) *image.NRGBA {
	img := image.NewNRGBA(f.Image.Rect)
	testutil.Rasterizer{}.Draw(img, f.Cells.Screen, f.Cells.State, f.At)
	return img
}

// pauseFrames runs frames through a stage of e, started on a context with a
// timeline of events at speed, and returns what it emits.
func pauseFrames(t *testing.T, e reel.Effect, speed float64, in []reel.Frame, events ...event) []reel.Frame {
	t.Helper()
	ctx := &reel.Context{
		Timeline:   buildTimeline(t, 40, 10, "", events...),
		Rasterizer: testutil.Rasterizer{},
		Layout:     testutil.Rasterizer{}.Layout(40, 10),
		Step:       10 * ms,
		Speed:      speed,
	}
	stage, err := e.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out []reel.Frame
	for i, f := range in {
		f.First, f.Last = i == 0, i == len(in)-1
		stage(f, func(f reel.Frame) { out = append(out, f) })
	}
	return out
}

// TestPause checks that by default the last frame is followed by a badged
// copy of it, paused for a second, and the frames before are left alone.
func TestPause(t *testing.T) {
	t.Parallel()
	var in []reel.Frame
	out := exportFrames(t, badgeTimeline(t), reel.Profile{
		Effects: []reel.Effect{recordFrames{&in}, Pause(WithBadgeSize(8))},
	})
	if len(in) < 2 || len(out) != len(in)+1 {
		t.Fatalf("%d frames in, %d out, want a pause after them", len(in), len(out))
	}
	for i, f := range out[:len(in)] {
		if f.Image != in[i].Image || f.Delay != in[i].Delay || f.Paused {
			t.Errorf("frame %d changed", i)
		}
	}
	paused, src := out[len(out)-1], in[len(in)-1]
	if !paused.Paused || paused.Delay != time.Second || paused.Cells != src.Cells {
		t.Fatalf("pause = %+v, want a paused second of the last frame", paused)
	}
	if want := src.At + src.Delay; paused.At != want {
		t.Errorf("pause at %v, want the end of the last frame, %v", paused.At, want)
	}
	if paused.Image == src.Image {
		t.Fatal("the pause was not drawn on a copy")
	}
	plain := redraw(src)
	if !bytes.Equal(src.Image.Pix, plain.Pix) {
		t.Error("the badge was drawn on the last frame's own image")
	}
	var changed bool
	b := paused.Image.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			same := paused.Image.NRGBAAt(x, y) == plain.NRGBAAt(x, y)
			if inBadge := image.Pt(x, y).In(badgeRect); !inBadge && !same {
				t.Errorf("pixel (%d, %d) outside the badge changed", x, y)
			} else if inBadge && !same {
				changed = true
			}
		}
	}
	if !changed {
		t.Errorf("no pixel in %v changed", badgeRect)
	}
}

// TestPauseNone checks that without Pause the last frame plays for a single
// step, with nothing after it.
func TestPauseNone(t *testing.T) {
	t.Parallel()
	out := exportFrames(t, badgeTimeline(t), reel.Profile{FPS: 100})
	if last := out[len(out)-1]; last.Paused || last.Delay != 200*ms {
		t.Errorf("last frame = %v paused %v, want 200ms to the end mark", last.Delay, last.Paused)
	}
}

// TestPauseCard checks that the end pause follows the last terminal frame,
// before a card that ends the recording, whichever is listed first, and
// that the card gets no badge.
func TestPauseCard(t *testing.T) {
	t.Parallel()
	want := image.NewNRGBA(image.Rect(0, 0, 40, 10))
	draw.Draw(want, want.Rect, image.NewUniform(outroColor), image.Point{}, draw.Src)
	for _, tc := range []struct {
		name    string
		effects []reel.Effect
	}{
		{"card first", []reel.Effect{outroEffect{}, Pause(WithBadgeSize(8))}},
		{"pause first", []reel.Effect{Pause(WithBadgeSize(8)), outroEffect{}}},
	} {
		out := exportFrames(t, badgeTimeline(t), reel.Profile{Effects: tc.effects})
		n := len(out)
		if n < 3 || !out[n-2].Paused || out[n-2].Cells == nil {
			t.Fatalf("%s: frame before the card is not the pause", tc.name)
		}
		card := out[n-1]
		if card.Cells != nil || card.Paused || !bytes.Equal(card.Image.Pix, want.Pix) {
			t.Errorf("%s: the card changed", tc.name)
		}
	}
}

// TestPauseMerge checks that the GIF still merges the frames that look
// the same before the pause.
func TestPauseMerge(t *testing.T) {
	t.Parallel()
	tl := badgeTimeline(t)
	for _, tc := range []struct {
		name    string
		effects []reel.Effect
		delays  []int
	}{
		{"none", nil, []int{40}},
		{"pause", []reel.Effect{Pause(WithBadgeSize(8))}, []int{40, 100}},
		{"no badge", []reel.Effect{Pause(WithoutBadge())}, []int{140}},
	} {
		path := filepath.Join(t.TempDir(), "out.gif")
		err := reel.Export(t.Context(), tl, reel.Profile{
			Outputs:    reel.Files(path),
			Rasterizer: blankRasterizer{},
			Effects:    tc.effects,
		})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(g.Delay, tc.delays) {
			t.Errorf("%s: GIF delays %v, want %v", tc.name, g.Delay, tc.delays)
		}
	}
}

// wantFrame is a frame [TestPauseMarks] expects: its offset, delay, and
// whether it is paused.
type wantFrame struct {
	at, delay time.Duration
	paused    bool
}

func framesOf(out []reel.Frame) []wantFrame {
	got := make([]wantFrame, len(out))
	for i, f := range out {
		got[i] = wantFrame{f.At, f.Delay, f.Paused}
	}
	return got
}

// TestPauseMarks checks the pause points at marks: each splits the frame
// that shows its mark on the delay grid, pauses that share a step play as
// one, for the longest, and the end pause follows the last frame.
func TestPauseMarks(t *testing.T) {
	t.Parallel()
	in := []reel.Frame{
		{At: 0, Delay: 300 * ms},
		{At: 300 * ms, Delay: 200 * ms},
	}
	events := []event{
		output(0, "a"), mark(100, "one"), mark(104, "two"), mark(300, "three"),
		mark(420, "x1"), mark(500, "end"),
	}
	for _, tc := range []struct {
		name  string
		opts  []PauseOption
		speed float64
		want  []wantFrame
	}{
		{
			name: "named and longest",
			opts: []PauseOption{PauseAtMarks(200*ms, "one", "two"), PauseAtMarks(500*ms, "two")},
			want: []wantFrame{
				{0, 100 * ms, false},
				{100 * ms, 500 * ms, true},
				{100 * ms, 200 * ms, false},
				{300 * ms, 200 * ms, false},
			},
		},
		{
			name: "every mark and end",
			opts: []PauseOption{PauseAtMarks(50 * ms), PauseAtEnd(time.Second)},
			want: []wantFrame{
				{0, 100 * ms, false},
				{100 * ms, 50 * ms, true},
				{100 * ms, 200 * ms, false},
				{300 * ms, 50 * ms, true},
				{300 * ms, 120 * ms, false},
				{420 * ms, 50 * ms, true},
				{420 * ms, 80 * ms, false},
				{500 * ms, time.Second, true},
			},
		},
		{
			name: "match",
			opts: []PauseOption{PauseAtMatch(70*ms, regexp.MustCompile(`^x`))},
			want: []wantFrame{
				{0, 300 * ms, false},
				{300 * ms, 120 * ms, false},
				{420 * ms, 70 * ms, true},
				{420 * ms, 80 * ms, false},
			},
		},
		{
			// The speed scales the timeline, but not the pauses.
			name:  "speed",
			opts:  []PauseOption{PauseAtMarks(200*ms, "one")},
			speed: 2,
			want: []wantFrame{
				{0, 50 * ms, false},
				{100 * ms, 200 * ms, true},
				{100 * ms, 250 * ms, false},
				{300 * ms, 200 * ms, false},
			},
		},
	} {
		frames := slices.Clone(in)
		for i := range frames {
			frames[i].Image = image.NewNRGBA(image.Rect(0, 0, 40, 10))
			frames[i].Cells = cellsOf(40, 10, "")
		}
		speed := cmp.Or(tc.speed, 1)
		out := pauseFrames(t, Pause(append(tc.opts, WithoutBadge())...), speed, frames, events...)
		if got := framesOf(out); !slices.Equal(got, tc.want) {
			t.Errorf("%s: frames\n%v\nwant\n%v", tc.name, got, tc.want)
		}
	}
}

// TestPauseShared checks that the pauses of one frame share a badged copy
// of its image.
func TestPauseShared(t *testing.T) {
	t.Parallel()
	f := reel.Frame{Image: image.NewNRGBA(image.Rect(0, 0, 40, 10)), Cells: cellsOf(40, 10, ""), Delay: 300 * ms}
	out := pauseFrames(t, Pause(PauseAtMarks(100*ms), PauseAtEnd(time.Second), WithBadgeSize(8)), 1, []reel.Frame{f},
		output(0, "a"), mark(100, "a"), mark(200, "b"))
	var badged []*image.NRGBA
	for _, f := range out {
		if f.Paused {
			badged = append(badged, f.Image)
		} else if f.Image != out[0].Image {
			t.Error("a frame between pauses got the badge")
		}
	}
	if len(badged) != 3 || badged[0] == f.Image || badged[1] != badged[0] || badged[2] != badged[0] {
		t.Errorf("%d pauses, want 3 sharing one badged copy", len(badged))
	}
}

// TestPauseKeys checks that [Keys] listed after Pause leaves a paused frame
// whole, with the keys as they were when it started, and with Keys before
// it, the pause copies the overlay of the frame it pauses.
func TestPauseKeys(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 10, "", output(0, "a"), input(300, "x"), output(300, "x"), mark(350, "here"), mark(500, "end"))
	for _, tc := range []struct {
		name    string
		effects []reel.Effect
	}{
		{"keys after", []reel.Effect{Pause(PauseAtMarks(time.Second, "here"), PauseAtEnd(time.Second)), Keys(time.Second)}},
		{"keys before", []reel.Effect{Keys(time.Second), Pause(PauseAtMarks(time.Second, "here"), PauseAtEnd(time.Second))}},
	} {
		out := exportFrames(t, tl, reel.Profile{Effects: tc.effects})
		var pauses []reel.Frame
		for _, f := range out {
			if f.Paused {
				pauses = append(pauses, f)
			}
		}
		if len(pauses) != 2 || pauses[0].Delay != time.Second || pauses[1].Delay != time.Second {
			t.Fatalf("%s: %d pauses, want two of a second", tc.name, len(pauses))
		}
		if pauses[0].At != 350*ms {
			t.Errorf("%s: pause at %v, want 350ms", tc.name, pauses[0].At)
		}
		if bytes.Equal(pauses[0].Image.Pix, redraw(pauses[0]).Pix) {
			t.Errorf("%s: the pause at the mark has no overlay or badge", tc.name)
		}
		if total := totalDelay(out); total != 2500*ms {
			t.Errorf("%s: plays for %v, want the timeline and both pauses, 2.5s", tc.name, total)
		}
	}
}

// TestPauseNoMark checks that a named mark missing from the timeline is an
// error from Start, and that matching every mark or a regexp is not.
func TestPauseNoMark(t *testing.T) {
	t.Parallel()
	tl := badgeTimeline(t)
	p := reel.Profile{Outputs: reel.Files(filepath.Join(t.TempDir(), "out.gif")), Rasterizer: testutil.Rasterizer{}}
	p.Effects = []reel.Effect{Pause(PauseAtMarks(time.Second, "end", "nope"))}
	if err := reel.Export(t.Context(), tl, p); !errors.Is(err, reel.ErrNoMark) || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("Export = %v, want ErrNoMark for nope", err)
	}
	p.Effects = []reel.Effect{Pause(PauseAtMarks(time.Second), PauseAtMatch(time.Second, regexp.MustCompile("nope")))}
	if err := reel.Export(t.Context(), tl, p); err != nil {
		t.Errorf("Export = %v", err)
	}
}

// TestPauseStart checks the options Start rejects.
func TestPauseStart(t *testing.T) {
	t.Parallel()
	ctx := &reel.Context{
		Timeline:   badgeTimeline(t),
		Rasterizer: testutil.Rasterizer{},
		Layout:     testutil.Rasterizer{}.Layout(40, 10),
	}
	for i, opt := range []PauseOption{
		WithBadgeSize(-1), WithBadgeSize(7), WithBadgeCorner(BottomRight + 1), WithBadgeCorner(-1),
		PauseAtEnd(-1), PauseAtMarks(-ms), PauseAtMatch(time.Second, nil),
	} {
		if _, err := Pause(opt).Start(ctx); err == nil {
			t.Errorf("Start with bad option %d = nil error", i)
		}
	}
	if _, err := Pause(WithBadgeSize(8), WithBadgeColor(nil)).Start(ctx); err != nil {
		t.Errorf("Start with size 8 = %v", err)
	}
	if _, err := Pause(WithoutBadge(), WithBadgeSize(1)).Start(ctx); err != nil {
		t.Errorf("Start without a badge = %v", err)
	}
}

// outroEffect inserts a card of a single color after the last frame.
type outroEffect struct{}

var outroColor = color.NRGBA{R: 1, G: 2, B: 3, A: 0xff}

func (outroEffect) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		emit(f)
		if f.Last {
			card := image.NewNRGBA(f.Image.Rect)
			draw.Draw(card, card.Rect, image.NewUniform(outroColor), image.Point{}, draw.Src)
			emit(reel.Frame{Image: card, Delay: ms * 500, Isolated: true})
		}
	}, nil
}

// blankRasterizer is the fake rasterizer drawing every cell as if empty, so
// every frame of a terminal of one size looks alike.
type blankRasterizer struct{ testutil.Rasterizer }

func (blankRasterizer) Draw(dst *image.NRGBA, _ vt.Screen, _ vt.State, _ time.Duration) {
	for i := 0; i < len(dst.Pix); i += 4 {
		copy(dst.Pix[i:], []uint8{0, 0, 0xff, 0xff})
	}
}

// TestPauseGlyph checks the glyph's colors: bars of the color on a
// disc that contrasts with it, and nothing outside the disc.
func TestPauseGlyph(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fg, disc color.NRGBA
	}{
		{badgeColor, badgeDark},
		{color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}, badgeLight},
	} {
		img := pauseGlyph(30, tc.fg)
		// Bars 4 wide and 14 tall, with 4 between them, from (9, 8).
		for _, p := range []struct {
			x, y int
			want color.NRGBA
		}{
			{0, 0, color.NRGBA{}},
			{29, 29, color.NRGBA{}},
			{15, 15, tc.disc},
			{15, 4, tc.disc},
			{11, 15, tc.fg},
			{19, 15, tc.fg},
		} {
			got, _ := color.NRGBAModel.Convert(img.At(p.x, p.y)).(color.NRGBA)
			if !closeColor(got, p.want) {
				t.Errorf("fg %v: pixel (%d, %d) = %v, want %v", tc.fg, p.x, p.y, got, p.want)
			}
		}
	}
}

// closeColor reports whether a and b differ by at most 2 in each channel,
// as premultiplying rounds.
func closeColor(a, b color.NRGBA) bool {
	near := func(x, y uint8) bool { return max(x, y)-min(x, y) <= 2 }
	return near(a.R, b.R) && near(a.G, b.G) && near(a.B, b.B) && near(a.A, b.A)
}

func TestBadgeAt(t *testing.T) {
	t.Parallel()
	grid := image.Rect(5, 10, 105, 60)
	for corner, want := range map[Corner]image.Point{
		TopLeft:     {7, 12},
		TopRight:    {97, 12},
		BottomLeft:  {7, 52},
		BottomRight: {97, 52},
	} {
		if got := badgeAt(grid, 6, corner); got != want {
			t.Errorf("badgeAt corner %d = %v, want %v", corner, got, want)
		}
	}
}

// TestPauseRecord checks that a live recording pauses at the marks it
// gets as an export of its timeline does, and that a named mark it never
// gets is an error from Stop, which still writes the recording.
func TestPauseRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	profile := func(name string, labels ...string) reel.Profile {
		return reel.Profile{
			Outputs:    reel.Files(filepath.Join(dir, name+".gif")),
			Rasterizer: testutil.Rasterizer{},
			Effects:    []reel.Effect{Pause(PauseAtMarks(300*ms, labels...), PauseAtEnd(time.Second))},
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
	missing, err := reel.Record(t.Context(), s, profile("missing", "never"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { _, werr := s.Write([]byte("a")); return werr },
		func() error { return s.Mark("one") },
		func() error { _, werr := s.Write([]byte("b")); return werr },
		func() error { return s.Mark("two") },
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
	// An export of a timeline without the mark writes what the recording
	// that waited for it did, and reports the label.
	err = reel.Export(t.Context(), s.Timeline(), profile("export-missing", "never"))
	if labels, rest := reel.MissingMarks(err); rest != nil || !slices.Equal(labels, []string{"never"}) {
		t.Errorf("Export = %v, want a NoMarkError for \"never\"", err)
	}
	waited, werr := os.ReadFile(filepath.Join(dir, "missing.gif"))
	lacked, lerr := os.ReadFile(filepath.Join(dir, "export-missing.gif"))
	if werr != nil || lerr != nil || !bytes.Equal(waited, lacked) {
		t.Errorf("recorded and exported GIFs without the mark differ (%v, %v)", werr, lerr)
	}
	if err = reel.Export(t.Context(), s.Timeline(), profile("export")); err != nil {
		t.Fatal(err)
	}
	recorded, rerr := os.ReadFile(filepath.Join(dir, "record.gif"))
	exported, eerr := os.ReadFile(filepath.Join(dir, "export.gif"))
	if rerr != nil || eerr != nil || !bytes.Equal(recorded, exported) {
		t.Errorf("recorded and exported GIFs differ (%v, %v)", rerr, eerr)
	}
	g, err := gif.DecodeAll(bytes.NewReader(exported))
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, d := range g.Delay {
		total += d
	}
	// Two pauses at the marks and one at the end, past the 180ms or so the
	// session took.
	if total < 160 {
		t.Errorf("GIF plays %dcs, want the pauses, 1.6s, and more", total)
	}
}

// TestPauseNextFrame checks that a mark past the start of the next frame,
// though within the delay of the one before on the delay grid, pauses on the
// next frame, the screen at the mark.
func TestPauseNextFrame(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 10, "", output(0, "a"), output(74, "b"), output(126, "c"), mark(130, "m"), mark(400, "end"))
	out := exportFrames(t, tl, reel.Profile{Effects: []reel.Effect{Pause(PauseAtMarks(time.Second, "m"), WithoutBadge())}})
	pauses := 0
	for i, f := range out {
		if !f.Paused {
			continue
		}
		pauses++
		if text := f.Cells.Screen.Text(); !strings.HasPrefix(text, "abc") {
			t.Errorf("pause shows %q, want the screen at the mark, abc", text)
		}
		if next := out[i+1]; next.Cells != f.Cells {
			t.Error("the frame after the pause is not the one paused")
		}
	}
	if pauses != 1 {
		t.Errorf("%d pauses, want 1", pauses)
	}
}

// TestPauseStacked checks that a Pause after another leaves the frames the
// first paused whole: at their moment, and badged once.
func TestPauseStacked(t *testing.T) {
	t.Parallel()
	var mid []reel.Frame
	out := exportFrames(t, badgeTimeline(t), reel.Profile{Effects: []reel.Effect{
		Pause(WithBadgeSize(8)), recordFrames{&mid}, Pause(PauseAtEnd(500*ms), WithBadgeSize(8)),
	}})
	first, second := out[len(out)-2], out[len(out)-1]
	if !first.Paused || !second.Paused || first.Delay != time.Second || second.Delay != 500*ms {
		t.Fatalf("last frames = %v, want pauses of 1s and 500ms", framesOf(out[len(out)-2:]))
	}
	if second.At != first.At || second.Image != mid[len(mid)-1].Image || first.Image != second.Image {
		t.Error("the second end pause is not the first's moment and image")
	}
}
