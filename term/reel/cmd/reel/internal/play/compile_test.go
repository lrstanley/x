// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lrstanley/x/term/reel"
	syntax "github.com/lrstanley/x/term/tape"
)

// describe is a step as the tests compare it.
func describe(s step) string {
	if w, ok := s.(waitStep); ok {
		re := ""
		if w.re != nil {
			re = w.re.String()
		}
		return fmt.Sprintf("wait screen=%v timeout=%v re=%q", w.screen, w.timeout, re)
	}
	return fmt.Sprintf("%T%+v", s, s)
}

func describeSteps(t *Tape) []string {
	var out []string
	for _, l := range t.steps {
		out = append(out, describe(l.step))
	}
	return out
}

func parseString(src string, opts ...CompileOption) (*Tape, error) {
	return Parse(strings.NewReader(src), opts...)
}

// TestParseCommands covers every command: the steps each parses to, and
// the errors of the ones rejected.
func TestParseCommands(t *testing.T) {
	const def = defaultTypingSpeed
	tests := []struct {
		src  string
		want []string
		// err is part of the error, with its position.
		err string
	}{
		{src: `Type "hello"`, want: []string{describe(typeStep{text: "hello", delay: def})}},
		{src: `Type@100ms 'a' ` + "`b`" + ` c`, want: []string{describe(typeStep{text: "a b c", delay: 100 * time.Millisecond})}},
		{src: `Type@.5 "x"`, want: []string{describe(typeStep{text: "x", delay: 500 * time.Millisecond})}},
		{src: `Type Enter`, err: "1:1: Type expects a string"},
		{src: `Type "open`, err: "1:6: unterminated string"},
		{src: `Sleep 1`, want: []string{describe(sleepStep{d: time.Second})}},
		{src: `Sleep 500ms`, want: []string{describe(sleepStep{d: 500 * time.Millisecond})}},
		{src: `Sleep 1.5s`, want: []string{describe(sleepStep{d: 1500 * time.Millisecond})}},
		{src: `Sleep 2m`, want: []string{describe(sleepStep{d: 2 * time.Minute})}},
		{src: `Sleep soon`, err: `1:7: Sleep: "soon" is not a duration`},
		{src: `Enter`, want: []string{describe(keyStep{key: "enter", delay: def, repeat: 1})}},
		{src: `Backspace@1 3`, want: []string{describe(keyStep{key: "backspace", delay: time.Second, repeat: 3})}},
		{src: `Escape 2`, want: []string{describe(keyStep{key: "esc", delay: def, repeat: 2})}},
		{
			src: "Delete Insert Tab Space Up Down Left Right PageUp PageDown Home End",
			want: []string{
				describe(keyStep{key: "delete", delay: def, repeat: 1}),
				describe(keyStep{key: "insert", delay: def, repeat: 1}),
				describe(keyStep{key: "tab", delay: def, repeat: 1}),
				describe(keyStep{key: "space", delay: def, repeat: 1}),
				describe(keyStep{key: "up", delay: def, repeat: 1}),
				describe(keyStep{key: "down", delay: def, repeat: 1}),
				describe(keyStep{key: "left", delay: def, repeat: 1}),
				describe(keyStep{key: "right", delay: def, repeat: 1}),
				describe(keyStep{key: "pgup", delay: def, repeat: 1}),
				describe(keyStep{key: "pgdown", delay: def, repeat: 1}),
				describe(keyStep{key: "home", delay: def, repeat: 1}),
				describe(keyStep{key: "end", delay: def, repeat: 1}),
			},
		},
		{src: `Enter 0`, err: "1:7: a repeat count must be at least 1, got 0"},
		{src: `Ctrl+C`, want: []string{describe(keyStep{key: "ctrl+c", repeat: 1})}},
		{src: `Ctrl+Alt+Shift+O`, want: []string{describe(keyStep{key: "ctrl+alt+shift+o", repeat: 1})}},
		{src: `Ctrl+[`, want: []string{describe(keyStep{key: "ctrl+[", repeat: 1})}},
		{src: `Ctrl+Enter`, want: []string{describe(keyStep{key: "ctrl+enter", repeat: 1})}},
		{src: `Ctrl+A+B`, err: "1:8: Ctrl takes one key"},
		{src: `Ctrl`, err: "1:1: expected Ctrl+<key>"},
		{src: `Alt+.`, want: []string{describe(keyStep{key: "alt+.", repeat: 1})}},
		{src: `Alt+Enter`, want: []string{describe(keyStep{key: "alt+enter", repeat: 1})}},
		{src: `Alt+ab`, want: []string{describe(keyStep{key: "alt+a", repeat: 1}), describe(keyStep{key: "alt+b", repeat: 1})}},
		{src: `Shift+Tab`, want: []string{describe(keyStep{key: "shift+tab", repeat: 1})}},
		{src: `Shift+A`, want: []string{describe(keyStep{key: "shift+a", repeat: 1})}},
		{src: `Shift+[`, err: "1:1: vt: key \"shift+[\": name the shifted character instead"},
		{src: `ScrollUp 3`, want: []string{describe(scrollStep{up: true, delay: def, repeat: 3})}},
		{src: `ScrollDown@10ms`, want: []string{describe(scrollStep{delay: 10 * time.Millisecond, repeat: 1})}},
		{src: `Wait`, want: []string{describe(waitStep{timeout: defaultWaitTimeout, re: defaultWaitPattern})}},
		{src: `Wait+Screen@10s /World/`, want: []string{describe(waitStep{screen: true, timeout: 10 * time.Second, re: regexpOf("World")})}},
		{src: `Wait+Line /a\/b/`, want: []string{describe(waitStep{timeout: defaultWaitTimeout, re: regexpOf(`a\/b`)})}},
		{src: `Wait+Pixel`, err: "1:6: Wait+ expects Line or Screen"},
		{src: `Wait@0 /x/`, err: "1:6: Wait expects a positive timeout"},
		{src: `Wait /(/`, err: "1:6: Wait: error parsing regexp"},
		{src: `Copy "a" "b"` + "\nPaste", want: []string{describe(copyStep{text: "a b"}), describe(pasteStep{})}},
		{src: `Mark "step:1"`, want: []string{describe(markStep{label: "step:1"})}},
		{src: `Mark "tape:end"`, err: `1:1: mark label "tape:end" is empty or starts with "tape:"`},
		{src: `Screenshot shot.png`, want: []string{describe(markStep{label: "shot"})}},
		{src: `Screenshot shot.jpg`, err: `1:1: screenshot "shot.jpg" is not a .png file`},
		{src: `Screenshot "{name}.png"`, err: `contains "{"`},
		{src: `Screenshot "a:b.png"`, err: `contains one of`},
		{src: "Screenshot a/shot.png\nScreenshot b/shot.png", err: `2:1: screenshot file name "shot.png" is already taken at 1:1`},
		{src: "Screenshot shot.png\nMark shot", err: `2:1: Mark "shot" is the label of a Screenshot`},
		{src: "Show\nType a\nHide\nType b\nShow\nType c", want: []string{
			describe(typeStep{text: "a", delay: def}),
			describe(markStep{label: hideMark}),
			describe(typeStep{text: "b", delay: def}),
			describe(markStep{label: showMark}),
			describe(typeStep{text: "c", delay: def}),
		}},
		{src: "Hide\nType a\nShow\nType b\nHide\nHide\nType c", want: []string{
			describe(typeStep{text: "a", delay: def}),
			describe(markStep{label: showMark}),
			describe(typeStep{text: "b", delay: def}),
			describe(markStep{label: hideRestMark}),
			describe(typeStep{text: "c", delay: def}),
		}},
		{src: "Hide\nType a", err: "1:1: Hide is never followed by Show"},
		{src: "Set Theme Nope", err: `1:11: unknown theme "Nope"`},
		{src: `Zoom "row=1;scale=2"` + "\nRedact \"env=X;style=blur\"\nHighlight \"rows=1-2;dim=0.5\"\nType a", want: []string{
			describe(typeStep{text: "a", delay: def}),
		}},
		{src: `Zoom "bogus"`, err: `1:6: Zoom: zoom "bogus": region "bogus" is not KIND=VALUE`},
		{src: "Type a\nSet TypingSpeed 10ms\nType b\nSet WaitTimeout 1s\nWait", want: []string{
			describe(typeStep{text: "a", delay: def}),
			describe(typeStep{text: "b", delay: 10 * time.Millisecond}),
			describe(waitStep{timeout: defaultWaitTimeout, re: defaultWaitPattern}),
		}},
		{src: "Type \"a\"\nMark \"m\"\nHome", want: []string{
			describe(typeStep{text: "a", delay: def}),
			describe(markStep{label: "m"}),
			describe(keyStep{key: "home", delay: def, repeat: 1}),
		}},
		{src: `Sleep 1 2`, err: `1:9: unexpected "2" after Sleep`},
		{src: `Sleep+Fast 1`, err: "1:7: Sleep takes no +Fast"},
		{src: `Hide@1s`, err: "1:6: Hide takes no @1s"},
		{src: `Wait+Line+Screen`, err: "1:11: Wait takes one of +Line or +Screen"},
		{src: `Foo`, err: `1:1: unknown command "Foo"`},
		{src: `"Foo"`, err: `1:1: expected a command, got "Foo"`},
		{src: `Type ~`, err: "1:6: unexpected character '~'"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			tp, err := parseString(tt.src)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want one containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := describeSteps(tp); !slices.Equal(got, tt.want) {
				t.Errorf("steps:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestCompile compiles commands parsed apart from it, with a command of
// WithCommand, which replaces a built-in one too.
func TestCompile(t *testing.T) {
	var got []string
	shout := func(cmd syntax.Command) (Step, error) {
		if len(cmd.Mods) != 1 || len(cmd.Args) != 1 {
			return nil, errors.New("expected Shout+<n> <text>")
		}
		got = append(got, strings.Repeat(strings.ToUpper(cmd.Args[0].Text), len(cmd.Mods[0].Text)))
		return func(context.Context, *reel.Session) error { return nil }, nil
	}
	quiet := func(syntax.Command) (Step, error) {
		return func(context.Context, *reel.Session) error { return nil }, nil
	}
	names := syntax.VHSCommands()
	names["Shout"] = syntax.Keep
	cmds, err := syntax.Parse("", strings.NewReader(`Shout+xx hi Enter Shout+x "a" b`), syntax.WithCommands(names))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(cmds, WithCommand("Shout", shout), WithCommand("Enter", quiet))
	if err == nil || !strings.Contains(err.Error(), "1:19: expected Shout+<n> <text>") {
		t.Errorf("error = %v, want the second Shout's", err)
	}
	if want := []string{"HIHI"}; !slices.Equal(got, want) {
		t.Errorf("Shout compiled %q, want %q", got, want)
	}

	tp, err := parseString("Shout+x hi\nEnter", WithCommand("Shout", shout), WithCommand("Enter", quiet))
	if err != nil {
		t.Fatal(err)
	}
	if steps := describeSteps(tp); len(steps) != 2 || strings.Contains(steps[1], "keyStep") {
		t.Errorf("steps = %q, want Shout's and the replaced Enter's", steps)
	}
}

// TestParseActions skips and fails commands with WithCommands, in a Source
// tape too, and reports them with WithReport.
func TestParseActions(t *testing.T) {
	root := fstest.MapFS{"setup.tape": {Data: []byte("Screenshot setup.png\nType a")}}
	var reported []string
	opts := []CompileOption{
		WithSourceRoot(root),
		WithCommands(func(cmds syntax.Commands) {
			cmds["Screenshot"], cmds["Env"] = syntax.Skip, syntax.Fail
			delete(cmds, "Home")
		}),
		WithReport(func(cmd syntax.Command, action syntax.Action) {
			reported = append(reported, fmt.Sprintf("%s %s %s", cmd.Name.Pos, action, cmd.Name.Text))
		}),
	}
	tp, err := parseString("Source setup.tape\nScreenshot main.png\nType Home", opts...)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		describe(typeStep{text: "a", delay: defaultTypingSpeed}),
		describe(typeStep{text: "Home", delay: defaultTypingSpeed}),
	}
	if got := describeSteps(tp); !slices.Equal(got, want) || len(tp.shots) != 0 {
		t.Errorf("steps = %q, shots = %v, want %q and none", got, tp.shots, want)
	}
	// The main tape is parsed before it compiles, and so before its Source.
	if !slices.Equal(reported, []string{"2:1 skip Screenshot", "setup.tape:1:1 skip Screenshot"}) {
		t.Errorf("reported %q, want both Screenshots", reported)
	}

	_, err = parseString("Env A b", opts...)
	if !errors.Is(err, syntax.ErrDisabled) || !strings.Contains(err.Error(), "1:1: Env: command is disabled") {
		t.Errorf("error = %v, want Env disabled", err)
	}
}

// TestBuiltins checks that every VHS command compiles.
func TestBuiltins(t *testing.T) {
	builtin := builtins()
	for name := range syntax.VHSCommands() {
		if _, ok := builtin[name]; !ok {
			t.Errorf("VHS's %s is not a built-in command", name)
		}
	}
}

// TestParseHide checks which hide rules a tape's Hide and Show commands
// need.
func TestParseHide(t *testing.T) {
	tp, err := parseString("Hide\nType a\nShow\nType b\nHide\nType c\nShow\nType d\nHide\nType e")
	if err != nil {
		t.Fatal(err)
	}
	if !tp.leadingHide || !tp.spans || !tp.hideRest {
		t.Errorf("leadingHide, spans, hideRest = %v, %v, %v, want all set", tp.leadingHide, tp.spans, tp.hideRest)
	}
}

// TestParseOutputs checks Output's formats, including those rejected, and
// that a Source tape's outputs are dropped.
func TestParseOutputs(t *testing.T) {
	tp, err := parseString(`Output demo.gif
Output "demo.mp4"
Output demo.webm
Output demo.webp
Output demo.cast
Output demo.snap
Output "demo.gif?loop=once"
Output "{mark}.png?marks=a"
Screenshot shots/one.png
Screenshot shots/two.png
Screenshot three.png`)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range tp.outputs {
		paths = append(paths, f.Path)
	}
	want := []string{"demo.gif", "demo.mp4", "demo.webm", "demo.webp", "demo.cast", "demo.snap", "demo.gif", "{mark}.png"}
	if !slices.Equal(paths, want) {
		t.Errorf("outputs = %q, want %q", paths, want)
	}
	if len(tp.outputs[6].Options) != 1 {
		t.Errorf("loop=once added %d options, want 1", len(tp.outputs[6].Options))
	}
	shots := []reel.File{
		{Path: filepath.Join("shots", "{mark}.png"), Marks: []string{"one", "two"}},
		{Path: "{mark}.png", Marks: []string{"three"}},
	}
	if fmt.Sprint(tp.shots) != fmt.Sprint(shots) {
		t.Errorf("shots = %v, want %v", tp.shots, shots)
	}

	for src, want := range map[string]string{
		"Output frames/":                "a directory of frames",
		"Output out.txt":                "unsupported output \"out.txt\"",
		"Output out.ascii":              "unsupported output \"out.ascii\"",
		"Output out.png":                "use Screenshot",
		"Output out.bmp":                "reel: output",
		`Output "{mark}.png?match=a:*"`: "use marks=",
		`Output "a.gif?nope=1"`:         `unknown parameter "nope"`,
		"Output":                        "Output expects a string",
		`Output "/tmp/out.gif"`:         "",
		"Output /tmp/out.gif":           `1:8: Output: "tmp" is not a string`,
		`Require "definitely"`:          "",
		"Require":                       "Require expects a string",
		`Env HELLO "world"`:             "",
		`Env HELLO`:                     "Env expects a string",
		"Set Shell bash\nEnv A b":       "",
	} {
		_, err = parseString(src)
		if want == "" {
			if err != nil {
				t.Errorf("%q: %v", src, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error = %v, want one containing %q", src, err, want)
		}
	}
}

// TestParseSource checks Source: its commands are read in place, its
// outputs dropped, and a nested Source is an error.
func TestParseSource(t *testing.T) {
	root := fstest.MapFS{
		"setup.tape":  {Data: []byte("Output dropped.gif\nSet FontSize 30\nType \"from setup\"")},
		"nested.tape": {Data: []byte("Source setup.tape")},
		"empty.tape":  {Data: nil},
	}
	tp, err := parseString("Output main.gif\nSource setup.tape\nEnter", WithSourceRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(tp.outputs) != 1 || tp.outputs[0].Path != "main.gif" {
		t.Errorf("outputs = %v, want main.gif alone", tp.outputs)
	}
	if tp.settings.FontSize == nil || *tp.settings.FontSize != 30 {
		t.Errorf("FontSize = %v, want the sourced 30", tp.settings.FontSize)
	}
	want := []string{describe(typeStep{text: "from setup", delay: defaultTypingSpeed}), describe(keyStep{key: "enter", delay: defaultTypingSpeed, repeat: 1})}
	if got := describeSteps(tp); !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}

	for src, want := range map[string]string{
		"Source nested.tape":        "nested.tape:1:1: a sourced tape cannot Source another",
		"Source missing.tape":       "1:1: reading the Source tape: open missing.tape",
		"Source setup.txt":          "can only Source a .tape file",
		"Source empty.tape":         "is empty",
		`Source setup.tape "extra"`: `1:19: unexpected "extra" after Source`,
	} {
		_, err = parseString(src, WithSourceRoot(root))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error = %v, want one containing %q", src, err, want)
		}
	}
}

// TestParseSettings checks each setting's value, defaults, and warnings.
func TestParseSettings(t *testing.T) {
	tp, err := parseString(`
Set Shell zsh
Set Columns 100
Set Rows 30
Set Width 800
Set Height 400
Set FontSize 18
Set FontFamily "Fira Code, JetBrains Mono"
Set LetterSpacing 1.5
Set LineHeight 1.2
Set TypingSpeed 20ms
Set Theme { "background": "#000", "foreground": "#ffffff", "purple": "#aa7ff0", "brightBlack": "#535178", "cursorColor": "#b3b0d6", "name": "x" }
Set Padding 0
Set Margin 10
Set MarginFill "#674EFF"
Set BorderRadius 8
Set WindowBar Rings
Set WindowBarSize 40
Set Framerate 30
Set PlaybackSpeed 2
Set WaitTimeout 3s
Set WaitPattern /\$ $/
Set CursorBlink false
Set Title "demo"
Set Scrollbar true
Set PromptMarks true
Set LinkUnderline true
Set Bell true
Set KeyOverlay true
Set Toasts true
Type "x"
Set FontSize 40
`)
	if err != nil {
		t.Fatal(err)
	}
	s := tp.settings
	got := fmt.Sprintf("%s %dx%d %dx%d %v %q %v %v %v %d %d %d %v %d %v %v %v %v %q %v %v %v %v %v %v",
		s.shell, s.cols, s.rows, s.width, s.height, *s.FontSize, *s.FontFamily, *s.LetterSpacing, *s.LineHeight,
		s.typingSpeed, *s.Padding, *s.Margin, *s.BorderRadius, *s.WindowBar, *s.FPS, *s.Speed, s.waitTimeout,
		s.waitPattern, *s.MarginFill, *s.Title, *s.Scrollbar, *s.PromptMarks, *s.LinkUnderline, *s.Bell, *s.KeyOverlay, *s.Toasts)
	want := `zsh 100x30 800x400 18 "Fira Code, JetBrains Mono" 1.5 1.2 20ms 0 10 8 true 30 2 3s \$ $ #674EFF "demo" true true true true true true`
	if got != want {
		t.Errorf("settings:\n got %s\nwant %s", got, want)
	}
	pal, themed, err := s.Palette()
	if err != nil || !themed {
		t.Fatalf("palette from the JSON theme: %v, %v", pal, err)
	}
	for name, c := range map[string]color.Color{
		"background": pal.DefaultBackground, "foreground": pal.DefaultForeground,
		"magenta": pal.Indexed[5], "bright black": pal.Indexed[8], "cursor": pal.Cursor,
	} {
		if c == nil {
			t.Errorf("theme %s is unset", name)
		}
	}
	if fmt.Sprint(pal.DefaultBackground) != "{0 0 0 255}" {
		t.Errorf("background = %v, want #000 as black", pal.DefaultBackground)
	}
	warnings := strings.Join(tp.warnings, "\n")
	for _, want := range []string{
		"17:5: WindowBar Rings is drawn as Colorful",
		"18:5: WindowBarSize is ignored",
		"23:5: CursorBlink false is ignored",
		"32:5: Set FontSize after the first command is ignored",
	} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings %q lack %q", warnings, want)
		}
	}

	defaults, err := parseString(`Set Theme "Catppuccin Mocha"`)
	if err != nil {
		t.Fatal(err)
	}
	if d := defaults.settings; d.width != 1200 || d.height != 600 || d.FontSize != nil || *d.Padding != 60 ||
		d.typingSpeed != 50*time.Millisecond || d.waitTimeout != 15*time.Second || d.waitPattern.String() != ">$" ||
		*d.Theme != "Catppuccin Mocha" {
		t.Errorf("defaults = %+v, want VHS's, with the theme name", d)
	}

	for src, want := range map[string]string{
		"Set LoopOffset 20%":         "1:5: LoopOffset is not supported",
		"Set Foo 1":                  `1:5: unknown setting "Foo"`,
		"Set Shell ksh":              `unknown shell "ksh"`,
		"Set FontSize 0":             "1:14: FontSize must not be 0",
		"Set Width big":              `1:11: Width: "big" is not a whole number`,
		"Set Columns 0":              "1:13: Columns must be at least 1, got 0",
		"Set CursorBlink maybe":      `CursorBlink: "maybe" is not true or false`,
		"Set MarginFill bg.png":      "reel does not fill the margin with an image",
		`Set Theme { "red": "red" }`: `invalid theme color red: "red" is not a #rrggbb color`,
		"Set Theme { bad }":          "1:11: invalid theme JSON",
		"Set WindowBar Fancy":        `unknown WindowBar "Fancy"`,
		"Set WaitPattern /(/":        "1:17: invalid WaitPattern",
		"Type a\nSet Foo 1":          `2:5: unknown setting "Foo"`,
		"Set":                        "Set expects a setting",
	} {
		_, err = parseString(src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error = %v, want one containing %q", src, err, want)
		}
	}
}

// TestParseVHSExamples compiles VHS's example tapes, which term/tape's
// testdata holds with VHS's license: each compiles, but for those that use
// what reel rejects, or that VHS rejects too.
func TestParseVHSExamples(t *testing.T) {
	dir := filepath.Join("..", "..", "tape", "testdata", "vhs")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("term/tape's testdata is not beside this module:", err)
	}
	rejected := map[string]string{
		"cli-ui/text-prompt.tape":        "a .snap or .json output holds",
		"errors/parser.tape":             `unknown setting "Foo"`,
		"fixtures/all.tape":              "LoopOffset is not supported",
		"glow/glow-simple.tape":          "a .snap or .json output holds",
		"glow/glow.tape":                 "a .snap or .json output holds",
		"neofetch/neofetch.tape":         "a directory of frames",
		"settings/set-loop-offset.tape":  "LoopOffset is not supported",
		"settings/set-shell-custom.tape": `unknown shell "ksh"`,
		"welcome.tape":                   "a directory of frames",
	}
	root := os.DirFS(dir)
	n := 0
	err := fs.WalkDir(root, ".", func(name string, _ fs.DirEntry, err error) error {
		if err != nil || filepath.Ext(name) != ".tape" {
			return err
		}
		n++
		_, err = ParseFile(filepath.Join(dir, name), WithSourceRoot(root))
		want, isRejected := rejected[name]
		switch {
		case !isRejected && err != nil:
			t.Errorf("%s: %v", name, err)
		case isRejected && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: error = %v, want one containing %q", name, err, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 100 {
		t.Errorf("parsed %d example tapes, want VHS's 100 or more", n)
	}
}

func regexpOf(s string) *regexp.Regexp { return regexp.MustCompile(s) }
