// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt"
)

// recordFrames is an [Effect] that appends every frame it sees to frames and
// passes it on.
func recordFrames(frames *[]Frame) Effect {
	return effectFunc(func(f Frame, emit func(Frame)) {
		*frames = append(*frames, f)
		emit(f)
	})
}

// frameText returns the text of a [testutil.Rasterizer] frame.
func frameText(f Frame) string { return testutil.Text(f.Image) }

// exportFrames exports tl with the fake rasterizer to a GIF, and returns the
// frames the GIF was given.
func exportFrames(t *testing.T, tl *Timeline, p Profile) []Frame {
	t.Helper()
	var frames []Frame
	p.Outputs = MustFiles(filepath.Join(t.TempDir(), "out.gif"))
	p.Rasterizer = testutil.Rasterizer{}
	p.Effects = append(slices.Clip(p.Effects), recordFrames(&frames))
	if err := Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	return frames
}

type wantFrame struct {
	text    string
	at      time.Duration
	delay   time.Duration
	focused bool
}

func assertFrames(t *testing.T, frames []Frame, want ...wantFrame) {
	t.Helper()
	var got []wantFrame
	for _, f := range frames {
		focused := f.Cells != nil && f.Cells.State.Focused
		got = append(got, wantFrame{text: frameText(f), at: f.At, delay: f.Delay, focused: focused})
	}
	if !slices.Equal(got, want) {
		t.Fatalf("frames:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestExportReplayRules(t *testing.T) {
	t.Parallel()
	bg := color.NRGBA{R: 0x20, A: 0xff}
	tl := buildTimeline(Header{Cols: 6, Rows: 1, SkipPristine: true},
		// A query changes nothing, so the terminal is still pristine.
		outAt(0, "\x1b[c"),
		outAt(100, "hi"),
		// Showing the visible cursor again is the same commit.
		outAt(150, "\x1b[?25h"),
		// A synchronized update is one frame, when it ends.
		outAt(300, "\x1b[?2026h!"),
		outAt(400, "\x1b[?2026l"),
		Input{event: at(450), Data: []byte("q")},
		// Focus and host colors change how the terminal is drawn, so each
		// change is a frame, and setting them again is not.
		Focus{event: at(600), On: false},
		Focus{event: at(620), On: false},
		Theme{event: at(650), Colors: Colors{Bg: bg}},
		Theme{event: at(680), Colors: Colors{Bg: bg}},
		// So does a program setting a color.
		outAt(690, "\x1b]12;rgb:ff/00/00\x1b\\"),
		outAt(700, "\r\x1b[K"),
		Mark{event: at(800), Label: "m"},
		Exit{event: at(1000)},
	)
	frames := exportFrames(t, tl, Profile{FPS: 100})
	assertFrames(t, frames,
		wantFrame{"hi", 100 * ms, 300 * ms, true},
		wantFrame{"hi!", 400 * ms, 200 * ms, true},
		wantFrame{"hi!", 600 * ms, 50 * ms, false},
		wantFrame{"hi!", 650 * ms, 40 * ms, false},
		wantFrame{"hi!", 690 * ms, 10 * ms, false},
		wantFrame{"", 700 * ms, 300 * ms, false},
	)
	if frames[0].Cells == nil || frames[0].Cells.At != 100*ms || frames[0].Cells.Screen.Text() == "" {
		t.Fatalf("frame cells = %+v, want the terminal at 100ms", frames[0].Cells)
	}
	if theme := frames[3].Cells.State; theme.Background != bg {
		t.Fatalf("background after the theme = %v, want %v", theme.Background, bg)
	}
	if cursor := frames[4].Cells.State; !cursor.CursorColorSet || cursor.CursorColor != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("cursor color after OSC 12 = %v (set %v), want red", cursor.CursorColor, cursor.CursorColorSet)
	}
}

// TestExportPristine checks that the blank terminal a program starts on is a
// frame of its own unless the header skips it.
func TestExportPristine(t *testing.T) {
	t.Parallel()
	events := []Event{outAt(0, "\x1b[c\x1b[?2004h"), outAt(100, "$ "), Exit{event: at(300)}}
	assertFrames(t, exportFrames(t, buildTimeline(Header{Cols: 4, Rows: 1}, events...), Profile{FPS: 100}),
		wantFrame{"", 0, 100 * ms, true},
		wantFrame{"$", 100 * ms, 200 * ms, true},
	)
	skipped := buildTimeline(Header{Cols: 4, Rows: 1, SkipPristine: true}, events...)
	assertFrames(t, exportFrames(t, skipped, Profile{FPS: 100}),
		wantFrame{"$", 100 * ms, 200 * ms, true},
	)
}

func TestExportSpeed(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 4, Rows: 1}, outAt(0, "a"), outAt(1000, "b"), Exit{event: at(2000)})
	assertFrames(t, exportFrames(t, tl, Profile{FPS: 100}),
		wantFrame{"a", 0, time.Second, true},
		wantFrame{"ab", time.Second, time.Second, true},
	)
	assertFrames(t, exportFrames(t, tl, Profile{FPS: 100, Speed: 2}),
		wantFrame{"a", 0, 500 * ms, true},
		wantFrame{"ab", time.Second, 500 * ms, true},
	)
}

// TestExportPlaysTimelineLength checks that delays carry their rounding
// error forward: changes 70ms apart on a 50ms grid play as long as the
// timeline, not one step each.
func TestExportPlaysTimelineLength(t *testing.T) {
	t.Parallel()
	events := []Event{}
	for i := range 15 {
		events = append(events, outAt(i*70, string(rune('a'+i))))
	}
	events = append(events, Exit{event: at(15 * 70)})
	tl := buildTimeline(Header{Cols: 20, Rows: 1}, events...)
	for _, speed := range []float64{1, 2} {
		var total time.Duration
		for _, f := range exportFrames(t, tl, Profile{Speed: speed}) {
			if f.Delay <= 0 || f.Delay%(50*ms) != 0 {
				t.Errorf("x%v: delay %v is not a whole number of steps", speed, f.Delay)
			}
			total += f.Delay
		}
		want := time.Duration(float64(15*70*ms) / speed)
		if d := total - want; d < -25*ms || d > 25*ms {
			t.Errorf("x%v: recording plays %v, want %v to within half a step", speed, total, want)
		}
	}
}

func TestExportWindow(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 4, Rows: 1},
		outAt(0, "a"),
		outAt(100, "b"),
		Mark{event: at(500), Label: "start"},
		outAt(700, "c"),
		Mark{event: at(900), Label: "end"},
		outAt(950, "d"),
	)
	cut, err := tl.Between("start", "end")
	if err != nil {
		t.Fatal(err)
	}
	// The window starts on the terminal the events before it drew, and ends
	// at its end mark.
	assertFrames(t, exportFrames(t, cut, Profile{FPS: 100}),
		wantFrame{"ab", 500 * ms, 200 * ms, true},
		wantFrame{"abc", 700 * ms, 200 * ms, true},
	)

	// The window opens on the focus and colors the events before it left,
	// whose frames fall before it.
	blurred := buildTimeline(Header{Cols: 4, Rows: 1},
		outAt(0, "a"),
		Focus{event: at(100)},
		Mark{event: at(500), Label: "start"},
		outAt(700, "c"),
	)
	if cut, err = blurred.Between("start", ""); err != nil {
		t.Fatal(err)
	}
	assertFrames(t, exportFrames(t, cut, Profile{FPS: 100}),
		wantFrame{"a", 500 * ms, 200 * ms, false},
		wantFrame{"ac", 700 * ms, 10 * ms, false},
	)

	// A window starting at a change starts on it.
	cut, err = tl.Between("", "start")
	if err != nil {
		t.Fatal(err)
	}
	assertFrames(t, exportFrames(t, cut, Profile{FPS: 100}),
		wantFrame{"a", 0, 100 * ms, true},
		wantFrame{"ab", 100 * ms, 400 * ms, true},
	)
}

func TestExportNoFrames(t *testing.T) {
	t.Parallel()
	// A terminal nothing visible was drawn on is still one frame.
	tl := buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "\x1b[c"), Exit{event: at(300)})
	assertFrames(t, exportFrames(t, tl, Profile{FPS: 100}), wantFrame{"", 0, 300 * ms, true})
	assertFrames(t, exportFrames(t, buildTimeline(Header{Cols: 2, Rows: 1}), Profile{FPS: 100}),
		wantFrame{"", 0, 10 * ms, true})
}

func TestExportEffects(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 4, Rows: 1}, outAt(0, "a"), outAt(100, "b"), Exit{event: at(200)})
	starts := 0
	var log []string
	card := &countingEffect{starts: &starts, stage: func(f Frame, emit func(Frame)) {
		if f.First {
			emit(Frame{Image: labelImage('C'), Delay: 70 * ms, Isolated: true})
		}
		emit(f)
	}}
	trace := effectFunc(func(f Frame, emit func(Frame)) {
		log = append(log, strings.Join([]string{frameText(f), boolStr(f.First), boolStr(f.Last), boolStr(f.Cells == nil)}, ":"))
		emit(f)
	})
	p := Profile{FPS: 100, Effects: []Effect{card, trace}}
	for range 2 {
		log = nil
		exportFrames(t, tl, p)
		if want := []string{"C:1:0:1", "a:0:0:0", "ab:0:1:0"}; !slices.Equal(log, want) {
			t.Fatalf("trace = %q, want %q", log, want)
		}
	}
	if starts != 2 {
		t.Fatalf("Start called %d times over two exports, want 2", starts)
	}
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// countingEffect counts its starts, which each return stage.
type countingEffect struct {
	starts *int
	stage  Stage
	ctx    *Context
}

func (e *countingEffect) Start(ctx *Context) (Stage, error) {
	*e.starts++
	e.ctx = ctx
	return e.stage, nil
}

// failEffect fails to start.
type failEffect struct{}

var errEffect = errors.New("effect failed")

func (failEffect) Start(*Context) (Stage, error) { return nil, errEffect }

// demoTimeline is a session-like timeline with color, a progress bar, and a
// resize.
func demoTimeline() *Timeline {
	tl := buildTimeline(Header{Cols: 20, Rows: 3, Title: "demo"},
		outAt(0, "\x1b[1;32mhello\x1b[0m \x1b[31mworld\x1b[0m"),
		outAt(120, "\r\n\x1b]9;4;3\x07working"),
		outAt(900, "\r\n\x1b[7mdone\x1b[0m\x1b]9;4;0\x07"),
		Resize{event: at(1100), Cols: 22, Rows: 3},
		Mark{event: at(1200), Label: "shot"},
		Exit{event: at(1500)},
	)
	tl.start = time.Date(2026, 10, 25, 12, 30, 45, 0, time.UTC)
	return tl
}

func TestExportDeterministic(t *testing.T) {
	t.Parallel()
	tl := demoTimeline()
	dir := t.TempDir()
	var gifs [][]byte
	for i := range 2 {
		path := filepath.Join(dir, "demo-"+string(rune('a'+i))+".gif")
		if err := Export(t.Context(), tl, Profile{Outputs: MustFiles(path)}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		gifs = append(gifs, data)
	}
	if !bytes.Equal(gifs[0], gifs[1]) {
		t.Fatal("two exports of one timeline wrote different GIFs")
	}
	// The held progress bar plays as several frames.
	if g := readGIF(t, filepath.Join(dir, "demo-a.gif")); len(g.Image) < 10 {
		t.Fatalf("gif has %d frames, want the progress bar to move", len(g.Image))
	}
}

// TestExportCastAgain checks that exporting the asciicast file an export
// wrote writes the same GIF, for times finer than the file's milliseconds
// that round across a step of the delay grid, and a first write of only part
// of a character, which the file moves into the next.
func TestExportCastAgain(t *testing.T) {
	t.Parallel()
	s := openTest(t, 6, 1)
	// The first byte of "─" draws a blank frame. Output at 4.6ms is on the
	// first step of the 10ms grid, but 5ms, once rounded to the millisecond,
	// is on the second. Then 24.5ms and 61.499ms round across half a
	// millisecond.
	timing := "0.000000 1\n0.004600 2\n0.019900 1\n0.036999 1\n0.010000 1\n"
	if err := Attach(t.Context(), s, strings.NewReader("─bcd"), WithTiming(strings.NewReader(timing))); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := Profile{FPS: 100, Outputs: MustFiles(filepath.Join(dir, "a.gif"), filepath.Join(dir, "a.cast"))}
	if err := Export(t.Context(), s.Timeline(), p); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "a.cast"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tl, err := ReadTimeline(f)
	if err != nil {
		t.Fatal(err)
	}
	assertSameTimes(t, tl, s.Timeline())
	p.Outputs = MustFiles(filepath.Join(dir, "b.gif"))
	if err = Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "a.gif"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "b.gif"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("exporting the .cast again wrote a different GIF")
	}
}

func TestExportOutputs(t *testing.T) {
	t.Parallel()
	tl := demoTimeline()
	dir := t.TempDir()
	p := Profile{
		Outputs: []File{
			{Path: filepath.Join(dir, "{name}-{duration}-{timestamp}.gif")},
			{Path: filepath.Join(dir, "sub", "{name}.png")},
			{Path: filepath.Join(dir, "{name}.cast")},
			{Path: filepath.Join(dir, "{name}.v3.cast"), Cast: CastV3},
			{Path: filepath.Join(dir, "still-{mark}.png"), Marks: []string{"shot"}},
		},
		Vars: map[string]string{"name": "demo"},
	}
	if err := Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo-1.5s-20261025T123045.gif", "sub/demo.png", "demo.cast", "demo.v3.cast", "still-shot.png"} {
		if fi, err := os.Stat(filepath.Join(dir, name)); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, version := range map[string]int{"demo.cast": 2, "demo.v3.cast": 3} {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		got, err := ReadTimeline(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(data, []byte(`{"version":`+string(rune('0'+version)))) || got.Len() == 0 {
			t.Errorf("%s is not a v%d cast of the timeline", name, version)
		}
	}
}

func TestExportStills(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 4, Rows: 1},
		outAt(0, "a"),
		Mark{event: at(10), Label: "one"},
		outAt(20, "b"),
		Mark{event: at(30), Label: "one"},
		Mark{event: at(40), Label: "two"},
	)
	dir := t.TempDir()
	p := Profile{
		Outputs:    []File{{Path: filepath.Join(dir, "{mark}.png"), Marks: []string{"one", "two"}, Options: []sink.Option{sink.WithOptimize(sink.OptimizeNone)}}},
		Rasterizer: testutil.Rasterizer{},
	}
	if err := Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	// A repeated label is shown at its first mark.
	for label, want := range map[string]string{"one": "a", "two": "ab"} {
		img := readPNG(t, filepath.Join(dir, label+".png"))
		var got []byte
		for x := range 4 {
			if r := img.NRGBAAt(x, 0).R; r != 0 {
				got = append(got, r)
			}
		}
		if strings.TrimSpace(string(got)) != want {
			t.Errorf("still %s shows %q, want %q", label, got, want)
		}
	}

	// A mark not in the window writes no still, and is reported once the
	// others are written.
	cut, err := tl.Between("two", "")
	if err != nil {
		t.Fatal(err)
	}
	cutDir := t.TempDir()
	p.Outputs[0].Path = filepath.Join(cutDir, "{mark}.png")
	err = Export(t.Context(), cut, p)
	if labels, rest := MissingMarks(err); rest != nil || !slices.Equal(labels, []string{"one"}) {
		t.Fatalf("Export = %v, want a NoMarkError for \"one\"", err)
	}
	if entries, _ := os.ReadDir(cutDir); len(entries) != 1 || entries[0].Name() != "two.png" {
		t.Fatalf("export of the cut wrote %v, want two.png", entries)
	}
}

func readPNG(t *testing.T, path string) *image.NRGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, n.Rect, img, n.Rect.Min, draw.Src)
	return n
}

// closeEffect is an [Effect] that registers two closers, which log their
// order, the second returning err.
type closeEffect struct {
	log *[]string
	err error
}

func (e closeEffect) Start(ctx *Context) (Stage, error) {
	ctx.OnClose(func() error { *e.log = append(*e.log, "first"); return nil })
	ctx.OnClose(func() error { *e.log = append(*e.log, "second"); return e.err })
	return func(f Frame, emit func(Frame)) { emit(f) }, nil
}

// TestExportClosesEffects checks that what effects register with OnClose
// runs once, newest first, whether the export succeeds or not, and that its
// errors are returned with a written export's.
func TestExportClosesEffects(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a"))
	errClose := errors.New("close failed")
	for _, tc := range []struct {
		name    string
		effects func(log *[]string) []Effect
		want    error
	}{
		{"written", func(log *[]string) []Effect { return []Effect{closeEffect{log: log}} }, nil},
		{"close fails", func(log *[]string) []Effect { return []Effect{closeEffect{log: log, err: errClose}} }, errClose},
		{"later effect fails", func(log *[]string) []Effect { return []Effect{closeEffect{log: log}, failEffect{}} }, errEffect},
	} {
		var log []string
		err := Export(t.Context(), tl, Profile{
			Outputs:    MustFiles(filepath.Join(t.TempDir(), "x.gif")),
			Rasterizer: testutil.Rasterizer{},
			Effects:    tc.effects(&log),
		})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: Export = %v, want %v", tc.name, err, tc.want)
		}
		if !slices.Equal(log, []string{"second", "first"}) {
			t.Errorf("%s: closers ran %q, want second then first, once", tc.name, log)
		}
	}
}

// TestExportFit checks that Profile.Fit draws frames within it, at the
// largest scale that fits.
func TestExportFit(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 20, Rows: 5}, outAt(0, "hello"))
	path := filepath.Join(t.TempDir(), "fit.png")
	var prev image.Point
	for _, size := range []image.Point{{400, 300}, {800, 600}, {1600, 1200}} {
		p := Profile{Outputs: MustFiles(path), Fit: size, Render: []render.Option{render.WithPadding(4)}}
		if err := Export(t.Context(), tl, p); err != nil {
			t.Fatal(err)
		}
		got := readPNG(t, path).Rect.Size()
		if got.X > size.X || got.Y > size.Y {
			t.Errorf("Fit %v wrote a %v image", size, got)
		}
		if got.X < prev.X || got.Y < prev.Y {
			t.Errorf("Fit %v wrote %v, smaller than %v for a smaller Fit", size, got, prev)
		}
		prev = got
	}
}

// TestExportFitOneDimension checks that a 0 in Fit leaves that direction
// unlimited, so the other alone decides the scale.
func TestExportFitOneDimension(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 20, Rows: 5}, outAt(0, "hello"))
	dir := t.TempDir()
	for _, size := range []image.Point{{800, 0}, {0, 300}} {
		path := filepath.Join(dir, "one.png")
		if err := Export(t.Context(), tl, Profile{Outputs: MustFiles(path), Fit: size}); err != nil {
			t.Fatal(err)
		}
		got := readPNG(t, path).Rect.Size()
		if size.X != 0 && (got.X > size.X || got.X*2 < size.X) || size.Y != 0 && (got.Y > size.Y || got.Y*2 < size.Y) {
			t.Errorf("Fit %v wrote a %v image, want it to fill the limited direction", size, got)
		}
	}
}

func TestExportFitErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 20, Rows: 5}, outAt(0, "hello"))
	path := filepath.Join(t.TempDir(), "x.png")
	for _, tc := range []struct {
		name string
		p    Profile
		want string
	}{
		{"negative", Profile{Fit: image.Pt(800, -1)}, "neither negative"},
		{"rasterizer", Profile{Fit: image.Pt(800, 600), Rasterizer: testutil.Rasterizer{}}, "cannot be set with a Rasterizer"},
		{"scale", Profile{Fit: image.Pt(800, 600), Render: []render.Option{render.WithScale(2)}}, "WithScale cannot be set"},
		{"too small", Profile{Fit: image.Pt(10, 10)}, "do not fit"},
	} {
		tc.p.Outputs = MustFiles(path)
		err := Export(t.Context(), tl, tc.p)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Export = %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}

func TestExportErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a"))
	dir := t.TempDir()
	p := func(outputs ...File) Profile {
		return Profile{Outputs: outputs, Rasterizer: testutil.Rasterizer{}}
	}
	path := filepath.Join(dir, "x.gif")
	for _, tc := range []struct {
		name string
		p    Profile
		want string
	}{
		{"no outputs", Profile{}, "no outputs"},
		{"unknown variable", p(File{Path: filepath.Join(dir, "{nope}.gif")}), "unknown variable {nope}"},
		{"unclosed variable", p(File{Path: filepath.Join(dir, "{name.gif")}), "unclosed {"},
		{"mark without Marks", p(File{Path: filepath.Join(dir, "{mark}.gif")}), "{mark} is only set in an output with Marks"},
		{"duplicate paths", p(File{Path: path}, File{Path: filepath.Join(dir, ".", "x.gif")}), "two outputs write"},
		{"duplicate marks", p(File{Path: filepath.Join(dir, "{mark}.png"), Marks: []string{"a", "a"}}), "two outputs write"},
		{"Marks without mark", p(File{Path: path, Marks: []string{"a"}}), "must hold {mark}"},
		{"cast with Marks", p(File{Path: filepath.Join(dir, "{mark}.cast"), Marks: []string{"a"}}), "cannot have Marks"},
		{"Cast on a gif", p(File{Path: path, Cast: CastV2}), "Cast is set"},
		{"unknown cast version", p(File{Path: filepath.Join(dir, "x.cast"), Cast: 7}), "unknown asciicast version"},
		{"unknown format", p(File{Path: filepath.Join(dir, "x.jpg")}), sink.ErrUnknownFormat.Error()},
		{"built-in var", Profile{Outputs: MustFiles(path), Vars: map[string]string{"duration": "1"}}, "built-in path variable {duration}"},
		{"bad fps", Profile{Outputs: MustFiles(path), FPS: 101}, "FPS 101"},
		{"bad speed", Profile{Outputs: MustFiles(path), Speed: -1}, "speed -1"},
		{"effects drop every frame", Profile{Outputs: MustFiles(path), Rasterizer: testutil.Rasterizer{}, Effects: []Effect{effectFunc(func(Frame, func(Frame)) {})}}, "dropped every frame"},
		{"effect fails", Profile{Outputs: MustFiles(path), Rasterizer: testutil.Rasterizer{}, Effects: []Effect{failEffect{}}}, errEffect.Error()},
	} {
		err := Export(t.Context(), tl, tc.p)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Export = %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed exports left %d files", len(entries))
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Export(ctx, tl, p(File{Path: path})); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Export = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a canceled export wrote its output")
	}
}

// TestExportCastOnly checks that an export of only asciicast outputs draws
// nothing: its effects never start.
func TestExportCastOnly(t *testing.T) {
	t.Parallel()
	tl := demoTimeline()
	path := filepath.Join(t.TempDir(), "only.cast")
	err := Export(t.Context(), tl, Profile{Outputs: MustFiles(path), Effects: []Effect{failEffect{}}, Title: "titled"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := tl.Header()
	h.Title = "titled"
	var want bytes.Buffer
	if _, err = tl.fixed(h, -1).WriteTo(&want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want.Bytes()) {
		t.Fatalf("cast:\n%s\nwant:\n%s", data, want.Bytes())
	}
}

// TestExportPalette checks the palette the frames are drawn with: one set
// in the profile's render options, else the timeline's, else the default,
// and that an asciicast output records it.
func TestExportPalette(t *testing.T) {
	t.Parallel()
	red, blue := color.NRGBA{R: 0xff, A: 0xff}, color.NRGBA{B: 0xff, A: 0xff}
	// A cast only has a palette with the header colors.
	h := Header{Cols: 2, Rows: 1, Theme: Colors{Fg: castFg, Bg: castBg}}
	withHeader := h
	withHeader.Palette[1] = red
	for _, tc := range []struct {
		name   string
		header Header
		render []render.Option
		want   color.Color
	}{
		{"default", h, nil, nil},
		{"timeline", withHeader, nil, red},
		{"profile", withHeader, []render.Option{render.WithPalette(render.Palette{Indexed: map[int]color.Color{1: blue}})}, blue},
	} {
		dir := t.TempDir()
		starts := 0
		ctxEffect := &countingEffect{starts: &starts, stage: func(f Frame, emit func(Frame)) { emit(f) }}
		p := Profile{
			Outputs:    MustFiles(filepath.Join(dir, "x.cast"), filepath.Join(dir, "x.gif")),
			Render:     tc.render,
			Rasterizer: testutil.Rasterizer{},
			Effects:    []Effect{ctxEffect},
		}
		// The fake rasterizer does not say which palette it drew with, so
		// the options are checked; a custom rasterizer keeps the timeline's
		// palette in the cast.
		if err := Export(t.Context(), buildTimeline(tc.header, outAt(0, "a")), p); err != nil {
			t.Fatal(err)
		}
		pal, err := render.PaletteOf(ctxEffect.ctx.Render...)
		if err != nil {
			t.Fatal(err)
		}
		if got := pal.Indexed[1]; got != tc.want {
			t.Errorf("%s: Context.Render palette color 1 = %v, want %v", tc.name, got, tc.want)
		}
		if got := readCastHeader(t, filepath.Join(dir, "x.cast")).Palette[1]; got != withDefaultPalette(tc.header).Palette[1] {
			t.Errorf("%s: custom rasterizer cast palette color 1 = %v, want the timeline's", tc.name, got)
		}

		p.Rasterizer, p.Outputs = nil, MustFiles(filepath.Join(dir, "y.cast"))
		if err = Export(t.Context(), buildTimeline(tc.header, outAt(0, "a")), p); err != nil {
			t.Fatal(err)
		}
		want := tc.want
		if want == nil {
			want = withDefaultPalette(h).Palette[1]
		}
		if got := readCastHeader(t, filepath.Join(dir, "y.cast")).Palette[1]; got != want {
			t.Errorf("%s: cast palette color 1 = %v, want %v", tc.name, got, want)
		}
	}
}

func readCastHeader(t *testing.T, path string) Header {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return readCast(t, data).Header()
}

// concealer is a [Concealer] that passes frames on, and conceals when on is
// set.
type concealer struct{ on bool }

func (concealer) Start(*Context) (Stage, error) {
	return func(f Frame, emit func(Frame)) { emit(f) }, nil
}

func (r concealer) Conceals() bool { return r.on }

// TestExportConcealer checks that a profile with a [Concealer] lists it
// first, and writes no output that skips the effects, failing before it
// writes anything.
func TestExportConcealer(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a"), Mark{event: at(10), Label: "m"})
	keep := effectFunc(func(f Frame, emit func(Frame)) { emit(f) })
	for _, tc := range []struct {
		name    string
		outputs []File
		effects []Effect
		is      error
		want    string
	}{
		{"cast", MustFiles("x.gif", "x.cast"), []Effect{concealer{true}}, ErrRawOutput, "asciicast"},
		{"cast only", MustFiles("x.cast"), []Effect{concealer{true}, keep}, ErrRawOutput, "asciicast"},
		{"order", MustFiles("x.gif"), []Effect{concealer{true}, keep, concealer{true}}, nil, "effect 2 (reel.concealer) conceals, so it must come before effect 1"},
		{"wrapped cast", MustFiles("x.gif", "x.cast"), []Effect{wrapped{concealer{true}}}, ErrRawOutput, "asciicast"},
		{"wrapped order", MustFiles("x.gif"), []Effect{keep, wrapped{wrapped{concealer{true}}}}, nil, "conceals, so it must come before effect 0"},
	} {
		dir := t.TempDir()
		var outputs []File
		for _, o := range tc.outputs {
			o.Path = filepath.Join(dir, o.Path)
			outputs = append(outputs, o)
		}
		err := Export(t.Context(), tl, Profile{Outputs: outputs, Rasterizer: testutil.Rasterizer{}, Effects: tc.effects})
		if err == nil || !strings.Contains(err.Error(), tc.want) || (tc.is != nil && !errors.Is(err, tc.is)) {
			t.Errorf("%s: Export = %v, want an error containing %q", tc.name, err, tc.want)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s: wrote %d files", tc.name, len(entries))
		}
	}
	// One that does not redact may come anywhere, and one that does may
	// write the other outputs.
	dir := t.TempDir()
	for _, effects := range [][]Effect{
		{keep, concealer{false}},
		{concealer{true}, concealer{true}, keep},
	} {
		if err := Export(t.Context(), tl, Profile{Outputs: MustFiles(filepath.Join(dir, "x.gif")), Rasterizer: testutil.Rasterizer{}, Effects: effects}); err != nil {
			t.Fatal(err)
		}
	}
}

// cutter is a [Cutter] that passes frames on, and cuts when on is set.
type cutter struct{ on bool }

func (cutter) Start(*Context) (Stage, error) {
	return func(f Frame, emit func(Frame)) { emit(f) }, nil
}

func (c cutter) Cuts() bool { return c.on }

// TestExportCutter checks that a profile with a [Cutter] that cuts writes
// no output that skips the effects, failing before it writes anything, and
// that a Cutter may come anywhere.
func TestExportCutter(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a"))
	keep := effectFunc(func(f Frame, emit func(Frame)) { emit(f) })
	for _, effects := range [][]Effect{{cutter{true}}, {keep, wrapped{cutter{true}}}} {
		dir := t.TempDir()
		err := Export(t.Context(), tl, Profile{Outputs: MustFiles(filepath.Join(dir, "x.gif"), filepath.Join(dir, "x.cast")), Rasterizer: testutil.Rasterizer{}, Effects: effects})
		if !errors.Is(err, ErrRawOutput) {
			t.Errorf("Export = %v, want ErrRawOutput", err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("wrote %d files", len(entries))
		}
	}
	// One that cuts may come anywhere, and one that does not may write
	// asciicast.
	dir := t.TempDir()
	for _, p := range []Profile{
		{Outputs: MustFiles(filepath.Join(dir, "a.gif")), Effects: []Effect{keep, cutter{true}, keep}},
		{Outputs: MustFiles(filepath.Join(dir, "b.gif"), filepath.Join(dir, "b.cast")), Effects: []Effect{cutter{false}}},
	} {
		p.Rasterizer = testutil.Rasterizer{}
		if err := Export(t.Context(), tl, p); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExportStillEffects checks that a still runs through the [Concealer]
// and [StillEffect] values alone, as a recording of its one frame, and that
// one they drop is an error.
func TestExportStillEffects(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a"), Mark{event: at(10), Label: "m"})
	var got []Frame
	hide := effectFunc(func(f Frame, emit func(Frame)) {
		got = append(got, f)
		img := image.NewNRGBA(f.Image.Rect)
		copy(img.Pix, f.Image.Pix)
		img.Pix[0] = 'x'
		f.Image = img
		emit(f)
	})
	other := effectFunc(func(f Frame, emit func(Frame)) {
		f.Image = image.NewNRGBA(f.Image.Rect)
		emit(f)
	})
	dir := t.TempDir()
	p := Profile{
		Outputs:    []File{{Path: filepath.Join(dir, "{mark}.png"), Marks: []string{"m"}}},
		Rasterizer: testutil.Rasterizer{},
		Effects:    []Effect{concealerEffect{hide}, other},
	}
	if err := Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].First || !got[0].Last || got[0].At != 10*time.Millisecond || got[0].Cells == nil {
		t.Fatalf("concealer given %+v, want the still's one frame", got)
	}
	if text := testutil.Text(readPNG(t, filepath.Join(dir, "m.png"))); text != "x" {
		t.Fatalf("still shows %q, want %q", text, "x")
	}
	// A concealer after one that emits two frames is given them as a
	// recording of two, and the first it emits is the still.
	twice := effectFunc(func(f Frame, emit func(Frame)) {
		emit(f)
		emit(f)
	})
	var flags [][2]bool
	mark := effectFunc(func(f Frame, emit func(Frame)) {
		flags = append(flags, [2]bool{f.First, f.Last})
		hide(f, emit)
	})
	p.Effects = []Effect{concealerEffect{twice}, concealerEffect{mark}}
	p.Outputs[0].Path = filepath.Join(t.TempDir(), "{mark}.png")
	if err := Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	if want := [][2]bool{{true, false}, {false, true}}; !slices.Equal(flags, want) {
		t.Fatalf("First and Last %v, want %v", flags, want)
	}
	// A StillEffect after an effect that does not run on stills runs on
	// them too, wrapped or not.
	for _, e := range []Effect{stillEffect{hide}, wrapped{stillEffect{hide}}} {
		got = nil
		p.Effects = []Effect{other, e}
		p.Outputs[0].Path = filepath.Join(t.TempDir(), "{mark}.png")
		if err := Export(t.Context(), tl, p); err != nil {
			t.Fatal(err)
		}
		if text := testutil.Text(readPNG(t, strings.Replace(p.Outputs[0].Path, "{mark}", "m", 1))); len(got) != 1 || text != "x" {
			t.Fatalf("%T: still shows %q after %d frames, want %q after one", e, text, len(got), "x")
		}
	}
	drop := effectFunc(func(Frame, func(Frame)) {})
	p.Effects = []Effect{concealerEffect{drop}}
	p.Outputs[0].Path = filepath.Join(t.TempDir(), "{mark}.png")
	if err := Export(t.Context(), tl, p); err == nil || !strings.Contains(err.Error(), "dropped the still") {
		t.Fatalf("Export = %v, want an error for the dropped still", err)
	}
}

// wrapped wraps an effect, and unwraps to it.
type wrapped struct{ Effect }

func (w wrapped) Unwrap() Effect { return w.Effect }

// stillEffect is a [StillEffect] that runs its stage.
type stillEffect struct{ stage effectFunc }

func (e stillEffect) Start(ctx *Context) (Stage, error) { return e.stage.Start(ctx) }

func (stillEffect) Stills() bool { return true }

// concealerEffect is a [Concealer] that runs its stage.
type concealerEffect struct{ stage effectFunc }

func (e concealerEffect) Start(ctx *Context) (Stage, error) { return e.stage.Start(ctx) }

func (concealerEffect) Conceals() bool { return true }

// TestContextValues checks that a value one effect stores is read by the
// effects started after it.
func TestContextValues(t *testing.T) {
	t.Parallel()
	type key struct{}
	var got []any
	store := startFunc(func(ctx *Context) { got = append(got, ctx.Value(key{})); ctx.SetValue(key{}, 1) })
	read := startFunc(func(ctx *Context) { got = append(got, ctx.Value(key{})) })
	exportFrames(t, buildTimeline(Header{Cols: 2, Rows: 1}, outAt(0, "a")), Profile{Effects: []Effect{store, read}})
	if !slices.Equal(got, []any{nil, 1}) {
		t.Fatalf("values %v, want [<nil> 1]", got)
	}
}

// startFunc is an [Effect] that calls fn when it starts, and changes no
// frame.
type startFunc func(*Context)

func (e startFunc) Start(ctx *Context) (Stage, error) {
	e(ctx)
	return nil, nil //nolint:nilnil // It changes no frame.
}

func TestExportReplaysScroll(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{Cols: 6, Rows: 2, Scrollback: vt.DefaultScrollback})
	tl.appendAt(Output{Data: []byte("a\r\nb\r\nc\r\nd\r\ne")}, 0)
	tl.appendAt(Scroll{Offset: 2}, time.Second)
	tl.appendAt(Scroll{Offset: 0}, 2*time.Second)

	var live, scrolled string
	for _, f := range exportFrames(t, tl, Profile{FPS: 10}) {
		if f.Cells == nil {
			continue
		}
		switch {
		case f.Cells.State.ScrollbackOffset == 0 && f.At < time.Second:
			live = frameText(f)
		case f.Cells.State.ScrollbackOffset == 2:
			scrolled = frameText(f)
		}
	}
	if live == "" || scrolled == "" || live == scrolled {
		t.Fatalf("live frame %q, scrolled frame %q, want two different screens", live, scrolled)
	}
}

func TestExportScrollClamps(t *testing.T) {
	t.Parallel()
	// The event was recorded against more scrollback than the replay has.
	tl := newTimeline(Header{Cols: 6, Rows: 2, Scrollback: vt.DefaultScrollback})
	tl.appendAt(Output{Data: []byte("a\r\nb\r\nc\r\nd")}, 0)
	tl.appendAt(Scroll{Offset: 500}, time.Second)

	var got, scrollback int
	for _, f := range exportFrames(t, tl, Profile{FPS: 10}) {
		if f.Cells != nil {
			got, scrollback = f.Cells.State.ScrollbackOffset, f.Cells.State.ScrollbackLen
		}
	}
	if scrollback == 0 || got != scrollback {
		t.Fatalf("last frame offset = %d with %d rows of scrollback, want the top", got, scrollback)
	}
}

func TestCutKeepsScrolledViewport(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{Cols: 6, Rows: 2, Scrollback: vt.DefaultScrollback})
	tl.appendAt(Output{Data: []byte("a\r\nb\r\nc\r\nd\r\ne")}, 0)
	tl.appendAt(Scroll{Offset: 2}, time.Second)
	tl.appendAt(Mark{Label: "here"}, 2*time.Second)
	tl.appendAt(Output{Data: []byte("!")}, 3*time.Second)

	cut, err := tl.Between("here", "")
	if err != nil {
		t.Fatal(err)
	}
	var scrolls []int
	for ev := range cut.Events() {
		if sc, ok := ev.(Scroll); ok {
			scrolls = append(scrolls, sc.Offset)
		}
	}
	if !slices.Equal(scrolls, []int{2}) {
		t.Fatalf("Scroll events of the cut = %v, want the scrolled viewport restored as [2]", scrolls)
	}
	data := writeCast(t, cut, CastV3)
	if !bytes.Contains(data, []byte(`"s"`)) {
		t.Fatalf("cast of the cut has no scroll event: %s", data)
	}
}
