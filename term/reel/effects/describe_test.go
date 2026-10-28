// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/vt"
)

// describeTimeline is a timeline with text, keys typed, and marks for the
// effects of the describe tests to work on.
func describeTimeline(t *testing.T) *reel.Timeline {
	t.Helper()
	return buildTimeline(t, 20, 4, "demo",
		output(0, "$ hunter2 secret\r\n"),
		input(100, "ab\r"),
		mark(200, "a"),
		output(300, "line two\r\n"),
		input(400, "hunter2\r"),
		mark(600, "b"),
		output(900, "done"),
	)
}

// customRegion is a region of a caller's own, which has no description.
type customRegion struct{}

func (customRegion) Rects(*reel.CellFrame) []image.Rectangle { return nil }

// describe returns the description of e, which must be a [reel.Describer].
func describeEffect(t *testing.T, e reel.Effect) (reel.Annotation, error) {
	t.Helper()
	d, ok := e.(reel.Describer)
	if !ok {
		t.Fatalf("%T is not a reel.Describer", e)
	}
	return d.Describe()
}

// TestDescribeRoundTrip checks that every effect, with every option and
// region, describes itself as the annotation it should, and that the effect
// read back from it describes itself the same, and draws the same frames.
func TestDescribeRoundTrip(t *testing.T) {
	t.Setenv("REEL_TEST_SECRET", "hunter2")
	re := regexp.MustCompile(`hun\w+`)
	keys := Keys(500 * ms)
	for _, tc := range []struct {
		name   string
		effect reel.Effect
		// after are effects run after it, which show what it does.
		after []reel.Effect
		// want is the description, or empty to only check the round trip.
		want string
	}{
		{name: "keys", effect: Keys(500 * ms), want: `{"effect":"keys","shown_ms":500}`},
		{name: "keys off", effect: Keys(0), want: `{"effect":"keys","shown_ms":0}`},
		{name: "intro title", effect: Intro(300 * ms), want: `{"effect":"intro","duration_ms":300}`},
		{
			name: "intro lines",
			effect: Intro(300*ms,
				Line{Text: "hi", Style: vt.Style{
					Fg:             vt.Indexed(3),
					Bg:             color.NRGBA{R: 1, G: 2, B: 3, A: 0xff},
					UnderlineColor: color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0x80},
					Underline:      vt.UnderlineCurly,
					Attrs:          vt.AttrBold | vt.AttrItalic | vt.AttrStrikethrough,
				}},
				Line{Text: "there", Style: vt.Style{Fg: vt.Indexed(200)}},
				Line{},
			),
			want: `{"effect":"intro","duration_ms":300,"lines":[` +
				`{"text":"hi","style":{"fg":3,"bg":"#010203","underline_color":"#aabbcc80","underline":"curly","attrs":["bold","italic","strikethrough"]}},` +
				`{"text":"there","style":{"fg":200}},` +
				`{"text":""}]}`,
		},
		{name: "pause default", effect: Pause(), want: `{"effect":"pause"}`},
		{name: "pause end second", effect: Pause(PauseAtEnd(1000 * ms)), want: `{"effect":"pause"}`},
		{
			name: "pause full",
			effect: Pause(
				PauseAtEnd(300*ms),
				PauseAtMarks(200*ms, "a"),
				PauseAtMarks(100*ms),
				PauseAtMatch(150*ms, regexp.MustCompile(`^b$`)),
				WithBadgeCorner(BottomLeft),
				WithBadgeSize(12),
				WithBadgeColor(color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}),
			),
			want: `{"effect":"pause","points":[` +
				`{"at":"end","duration_ms":300},` +
				`{"at":"marks","duration_ms":200,"labels":["a"]},` +
				`{"at":"marks","duration_ms":100},` +
				`{"at":"match","duration_ms":150,"pattern":"^b$"}],` +
				`"badge_corner":"bottom_left","badge_size_px":12,"badge_color":"#102030"}`,
		},
		{
			name:   "pause without badge",
			effect: Pause(WithoutBadge(), WithBadgeCorner(BottomRight)),
			want:   `{"effect":"pause","badge":false}`,
		},
		{
			name:   "highlight default",
			effect: Highlight(Text("hunter2")),
			want:   `{"effect":"highlight","region":{"type":"text","text":"hunter2"}}`,
		},
		{
			name: "highlight full",
			effect: Highlight(Cells(image.Rect(5, 2, 1, 0)),
				DimOthers(0.25), Outline(color.NRGBA{R: 0xff, A: 0xc8}, 3), Padding(2), Between("a", "b"), Ease(100*ms)),
			want: `{"effect":"highlight","region":{"type":"cells","x0":1,"y0":0,"x1":5,"y1":2},` +
				`"timing":{"between":{"from":"a","to":"b"},"ease_in_ms":100,"ease_out_ms":100},` +
				`"dim":0.25,"outline":{"color":"#ff0000c8","width_px":3},"padding_px":2}`,
		},
		{
			name:   "highlight default outline",
			effect: Highlight(Row(1), Outline(nil, 0), Padding(0), DimOthers(0), EaseIn(50*ms), EaseOut(70*ms)),
			want: `{"effect":"highlight","region":{"type":"rows","from":1,"to":2},` +
				`"timing":{"ease_in_ms":50,"ease_out_ms":70},"dim":0,"outline":{},"padding_px":0}`,
		},
		{name: "zoom default", effect: Zoom(Rows(2, 0)), want: `{"effect":"zoom","region":{"type":"rows","from":0,"to":2}}`},
		{
			name:   "zoom full",
			effect: Zoom(Regexp(re), Scale(2), Anchor(0, 1), Between("", "b")),
			want: `{"effect":"zoom","region":{"type":"regexp","pattern":"hun\\w+"},` +
				`"timing":{"between":{"to":"b"}},"scale":2,"anchor":{"x":0,"y":1}}`,
		},
		{
			name:   "highlight env",
			effect: Highlight(Env("REEL_TEST_SECRET"), Between("a", "")),
			want:   `{"effect":"highlight","region":{"type":"env","names":["REEL_TEST_SECRET"]},"timing":{"between":{"from":"a"}}}`,
		},
		{
			name:   "highlight any",
			effect: Highlight(Any(Text("hunter2"), Regexp(re), Cells(image.Rect(0, 3, 2, 4)), Row(2), Env("REEL_TEST_SECRET", "REEL_UNSET"))),
			after:  []reel.Effect{keys},
			want: `{"effect":"highlight","region":{"type":"any","regions":[` +
				`{"type":"text","text":"hunter2"},{"type":"regexp","pattern":"hun\\w+"},` +
				`{"type":"cells","x0":0,"y0":3,"x1":2,"y1":4},{"type":"rows","from":2,"to":3},` +
				`{"type":"env","names":["REEL_TEST_SECRET","REEL_UNSET"]}]}}`,
		},
		{
			name:   "zoom env without names",
			effect: Zoom(Env()),
			want:   `{"effect":"zoom","region":{"type":"env","names":[]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := describeEffect(t, tc.effect)
			if err != nil {
				t.Fatal(err)
			}
			if a.Kind != reel.EffectKind {
				t.Fatalf("kind %q, want %q", a.Kind, reel.EffectKind)
			}
			if tc.want != "" && string(a.Data) != tc.want {
				t.Fatalf("description:\n%s\nwant:\n%s", a.Data, tc.want)
			}
			decoded, err := FromAnnotation(a)
			if err != nil {
				t.Fatal(err)
			}
			again, err := describeEffect(t, decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again.Data, a.Data) {
				t.Fatalf("read back, described as:\n%s\nwant:\n%s", again.Data, a.Data)
			}

			tl := describeTimeline(t)
			want := exportFrames(t, tl, reel.Profile{Effects: append([]reel.Effect{tc.effect}, tc.after...)})
			got := exportFrames(t, tl, reel.Profile{Effects: append([]reel.Effect{decoded}, tc.after...)})
			assertSameFrames(t, got, want)
		})
	}
}

// assertSameFrames checks that got and want play the same images for the
// same delays.
func assertSameFrames(t *testing.T, got, want []reel.Frame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d frames, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Delay != want[i].Delay || got[i].At != want[i].At {
			t.Fatalf("frame %d at %v for %v, want at %v for %v", i, got[i].At, got[i].Delay, want[i].At, want[i].Delay)
		}
		if got[i].Image.Rect != want[i].Image.Rect || !bytes.Equal(got[i].Image.Pix, want[i].Image.Pix) {
			t.Fatalf("frame %d differs", i)
		}
	}
}

// TestDescribeANSIColors checks that the palette colors of the ansi package
// are described by their index, and read back as [vt.Indexed].
func TestDescribeANSIColors(t *testing.T) {
	t.Parallel()
	a, err := describeEffect(t, Intro(ms, Line{Text: "x", Style: vt.Style{Fg: ansi.IndexedColor(200), Bg: ansi.BasicColor(4)}}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"fg":200,"bg":4}}]}`; string(a.Data) != want {
		t.Fatalf("description %s, want %s", a.Data, want)
	}
	e, err := FromAnnotation(a)
	if err != nil {
		t.Fatal(err)
	}
	in, ok := e.(intro)
	if !ok || len(in.lines) != 1 {
		t.Fatalf("read back as %#v", e)
	}
	if st := in.lines[0].Style; st.Fg != vt.Indexed(200) || st.Bg != vt.Indexed(4) {
		t.Fatalf("read back as %#v and %#v", st.Fg, st.Bg)
	}
}

func TestDescribeUnsupported(t *testing.T) {
	t.Parallel()
	for name, e := range map[string]reel.Effect{
		"highlight": Highlight(customRegion{}),
		"zoom any":  Zoom(Any(Text("x"), customRegion{})),
	} {
		if _, err := describeEffect(t, e); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: Describe = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestDescribeInvalid(t *testing.T) {
	t.Parallel()
	for name, e := range map[string]reel.Effect{
		"nil region":       Highlight(nil),
		"nil regexp":       Zoom(Regexp(nil)),
		"dim":              Highlight(Text("x"), DimOthers(2)),
		"negative ease":    Zoom(Text("x"), EaseIn(-10*ms)),
		"pause nil regexp": Pause(PauseAtMatch(ms, nil)),
		"pause negative":   Pause(PauseAtMarks(-ms)),
		"pause corner":     Pause(WithBadgeCorner(Corner(9))),
		"badge size":       Pause(WithBadgeSize(4)),
		"underline":        Intro(ms, Line{Text: "x", Style: vt.Style{Underline: 99}}),
		"anchor":           Zoom(Text("x"), Anchor(2, 0)),
	} {
		_, err := describeEffect(t, e)
		if err == nil || errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: Describe = %v, want an error", name, err)
		}
	}
}

func TestFromAnnotationErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		kind, data, want string
	}{
		"kind":          {"test.effect", `{"effect":"keys","shown_ms":1}`, "not \"reel.effect\""},
		"effect":        {reel.EffectKind, `{"effect":"sparkle"}`, `unknown effect "sparkle"`},
		"no effect":     {reel.EffectKind, ``, `unknown effect ""`},
		"no region":     {reel.EffectKind, `{"effect":"zoom"}`, "no region"},
		"region type":   {reel.EffectKind, `{"effect":"zoom","region":{"type":"oval"}}`, `unknown region type "oval"`},
		"nested region": {reel.EffectKind, `{"effect":"zoom","region":{"type":"any","regions":[{"type":"oval"}]}}`, `unknown region type "oval"`},
		"pattern":       {reel.EffectKind, `{"effect":"zoom","region":{"type":"regexp","pattern":"("}}`, "missing closing )"},
		"pause pattern": {reel.EffectKind, `{"effect":"pause","points":[{"at":"match","duration_ms":1}]}`, "without a pattern"},
		"pause at":      {reel.EffectKind, `{"effect":"pause","points":[{"at":"start","duration_ms":1}]}`, `unknown pause point "start"`},
		"corner":        {reel.EffectKind, `{"effect":"pause","badge_corner":"middle"}`, `unknown badge corner "middle"`},
		"color":         {reel.EffectKind, `{"effect":"pause","badge_color":"red"}`, `color "red"`},
		"short color":   {reel.EffectKind, `{"effect":"pause","badge_color":"#fff"}`, `color "#fff"`},
		"index color":   {reel.EffectKind, `{"effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"fg":256}}]}`, "palette index"},
		"underline":     {reel.EffectKind, `{"effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"underline":"wavy"}}]}`, `unknown underline style "wavy"`},
		"attr":          {reel.EffectKind, `{"effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"attrs":["loud"]}}]}`, `unknown attribute "loud"`},
		// The effects that hide secrets are never described.
		"redact":        {reel.EffectKind, `{"effect":"redact","region":{"type":"text","text":"x"}}`, `unknown effect "redact"`},
		"redact inputs": {reel.EffectKind, `{"effect":"redact_inputs","region":{"type":"text","text":"x"}}`, `unknown effect "redact_inputs"`},
		// A field a newer reel may write is not applied without.
		"field":        {reel.EffectKind, `{"effect":"keys","shown_ms":1,"fade_ms":1}`, `"fade_ms"`},
		"region field": {reel.EffectKind, `{"effect":"zoom","region":{"type":"text","text":"x","pattern":"y"}}`, `"pattern"`},
		"timing field": {reel.EffectKind, `{"effect":"zoom","region":{"type":"text","text":"x"},"timing":{"ease_ms":1}}`, `"ease_ms"`},
		"line field":   {reel.EffectKind, `{"effect":"intro","duration_ms":1,"lines":[{"text":"x","align":"left"}]}`, `"align"`},
		"badge field":  {reel.EffectKind, `{"effect":"pause","badge_shape":"square"}`, `"badge_shape"`},
	} {
		a := reel.Annotation{Kind: tc.kind, Data: jsontext.Value(tc.data)}
		if _, err := FromAnnotation(a); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: FromAnnotation = %v, want an error containing %q", name, err, tc.want)
		}
	}
}

// TestExportCastEffects checks that an asciicast output of an export holds
// the effects of its profile that describe themselves, that the effects
// read back from the file draw the same frames, and that exporting the file
// again with them writes the same file.
func TestExportCastEffects(t *testing.T) {
	t.Parallel()
	for _, v := range []reel.CastVersion{reel.CastV2, reel.CastV3} {
		dir := t.TempDir()
		tl := describeTimeline(t)
		effects := []reel.Effect{
			Zoom(Text("hunter2"), Scale(2), Between("a", "b")),
			Highlight(customRegion{}),
			Highlight(Text("line"), Outline(nil, 2)),
			Keys(500 * ms),
			Intro(200 * ms),
			Pause(PauseAtMarks(100*ms, "b")),
		}
		first := filepath.Join(dir, "first.cast")
		p := reel.Profile{
			Outputs:    []reel.File{{Path: first, Cast: v}},
			Rasterizer: testutil.Rasterizer{},
			Effects:    effects,
		}
		if err := reel.Export(t.Context(), tl, p); err != nil {
			t.Fatal(err)
		}
		cast := readCastFile(t, first)
		read, err := FromTimeline(cast)
		if err != nil {
			t.Fatal(err)
		}
		if len(read) != len(effects)-1 {
			t.Fatalf("v%d: read %d effects, want %d", v, len(read), len(effects)-1)
		}
		for a := range cast.Annotations() {
			if a.At() != 0 {
				t.Fatalf("v%d: annotation at %v, want 0", v, a.At())
			}
		}
		described := slices.Delete(slices.Clone(effects), 1, 2)
		for i, e := range read {
			want, _ := describeEffect(t, described[i])
			got, _ := describeEffect(t, e)
			if !bytes.Equal(got.Data, want.Data) {
				t.Fatalf("v%d: effect %d read back as %s, want %s", v, i, got.Data, want.Data)
			}
		}
		assertSameFrames(t,
			exportFrames(t, cast, reel.Profile{Effects: read}),
			exportFrames(t, tl, reel.Profile{Effects: described}),
		)

		second := filepath.Join(dir, "second.cast")
		p.Outputs[0].Path, p.Effects = second, read
		if err = reel.Export(t.Context(), cast, p); err != nil {
			t.Fatal(err)
		}
		a, _ := os.ReadFile(first)
		b, _ := os.ReadFile(second)
		if !bytes.Equal(a, b) {
			t.Fatalf("v%d: exporting the file with its effects wrote:\n%s\nwant:\n%s", v, b, a)
		}
	}
}

// TestCheckMarks checks that the effects that look for marks report those a
// cut does not have, which an export of the cut reports as it writes an
// asciicast output without their descriptions, and that the file exports
// again with the effects it has.
func TestCheckMarks(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 2, 1, "",
		mark(0, "a"),
		output(1, "x"),
		mark(2, "b"),
		output(3, "y"),
		mark(4, "c"),
	)
	cut, err := tl.Between("b", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		effect reel.Effect
		ok     bool
	}{
		"between":       {Zoom(Rows(0, 1), Between("b", "c")), true},
		"to":            {Highlight(Rows(0, 1), Between("", "c")), true},
		"no marks":      {Highlight(Rows(0, 1)), true},
		"from outside":  {Zoom(Rows(0, 1), Between("a", "c")), false},
		"to outside":    {Highlight(Rows(0, 1), Between("", "a")), false},
		"pause":         {Pause(PauseAtMarks(ms, "b", "c")), true},
		"pause outside": {Pause(PauseAtMarks(ms, "c", "a")), false},
		"pause match":   {Pause(PauseAtMatch(ms, regexp.MustCompile("a"))), true},
		"pause end":     {Pause(), true},
	} {
		mc, ok := tc.effect.(reel.MarkChecker)
		if !ok {
			t.Fatalf("%s: %T is not a reel.MarkChecker", name, tc.effect)
		}
		if err := mc.CheckMarks(tl); err != nil {
			t.Errorf("%s: CheckMarks of the whole timeline = %v", name, err)
		}
		path := filepath.Join(t.TempDir(), "a.cast")
		p := reel.Profile{Outputs: reel.MustFiles(path), Effects: []reel.Effect{tc.effect}}
		err := reel.Export(t.Context(), cut, p)
		if !tc.ok {
			if cerr := mc.CheckMarks(cut); !errors.Is(cerr, reel.ErrNoMark) {
				t.Errorf("%s: CheckMarks = %v, want ErrNoMark", name, cerr)
			}
			if labels, rest := reel.MissingMarks(err); rest != nil || !slices.Equal(labels, []string{"a"}) {
				t.Errorf("%s: Export = %v, want a NoMarkError for \"a\"", name, err)
			}
			if read, rerr := FromTimeline(readCastFile(t, path)); rerr != nil || len(read) != 0 {
				t.Errorf("%s: the file describes %d effects (%v), want none", name, len(read), rerr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		read, err := FromTimeline(readCastFile(t, path))
		if err != nil {
			t.Fatal(err)
		}
		p.Outputs[0].Path, p.Effects = filepath.Join(t.TempDir(), "a.cast"), read
		if err = reel.Export(t.Context(), readCastFile(t, path), p); err != nil {
			t.Errorf("%s: exporting the file again: %v", name, err)
		}
	}
}

// readCastFile reads the timeline of the asciicast file at path.
func readCastFile(t *testing.T, path string) *reel.Timeline {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(f)
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestFromTimeline(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(t, 4, 1, "",
		event{0, "R", `{"kind":"reel.effect","effect":"keys","shown_ms":100}`},
		event{0, "R", `{"kind":"test.note","n":1}`},
		output(10, "a"),
		event{20, "R", `{"kind":"reel.effect","effect":"pause","badge":false}`},
	)
	got, err := FromTimeline(tl)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("FromTimeline = %d effects, want 2", len(got))
	}
	if _, ok := got[0].(keys); !ok {
		t.Errorf("effect 0 is %T, want keys", got[0])
	}
	if p, ok := got[1].(pause); !ok || !p.noBadge {
		t.Errorf("effect 1 is %#v, want a pause without a badge", got[1])
	}

	empty, err := FromTimeline(buildTimeline(t, 4, 1, "", output(0, "a")))
	if err != nil || empty != nil {
		t.Fatalf("FromTimeline of no annotations = %v, %v", empty, err)
	}
}
