// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// redactorFunc is an [reel.Effect] whose stage is fn, and that claims to
// redact, so it may run before [Redact].
type redactorFunc func(f reel.Frame, emit func(reel.Frame))

func (e redactorFunc) Start(*reel.Context) (reel.Stage, error) { return reel.Stage(e), nil }

func (redactorFunc) Conceals() bool { return true }

// redactFrames exports tl through effects with the fake rasterizer r at
// 100 FPS, and returns the frames given to the effects and those they
// emitted. It checks that every frame emitted shows the cells it holds, so
// what a test reads of the cells is what the frame shows.
func redactFrames(t *testing.T, r testutil.Rasterizer, tl *reel.Timeline, effects ...reel.Effect) (in, out []reel.Frame) {
	t.Helper()
	keep := redactorFunc(func(f reel.Frame, emit func(reel.Frame)) {
		in = append(in, f)
		emit(f)
	})
	out = exportFrames(t, tl, reel.Profile{
		FPS:        100,
		Rasterizer: r,
		Effects:    append([]reel.Effect{keep}, effects...),
	})
	for i, f := range out {
		if f.Cells == nil {
			continue
		}
		img := image.NewNRGBA(f.Image.Rect)
		r.Draw(img, f.Cells.Screen, f.Cells.State, f.At)
		if !bytes.Equal(img.Pix, f.Image.Pix) && !f.Isolated {
			t.Fatalf("frame %d does not show its cells", i)
		}
	}
	return in, out
}

// trimLines cuts the trailing spaces of each line of s.
func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// redactedScreens returns the trimmed text of the screen of each terminal
// frame of the export of tl through effects.
func redactedScreens(t *testing.T, tl *reel.Timeline, effects ...reel.Effect) []string {
	t.Helper()
	_, out := redactFrames(t, testutil.Rasterizer{}, tl, effects...)
	var got []string
	for _, f := range out {
		if f.Cells != nil {
			got = append(got, trimLines(f.Cells.Screen.Text()))
		}
	}
	return got
}

func TestRedactModes(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 14, 2, "", output(0, "pw \x1b[4;31mhunter2\x1b[0m ok"))
	for _, tc := range []struct {
		name string
		opts []RedactOption
		want string
	}{
		{"default", nil, "pw " + strings.Repeat(" ", 7) + " ok"},
		{"block", []RedactOption{Block()}, "pw " + strings.Repeat(" ", 7) + " ok"},
		{"replace", []RedactOption{Replace('*')}, "pw ******* ok"},
		{"bullet", []RedactOption{Replace('•')}, "pw ••••••• ok"},
		{"blur", []RedactOption{Blur()}, "pw xxxxxxx ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, out := redactFrames(t, testutil.Rasterizer{}, tl, Redact(Text("hunter2"), tc.opts...))
			if len(out) != 1 || out[0].Cells == in[0].Cells || out[0].Image == in[0].Image {
				t.Fatalf("%d frames, want one redacted frame", len(out))
			}
			if got := trimLines(out[0].Cells.Screen.Text()); got != tc.want {
				t.Fatalf("screen %q, want %q", got, tc.want)
			}
			if strings.Contains(in[0].Cells.Screen.Text(), "•") || !strings.Contains(in[0].Cells.Screen.Text(), "hunter2") {
				t.Fatal("the frame given to Redact was changed")
			}
			c := out[0].Cells.Screen.CellAt(3, 0)
			switch tc.name {
			case "default", "block":
				if c.Style.Fg != nil || c.Style.Underline != 0 || c.Style.Attrs != 0 {
					t.Errorf("hidden cell styled %+v, want the background only", c.Style)
				}
			default:
				if c.Style.Fg == nil || c.Style.Underline == 0 {
					t.Errorf("hidden cell styled %+v, want the cell's style", c.Style)
				}
			}
			if isolated := tc.name == "blur"; out[0].Isolated != isolated {
				t.Errorf("isolated = %v, want %v", out[0].Isolated, isolated)
			}
		})
	}
}

func TestRedactRegions(t *testing.T) {
	t.Setenv("REEL_REDACT_TEST", "s3cr3t")
	t.Setenv("REEL_REDACT_EMPTY", " ")
	tl := buildTimeline(t, 16, 3, "",
		output(0, "id 1234-5678 x\r\nenv s3cr3t\r\nab"),
	)
	for _, tc := range []struct {
		name   string
		region Region
		want   string
	}{
		{"text", Text("5678"), "id 1234-**** x\nenv s3cr3t\nab"},
		{"regexp", Regexp(regexp.MustCompile(`\d{4}-\d{4}`)), "id ********* x\nenv s3cr3t\nab"},
		{"env", Env("REEL_REDACT_TEST", "REEL_REDACT_EMPTY"), "id 1234-5678 x\nenv ******\nab"},
		{"cells", Cells(image.Rect(0, 1, 3, 3)), "id 1234-5678 x\n*** s3cr3t\n***"},
		{"row", Row(2), "id 1234-5678 x\nenv s3cr3t\n****************"},
		{"any", Any(Text("id"), nil, Env("REEL_REDACT_TEST")), "** 1234-5678 x\nenv ******\nab"},
	} {
		got := redactedScreens(t, tl, Redact(tc.region, Replace('*')))
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: screens %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestRedactTyping checks that a secret typed into a prompt that echoes it
// is hidden from its third character on, and that its start is hidden
// where it is cut off, but not where it goes on as other text.
func TestRedactTyping(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 4, "",
		output(0, "Token: "),
		output(100, "a"),
		output(200, "b"),
		output(300, "c"),
		output(400, "d"),
		output(500, "ef\r\n"),
		output(600, "abcx abc- [abcd…]\r\n"),
		output(700, "ab abcdef\r\n"),
		output(800, "abcz\x1b[D"),
	)
	got := redactedScreens(t, tl, Redact(Text("abcdef"), Replace('*')))
	want := []string{
		"Token:",
		"Token: a",
		"Token: ab",
		"Token: ***",
		"Token: ****",
		"Token: ******",
		"Token: ******\nabcx ***- [****…]",
		"Token: ******\nabcx ***- [****…]\nab ******",
		// The cursor is on the z.
		"Token: ******\nabcx ***- [****…]\nab ******\n***z",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("screens\n got %q\nwant %q", got, want)
	}
	// Three characters or fewer are hidden only whole.
	tl = buildTimeline(t, 10, 1, "", output(0, "ab abc"))
	if short := redactedScreens(t, tl, Redact(Text("abc"), Replace('*'))); !slices.Equal(short, []string{"ab ***"}) {
		t.Fatalf("screens %q", short)
	}
}

// TestRedactMinPartial checks that [MinPartial] sets the fewest characters
// of the start or end of a secret hidden, and that one as long as the
// secret hides it only whole.
func TestRedactMinPartial(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 40, 2, "", output(0, "a ab abx f. ef def abcdef ghi"), output(100, "\r\na"))
	for _, tc := range []struct {
		n    int
		want []string
	}{
		{1, []string{"* ** abx *. ** *** ****** ghi", "* ** abx *. ** *** ****** ghi\n*"}},
		{2, []string{"a ** abx f. ** *** ****** ghi", "a ** abx f. ** *** ****** ghi\na"}},
		{3, []string{"a ab abx f. ef *** ****** ghi", "a ab abx f. ef *** ****** ghi\na"}},
		{6, []string{"a ab abx f. ef def ****** ghi", "a ab abx f. ef def ****** ghi\na"}},
		{100, []string{"a ab abx f. ef def ****** ghi", "a ab abx f. ef def ****** ghi\na"}},
	} {
		got := redactedScreens(t, tl, Redact(Text("abcdef"), Replace('*'), MinPartial(tc.n)))
		if !slices.Equal(got, tc.want) {
			t.Errorf("MinPartial(%d): screens\n got %q\nwant %q", tc.n, got, tc.want)
		}
	}
}

// TestRedactSuffix checks that the end of a secret is hidden where its
// start is gone: soft-wrapped past the top of the screen, or cut off.
func TestRedactSuffix(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 12, 3, "", output(0, "token: ghp_abcdefghij0123456789\r\nx"))
	got := redactedScreens(t, tl, Redact(Text("ghp_abcdefghij0123456789"), Replace('*')))
	if want := []string{"************\n*******\nx"}; !slices.Equal(got, want) {
		t.Fatalf("screens %q, want %q", got, want)
	}
	tl = buildTimeline(t, 30, 1, "", output(0, "[…nter2] xnter2 ter2"))
	got = redactedScreens(t, tl, Redact(Text("hunter2"), Replace('*')))
	if want := []string{"[…*****] xnter2 ****"}; !slices.Equal(got, want) {
		t.Fatalf("screens %q, want %q", got, want)
	}
}

// TestRedactRenderer checks Redact with the renderer, whose frame cache
// must never keep pixels of the frames it drew before the effects: every
// frame shows what a fresh renderer draws of its cells, with the secret on
// screen, in the title, and in the title only. The frames a held frame
// plays as, to keep its progress bar moving, share one hidden screen, as
// they share one terminal.
func TestRedactRenderer(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 20, 3, "",
		output(0, "\x1b]2;hunter2\x07\x1b]9;4;3\x07pw hunter2"),
		output(300, "\x1b[2J\x1b[Hok"),
		output(400, "\x1b]2;done\x07"),
		mark(900, "end"),
	)
	opts := []render.Option{render.WithWindowChrome(true)}
	var frames []reel.Frame
	err := reel.Export(t.Context(), tl, reel.Profile{
		Outputs: reel.Files(filepath.Join(t.TempDir(), "out.gif")),
		Render:  opts,
		Effects: []reel.Effect{Redact(Text("hunter2")), recordFrames{&frames}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := render.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	screens := map[*vt.Buffer]int{}
	for i, f := range frames {
		if strings.Contains(f.Cells.Screen.Text(), "hunter2") || strings.Contains(f.Cells.State.Title, "hunter2") {
			t.Fatalf("frame %d shows the secret", i)
		}
		// A renderer draws a screen it has not drawn before whole.
		want := image.NewNRGBA(f.Image.Rect)
		fresh.Draw(want, f.Cells.Screen.Clone(), f.Cells.State, f.At)
		if !bytes.Equal(want.Pix, f.Image.Pix) {
			t.Fatalf("frame %d at %v differs from its cells drawn afresh", i, f.At)
		}
		screens[f.Cells.Screen]++
	}
	if len(frames) < 6 || len(screens) > 4 {
		t.Fatalf("%d frames of %d screens, want the held frames to share theirs", len(frames), len(screens))
	}
}

// TestRedactWrappedWide checks a secret soft-wrapped over two rows, and
// wide characters, which are hidden as two cells.
func TestRedactWrappedWide(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 8, 3, "", output(0, "key=hunter2\r\n日本語x"))
	got := redactedScreens(t, tl, Redact(Any(Text("hunter2"), Regexp(regexp.MustCompile(`本.`))), Replace('*')))
	want := []string{"key=****\n***\n日****x"}
	if !slices.Equal(got, want) {
		t.Fatalf("screens %q, want %q", got, want)
	}
}

// TestRedactTitleStatus checks that text regions hide their text in the
// title, the program status, and the context's title.
func TestRedactTitleStatus(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 10, 2, "deploy hunter2",
		output(0, "\x1b]2;login hunter2\x07\x1b]7501;state=working:app=hunter2\x1b\\x"),
	)
	var title string
	_, out := redactFrames(t, testutil.Rasterizer{}, tl, Redact(Text("hunter2")), titleOf{&title})
	if title != "deploy •••••••" {
		t.Errorf("context title %q", title)
	}
	if len(out) != 1 {
		t.Fatalf("%d frames, want 1", len(out))
	}
	st := out[0].Cells.State
	if st.Title != "login •••••••" || len(st.ProgramStatuses) != 1 || st.ProgramStatuses[0].App != "•••••••" {
		t.Fatalf("title %q, statuses %+v", st.Title, st.ProgramStatuses)
	}
	// Cells regions hide no title.
	_, out = redactFrames(t, testutil.Rasterizer{}, tl, Redact(Row(1)))
	if len(out) != 1 || out[0].Cells.State.Title != "login hunter2" {
		t.Fatalf("frames %+v", infoOf(out))
	}
}

// titleOf is an [reel.Effect] that stores the context's title when it
// starts.
type titleOf struct{ title *string }

func (e titleOf) Start(ctx *reel.Context) (reel.Stage, error) {
	*e.title = ctx.Title
	return nil, nil //nolint:nilnil // It changes no frame.
}

// TestRedactBlur checks that blurred cells show a blur of x's, never the
// secret, and that nothing outside them changes.
func TestRedactBlur(t *testing.T) {
	t.Parallel()
	r := testutil.Rasterizer{Cell: image.Pt(4, 4), Pad: 2}
	// The cursor is on the first e, so the cells do not all look alike.
	tl := buildTimeline(t, 8, 2, "", output(0, "a secret\r\nzz\x1b[1;4H"))
	_, blurred := redactFrames(t, r, tl, Redact(Text("secret"), Blur()))
	_, replaced := redactFrames(t, r, tl, Redact(Text("secret"), Replace('x')))
	if len(blurred) != 1 || len(replaced) != 1 {
		t.Fatalf("%d and %d frames, want 1", len(blurred), len(replaced))
	}
	if got := trimLines(blurred[0].Cells.Screen.Text()); got != "a xxxxxx\nzz" {
		t.Fatalf("screen %q", got)
	}
	cells := r.Layout(8, 2).CellRect(2, 0).Union(r.Layout(8, 2).CellRect(7, 0))
	b, p := blurred[0].Image, replaced[0].Image
	changed := false
	for y := b.Rect.Min.Y; y < b.Rect.Max.Y; y++ {
		for x := b.Rect.Min.X; x < b.Rect.Max.X; x++ {
			same := b.NRGBAAt(x, y) == p.NRGBAAt(x, y)
			if !image.Pt(x, y).In(cells) && !same {
				t.Fatalf("pixel (%d, %d) outside the cells changed", x, y)
			}
			changed = changed || !same
		}
	}
	if !changed {
		t.Fatal("nothing was blurred")
	}
}

// TestRedactBetween checks a redaction limited to a window between marks,
// which covers every step that shows any of it, and a frame without the
// secret, which is left alone.
func TestRedactBetween(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 8, 1, "",
		output(0, "pw"),
		output(100, " abcd"),
		mark(155, "on"),
		output(200, "\x1b[2K\rxx"),
		output(300, "\x1b[2K\rabcd"),
		mark(345, "off"),
		output(400, "!"),
	)
	in, out := redactFrames(t, testutil.Rasterizer{}, tl, Redact(Text("abcd"), Replace('*'), Between("on", "off")))
	type piece struct {
		at, delay time.Duration
		screen    string
	}
	var got []piece
	for _, f := range out {
		got = append(got, piece{f.At, f.Delay, trimLines(f.Cells.Screen.Text())})
	}
	want := []piece{
		{0, 100 * ms, "pw"},
		{100 * ms, 50 * ms, "pw abcd"},
		{150 * ms, 50 * ms, "pw ****"},
		{200 * ms, 100 * ms, "xx"},
		{300 * ms, 50 * ms, "****"},
		{350 * ms, 50 * ms, "abcd"},
		{400 * ms, 10 * ms, "abcd!"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("frames\n got %+v\nwant %+v", got, want)
	}
	if out[3].Cells != in[2].Cells || out[3].Image != in[2].Image {
		t.Error("the frame without the secret was changed")
	}
}

func TestRedactErrors(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "", output(0, "a"), mark(10, "m"))
	r := testutil.Rasterizer{}
	ctx := &reel.Context{Timeline: tl, Rasterizer: r, Layout: r.Layout(4, 1), Step: 10 * ms, Speed: 1}
	for _, tc := range []struct {
		name   string
		effect reel.Effect
	}{
		{"nil region", Redact(nil)},
		{"ease", Redact(Row(0), Ease(10*ms))},
		{"ease in", Redact(Row(0), EaseIn(10*ms))},
		{"ease out", Redact(Row(0), EaseOut(10*ms))},
		{"negative ease", Redact(Row(0), Ease(-ms))},
		{"wide replacement", Redact(Row(0), Replace('日'))},
		{"control replacement", Redact(Row(0), Replace('\t'))},
		{"inputs nil region", RedactInputs(nil)},
		{"inputs cells", RedactInputs(Any(Text("a"), Row(0)))},
		{"min partial", Redact(Text("a"), MinPartial(0))},
		{"inputs min partial", RedactInputs(Text("a"), MinPartial(-1))},
	} {
		st, err := tc.effect.Start(ctx)
		if err == nil || st != nil {
			t.Errorf("%s: Start = %v, want an error", tc.name, err)
		}
	}
}

// TestRedactOutputs checks that a profile with a redacting effect cannot
// write an asciicast file, writing nothing, and must list its redacting
// effects first.
func TestRedactOutputs(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 10, 1, "", output(0, "hunter2"), mark(10, "m"))
	for _, tc := range []struct {
		name    string
		outputs []reel.File
		effects []reel.Effect
		is      error
	}{
		{"cast", reel.Files("out.gif", "out.cast"), []reel.Effect{Redact(Text("hunter2"))}, reel.ErrRawOutput},
		{"cast only", reel.Files("out.cast"), []reel.Effect{Redact(Text("hunter2"))}, reel.ErrRawOutput},
		{"inputs cast", reel.Files("out.gif", "out.cast"), []reel.Effect{RedactInputs(Text("hunter2"))}, reel.ErrRawOutput},
		{"order", reel.Files("out.gif"), []reel.Effect{Keys(time.Second), Redact(Text("hunter2"))}, nil},
		{"inputs order", reel.Files("out.gif"), []reel.Effect{Keys(time.Second), RedactInputs(Text("hunter2"))}, nil},
	} {
		dir := t.TempDir()
		var outputs []reel.File
		for _, o := range tc.outputs {
			o.Path = filepath.Join(dir, o.Path)
			outputs = append(outputs, o)
		}
		err := reel.Export(t.Context(), tl, reel.Profile{Outputs: outputs, Rasterizer: testutil.Rasterizer{}, Effects: tc.effects})
		if err == nil || (tc.is != nil && !errors.Is(err, tc.is)) {
			t.Errorf("%s: Export = %v, want an error", tc.name, err)
		}
		if files, _ := os.ReadDir(dir); len(files) > 0 {
			t.Errorf("%s: wrote %d files", tc.name, len(files))
		}
	}
	s, err := reel.Open(t.Context(), 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = reel.Record(t.Context(), s, reel.Profile{
		Outputs:    reel.Files(filepath.Join(t.TempDir(), "out.cast")),
		Rasterizer: testutil.Rasterizer{},
		Effects:    []reel.Effect{Redact(Text("hunter2"))},
	})
	if !errors.Is(err, reel.ErrRawOutput) {
		t.Fatalf("Record = %v, want an error wrapping reel.ErrRawOutput", err)
	}
}

// TestRedactRecord checks a live recording whose window opens on a mark
// appended after it started: from the mark on, the secret never shows, and
// the effects after Redact see the cells it hid.
func TestRedactRecord(t *testing.T) {
	t.Parallel()
	s, err := reel.Open(t.Context(), 12, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var frames []reel.Frame
	rec, err := reel.Record(t.Context(), s, reel.Profile{
		Outputs:    reel.Files(filepath.Join(t.TempDir(), "out.gif")),
		Rasterizer: testutil.Rasterizer{},
		FPS:        100,
		Effects:    []reel.Effect{Redact(Text("hunter2"), Between("secret", "")), recordFrames{&frames}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range []string{"pw", "secret", " hunter2", "\r\nhunter2", "!"} {
		if step == "secret" {
			err = s.Mark(step)
		} else {
			_, err = s.Write([]byte(step))
		}
		if err != nil {
			t.Fatal(i, err)
		}
		time.Sleep(30 * ms)
	}
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(frames) < 3 {
		t.Fatalf("%d frames", len(frames))
	}
	for i, f := range frames {
		if text := f.Cells.Screen.Text(); strings.Contains(text, "hun") {
			t.Errorf("frame %d shows %q", i, text)
		}
		if testutil.Text(f.Image) != testutil.Text(drawCells(f.Cells)) {
			t.Errorf("frame %d does not show its cells", i)
		}
	}
}

// TestRedactUnreachedMarks checks an export of a timeline that never
// reached a mark of [Between], as a test that failed early leaves: without
// its to mark the secret stays hidden to the end, and without its from mark
// the whole recording is, so a secret never shows, and either way the
// export is written and reports the mark.
func TestRedactUnreachedMarks(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 12, 1, "", output(0, "pw"), mark(100, "secret"), output(200, " hunter2"), output(300, "!"))
	for _, tc := range []struct {
		from, to string
		hidden   bool
	}{
		{"secret", "nope", true},
		{"nope", "", true},
	} {
		var frames []reel.Frame
		err := reel.Export(t.Context(), tl, reel.Profile{
			Outputs:    reel.Files(filepath.Join(t.TempDir(), "out.gif")),
			Rasterizer: testutil.Rasterizer{},
			FPS:        100,
			Effects:    []reel.Effect{Redact(Text("hunter2"), Between(tc.from, tc.to)), recordFrames{&frames}},
		})
		if labels, rest := reel.MissingMarks(err); rest != nil || !slices.Equal(labels, []string{"nope"}) {
			t.Errorf("between %q and %q: Export = %v, want a NoMarkError for \"nope\"", tc.from, tc.to, err)
		}
		last := frames[len(frames)-1]
		if shown := strings.Contains(testutil.Text(last.Image), "hunter2"); shown == tc.hidden {
			t.Errorf("between %q and %q: the last frame shows the secret: %v, want %v", tc.from, tc.to, shown, !tc.hidden)
		}
	}
}

// TestRedactStills checks that the stills of an export and of a live
// recording are redacted when the step of playback they show covers any of
// the window of [Between], as a frame of the recording is, and that only
// the redacting effects run on them. In the live recording, the window's
// from mark is appended just after the still before it is drawn.
func TestRedactStills(t *testing.T) {
	t.Parallel()
	paint := redactorFunc(func(f reel.Frame, emit func(reel.Frame)) {
		f.Image = image.NewNRGBA(f.Image.Rect)
		emit(f)
	})
	labels := []string{"before", "near", "secret", "done", "after"}
	profile := func(dir string, recording bool, between ...string) reel.Profile {
		p := reel.Profile{
			Outputs:    []reel.File{{Path: filepath.Join(dir, "{mark}.png"), Marks: labels}},
			Rasterizer: testutil.Rasterizer{},
			Effects:    []reel.Effect{Redact(Text("hunter2"), Between(between[0], between[1])), Keys(time.Second), cardOf{paint}},
		}
		if recording {
			p.Outputs = append(p.Outputs, reel.File{Path: filepath.Join(dir, "out.gif")})
		}
		return p
	}
	want := map[string]string{"before": "pw hunter2", "near": "pw", "secret": "pw", "done": "pw", "after": "pw hunter2"}
	check := func(name, dir string) {
		t.Helper()
		for label, text := range want {
			got := testutil.Text(readPNG(t, filepath.Join(dir, label+".png")))
			if strings.TrimSpace(got) != text {
				t.Errorf("%s: still %q shows %q, want %q", name, label, got, text)
			}
		}
	}

	// The step is 50ms.
	dir := t.TempDir()
	tl := buildTimeline(t, 10, 1, "", output(0, "pw hunter2"),
		mark(100, "before"), mark(190, "near"), mark(200, "secret"), mark(300, "done"), mark(400, "after"))
	if err := reel.Export(t.Context(), tl, profile(dir, true, "secret", "done")); err != nil {
		t.Fatal(err)
	}
	check("export", dir)

	for _, recording := range []bool{true, false} {
		name := fmt.Sprintf("record (recording %v)", recording)
		record := func(dir string, between ...string) error {
			s, err := reel.Open(t.Context(), 10, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			rec, err := reel.Record(t.Context(), s, profile(dir, recording, between...))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Write([]byte("pw hunter2")); err != nil {
				t.Fatal(err)
			}
			// end comes after the last still.
			for _, label := range append(labels, "end") {
				if label == "secret" {
					time.Sleep(5 * ms)
				} else {
					time.Sleep(100 * ms)
				}
				if err = s.Mark(label); err != nil {
					t.Fatal(err)
				}
			}
			return rec.Stop()
		}
		dir = t.TempDir()
		if err := record(dir, "secret", "done"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		check(name, dir)
		// A mark after the last still is found, and one that never comes
		// is reported once.
		if err := record(t.TempDir(), "secret", "end"); err != nil {
			t.Errorf("%s: Stop = %v", name, err)
		}
		err := record(t.TempDir(), "nope", "")
		if !errors.Is(err, reel.ErrNoMark) || strings.Count(err.Error(), "nope") != 2 {
			t.Errorf("%s: Stop = %v, want one error wrapping reel.ErrNoMark", name, err)
		}
	}
}

// cardOf is an effect that does not redact, whose stage is its func.
type cardOf struct{ stage redactorFunc }

func (e cardOf) Start(*reel.Context) (reel.Stage, error) { return reel.Stage(e.stage), nil }

// drawCells draws cf with a zero fake rasterizer.
func drawCells(cf *reel.CellFrame) *image.NRGBA {
	r := testutil.Rasterizer{}
	b := cf.Screen.Bounds()
	img := image.NewNRGBA(r.Layout(b.Dx(), b.Dy()).Image)
	r.Draw(img, cf.Screen, cf.State, cf.At)
	return img
}
