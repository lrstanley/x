// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/look"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/themes"
	syntax "github.com/lrstanley/x/term/tape"
)

// echo is an in-process program with a "> " prompt that echoes what it
// reads, starting a new prompt on enter.
func echo(_ context.Context, s *reel.Session) error {
	if _, err := s.Write([]byte("> ")); err != nil {
		return err
	}
	buf := make([]byte, 256)
	for {
		n, err := s.Read(buf)
		if err != nil {
			return nil //nolint:nilerr // The session closed.
		}
		if _, err = s.Write(bytes.ReplaceAll(buf[:n], []byte("\r"), []byte("\r\n> "))); err != nil {
			return nil //nolint:nilerr // The session closed.
		}
	}
}

// runTape parses src, with %s replaced by dir, and runs it with opts.
func runTape(t *testing.T, src string, opts ...RunOption) (*Result, string, error) {
	t.Helper()
	return runCompiled(t, src, nil, opts...)
}

// runCompiled is runTape, compiling src with copts.
func runCompiled(t *testing.T, src string, copts []CompileOption, opts ...RunOption) (*Result, string, error) {
	t.Helper()
	dir := t.TempDir()
	tp, err := parseString(strings.ReplaceAll(src, "%s", filepath.ToSlash(dir)), copts...)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tp.Run(t.Context(), opts...)
	return res, dir, err
}

func labels(tl *reel.Timeline) []string {
	var out []string
	for m := range tl.Marks() {
		out = append(out, m.Label)
	}
	return out
}

// TestRunProgram runs a tape against an in-process program.
func TestRunProgram(t *testing.T) {
	res, dir, err := runTape(t, `
Output "%s/out.snap"
Output "%s/out.cast"
Set Columns 40
Set Rows 6
Set TypingSpeed 0
Type "hello"
Enter
Wait /^>$/
Type@5ms "wor" "ld"
Backspace 2
Type "ld"
Copy "pasted"
Paste
Mark "step:1"
Screenshot "%s/shot.png"
Sleep 50ms
`, WithProgram(echo))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := os.ReadFile(filepath.Join(dir, "out.snap"))
	if err != nil {
		t.Fatal(err)
	}
	// echo writes back the DEL that Backspace sends.
	if !strings.Contains(string(snap), "> hello") || !strings.Contains(string(snap), "> wor ld\x7f\x7fldpasted") {
		t.Errorf("snap:\n%s", snap)
	}
	if fi, serr := os.Stat(filepath.Join(dir, "shot.png")); serr != nil || fi.Size() == 0 {
		t.Errorf("screenshot: %v", serr)
	}
	if h := res.Timeline.Header(); h.Cols != 40 || h.Rows != 6 {
		t.Errorf("screen = %dx%d, want 40x6", h.Cols, h.Rows)
	}
	if got, want := labels(res.Timeline), []string{"step:1", "shot", endMark}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
	var paste bool
	for in := range res.Timeline.Inputs() {
		paste = paste || strings.Contains(string(in.Data), "pasted")
	}
	if !paste {
		t.Error("Paste sent no input holding the copied text")
	}
}

// TestRunShell runs a tape in a real shell.
func TestRunShell(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs bash on a pseudo-terminal")
	}
	res, dir, err := runTape(t, `
Output "%s/out.snap"
Require bash
Env GREETING "hello from env"
Set Shell bash
Set Rows 10
Set TypingSpeed 0
Type "echo $GREETING" Enter
Wait+Screen /hello from env/
Type "seq 1 40" Enter
Wait+Screen /40\n>/
ScrollUp 3
`)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := os.ReadFile(filepath.Join(dir, "out.snap"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snap), "37") {
		t.Errorf("snap:\n%s", snap)
	}
	var scrolled bool
	for ev := range res.Timeline.Events() {
		if s, ok := ev.(reel.Scroll); ok && s.Offset == 3 {
			scrolled = true
		}
	}
	if !scrolled {
		t.Error("ScrollUp 3 did not scroll the viewport 3 rows")
	}
	var out strings.Builder
	for ev := range res.Timeline.Events() {
		if o, ok := ev.(reel.Output); ok {
			out.Write(o.Data)
		}
	}
	if !strings.Contains(out.String(), "hello from env") {
		t.Errorf("the shell did not get Env's variable:\n%q", out.String())
	}
}

// TestRunCommand runs a command of WithCommand.
func TestRunCommand(t *testing.T) {
	shout := WithCommand("Shout", func(cmd syntax.Command) (Step, error) {
		text := strings.ToUpper(cmd.Args[0].Text)
		return func(_ context.Context, s *reel.Session) error { return s.Type(text) }, nil
	})
	_, dir, err := runCompiled(t, `
Output "%s/out.snap"
Set Columns 20
Set Rows 3
Shout "hi"
Enter
Wait
`, []CompileOption{shout}, WithProgram(echo))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := os.ReadFile(filepath.Join(dir, "out.snap"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snap), "> HI") {
		t.Errorf("snap:\n%s", snap)
	}
}

// TestRunHide checks that what Hide hides is cut out of playback.
func TestRunHide(t *testing.T) {
	res, dir, err := runTape(t, `
Output "%s/out.gif"
Set Columns 20
Set Rows 3
Set FontSize 8
Set Padding 0
Set TypingSpeed 0
Type "shown"
Sleep 300ms
Hide
Type "hidden"
Sleep 1s
Show
Type "!"
Sleep 300ms
`, WithProgram(echo))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "out.gif"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	var played time.Duration
	for _, d := range g.Delay {
		played += time.Duration(d) * 10 * time.Millisecond
	}
	recorded := res.Timeline.Duration()
	if recorded < 1500*time.Millisecond || played > recorded-800*time.Millisecond {
		t.Errorf("played %v of %v recorded, want the hidden second cut", played, recorded)
	}
}

// TestRunWaitTimeout checks that a Wait that times out reports where, and
// the screen it last saw.
func TestRunWaitTimeout(t *testing.T) {
	_, _, err := runTape(t, `
Output "%s/out.snap"
Set Columns 30
Set Rows 3
Set TypingSpeed 0
Type "ready"
Wait+Screen@200ms /never/
`, WithProgram(echo))
	var terr *syntax.Error
	var werr *reel.WaitError
	if !errors.As(err, &terr) || !errors.As(err, &werr) {
		t.Fatalf("error = %v, want a *syntax.Error wrapping a *reel.WaitError", err)
	}
	if terr.Pos.Line != 7 || !strings.Contains(err.Error(), "wait for /never/ on the screen timed out after 200ms") ||
		!strings.Contains(err.Error(), "> ready") {
		t.Errorf("error = %v, want one at line 7 with the screen", err)
	}
}

// TestRunRejects checks the errors Run returns before running anything.
func TestRunRejects(t *testing.T) {
	tests := []struct {
		src  string
		opts []RunOption
		err  string
	}{
		{src: "Output out.gif\nEnv A b\nType x", opts: []RunOption{WithProgram(echo)}, err: "2:1: Env needs a shell, and the target is a program"},
		{src: "Output out.gif\nSet Shell bash", opts: []RunOption{WithProgram(echo)}, err: "2:5: Shell needs a shell"},
		{src: "Output out.gif\nRequire definitely-not-a-program-tape", err: "2:1: unmet Require:"},
		{src: "Output a.cast\nType a\nHide\nType b\nShow", opts: []RunOption{WithProgram(echo)}, err: "3:1: an asciicast output (a.cast) holds the recording uncut"},
		{src: "Type x", opts: []RunOption{WithProgram(echo)}, err: "no outputs"},
		{src: "Output out.gif\nSet Width 50\nSet Height 50", opts: []RunOption{WithProgram(echo)}, err: "has no room for text"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			tp, err := parseString(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			res, err := tp.Run(t.Context(), tt.opts...)
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("error = %v, want one containing %q", err, tt.err)
			}
			if res.Timeline != nil && res.Timeline.Len() > 0 {
				t.Errorf("the tape ran %d events before failing", res.Timeline.Len())
			}
		})
	}
}

// TestRunThemes checks that a theme name is looked up in the catalog, and
// becomes the terminal's default colors.
func TestRunThemes(t *testing.T) {
	res, _, err := runTape(t, "Output \"%s/out.snap\"\nSet Columns 10\nSet Rows 2\nSet Theme \"Dracula\"\nSleep 10ms",
		WithProgram(echo))
	if err != nil {
		t.Fatal(err)
	}
	var got color.Color
	for ev := range res.Timeline.Events() {
		if th, ok := ev.(reel.Theme); ok {
			got = th.Colors.Bg
		}
	}
	want := themes.Lookup("Dracula").Bg
	if r, g, b, _ := got.RGBA(); r>>8 != uint32(want.R) || g>>8 != uint32(want.G) || b>>8 != uint32(want.B) {
		t.Errorf("terminal background = %v, want %v", got, want.Hex())
	}
}

// TestRunProgramFails checks that a program that fails ends the run.
func TestRunProgramFails(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := runTape(t, "Output \"%s/out.snap\"\nSet Columns 10\nSet Rows 2\nSleep 5s", WithProgram(func(context.Context, *reel.Session) error {
		return boom
	}))
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "4:1: program: boom") {
		t.Errorf("error = %v, want boom at the Sleep", err)
	}
}

// TestRunProgramFailsLate checks that a program that fails during the last
// command fails the run, and ends the pause between typed keys.
func TestRunProgramFailsLate(t *testing.T) {
	boom := errors.New("boom")
	for _, last := range []string{`Type@1s "abc"`, `Enter@1s 3`, `Enter@0 1000000000`} {
		start := time.Now()
		_, _, err := runTape(t, "Output \"%s/out.snap\"\nSet Columns 10\nSet Rows 2\n"+last, WithProgram(func(context.Context, *reel.Session) error {
			time.Sleep(50 * time.Millisecond)
			return boom
		}))
		if !errors.Is(err, boom) {
			t.Errorf("%s: error = %v, want boom", last, err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("%s: took %v, want the failure to end the pause", last, elapsed)
		}
	}
}

// TestRunOverrides checks that a look and profile given to Run replace the
// tape's settings and Output commands, but not its Screenshots.
func TestRunOverrides(t *testing.T) {
	title := "flag"
	res, dir, err := runTape(t, "Output \"%s/tape.snap\"\nSet Title tape\nSet Columns 10\nSet Rows 2\nScreenshot \"%s/shot.png\"",
		WithProgram(echo), WithLook(look.Look{Title: &title}),
		WithProfile(reel.Profile{Outputs: reel.MustFiles(filepath.Join(t.TempDir(), "flag.snap"))}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Profile.Title != "flag" {
		t.Errorf("title = %q, want the look's", res.Profile.Title)
	}
	var paths []string
	for _, f := range res.Profile.Outputs {
		paths = append(paths, filepath.Base(f.Path))
	}
	if !slices.Equal(paths, []string{"flag.snap", "{mark}.png"}) {
		t.Errorf("outputs = %v, want the profile's and the screenshot's", paths)
	}
	if _, err = os.Stat(filepath.Join(dir, "tape.snap")); !os.IsNotExist(err) {
		t.Errorf("the tape's Output was written: %v", err)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[len(res.Warnings)-1], "replace the tape's 1 Output") {
		t.Errorf("warnings = %q, want one about the replaced Output", res.Warnings)
	}
}
