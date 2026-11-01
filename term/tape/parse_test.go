// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tape

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// describe is a command as the tests compare it.
func describe(cmd Command) string {
	var b strings.Builder
	b.WriteString(cmd.Name.Text)
	for _, m := range cmd.Mods {
		b.WriteString("+" + m.Text)
	}
	if cmd.At != nil {
		b.WriteString("@" + cmd.At.Text)
	}
	for _, a := range cmd.Args {
		fmt.Fprintf(&b, " %d:%q", a.Kind, a.Text)
	}
	return b.String()
}

func TestParse(t *testing.T) {
	withMark := VHSCommands()
	withMark["Mark"] = Keep
	tests := []struct {
		src  string
		opts []Option
		want []string
		// err is part of the error, with its position.
		err string
	}{
		{src: `Type "hello"`, want: []string{`Type 2:"hello"`}},
		{src: "Type@100ms 'a' `b` c 3", want: []string{`Type@100ms 2:"a" 2:"b" 1:"c" 3:"3"`}},
		{src: `Type "x" Sleep 500ms  Enter`, want: []string{`Type 2:"x"`, `Sleep 3:"500ms"`, "Enter"}},
		{src: "Type\n\"a\"\n# comment\nEnter@.5 2", want: []string{`Type 2:"a"`, `Enter@.5 3:"2"`}},
		{src: `Ctrl+Alt+Shift+O`, want: []string{"Ctrl+Alt+Shift+O"}},
		{src: `Ctrl+[ Alt+Enter`, want: []string{"Ctrl+[", "Alt+Enter"}},
		{src: `Wait+Screen@10s /a\/b/`, want: []string{`Wait+Screen@10s 4:"a\\/b"`}},
		{src: "Set Theme { \"a\":\n\"#000\" } Set WaitPattern />$/", want: []string{
			`Set 1:"Theme" 5:"{ \"a\":\n\"#000\" }"`, `Set 1:"WaitPattern" 4:">$"`,
		}},
		{src: `Set LoopOffset 20%`, want: []string{`Set 1:"LoopOffset" 3:"20%"`}},
		{src: `Type "a" Mark b`, want: []string{`Type 2:"a" 1:"Mark" 1:"b"`}},
		{src: `Type "a" Mark b`, opts: []Option{WithCommands(withMark)}, want: []string{`Type 2:"a"`, `Mark 1:"b"`}},
		{src: `Type "a" Mark b`, opts: []Option{WithCommands(Commands{"Mark": Keep})}, err: `1:1: unknown command "Type"`},
		{src: `Foo 1 2 Enter Bar`, err: `1:1: unknown command "Foo"`},
		{src: `Foo 1 2 Enter@`, err: `1:14: Enter@ expects a value`},
		{src: `"Foo"`, err: `1:1: expected a command, got "Foo"`},
		{src: `Backspace@`, err: `1:10: Backspace@ expects a value`},
		{src: `Type "open`, err: "1:6: unterminated string"},
		{src: `Wait /open`, err: "1:6: unterminated regular expression"},
		{src: `Set Theme { "a": 1`, err: "1:11: unterminated JSON"},
		{src: `Type ~`, err: "1:6: unexpected character '~'"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			cmds, err := Parse("", strings.NewReader(tt.src), tt.opts...)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want one containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, cmd := range cmds {
				got = append(got, describe(cmd))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("commands:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestParseActions checks that skipped commands are dropped, failed ones
// are errors, and both are reported.
func TestParseActions(t *testing.T) {
	cmds := VHSCommands()
	cmds["Screenshot"], cmds["Env"], cmds["Mark"] = Skip, Fail, Skip
	var reported []string
	report := WithReport(func(cmd Command, action Action) {
		reported = append(reported, action.String()+" "+describe(cmd))
	})
	src := "Type a\nScreenshot b.png\nMark x y Enter"
	got, err := Parse("", strings.NewReader(src), WithCommands(cmds), report)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name.Text != "Type" || got[1].Name.Text != "Enter" {
		t.Errorf("commands = %v, want Type and Enter", got)
	}
	want := []string{`skip Screenshot 1:"b.png"`, `skip Mark 1:"x" 1:"y"`}
	if !slices.Equal(reported, want) {
		t.Errorf("reported %q, want %q", reported, want)
	}

	reported = nil
	_, err = Parse("", strings.NewReader("Env A b\nType c"), WithCommands(cmds), report)
	if !errors.Is(err, ErrDisabled) || !strings.Contains(err.Error(), "1:1: Env: command is disabled") {
		t.Errorf("error = %v, want Env disabled", err)
	}
	if !slices.Equal(reported, []string{`fail Env 1:"A" 1:"b"`}) {
		t.Errorf("reported %q, want Env's failure", reported)
	}
	if s := Action(7).String(); s != "Action(7)" {
		t.Errorf("Action(7) = %s", s)
	}
}

func TestParsePositions(t *testing.T) {
	cmds, err := Parse("demo.tape", strings.NewReader("Set Theme {\n}\n  Type@1s \"a\""))
	if err != nil {
		t.Fatal(err)
	}
	if got := cmds[1].Name.Pos.String(); got != "demo.tape:3:3" {
		t.Errorf("Type is at %s, want demo.tape:3:3", got)
	}
	if got := cmds[1].At.Pos.String(); got != "demo.tape:3:8" {
		t.Errorf("1s is at %s, want demo.tape:3:8", got)
	}
}

func TestTokenValues(t *testing.T) {
	num := func(s string) Token { return Token{Kind: Number, Text: s} }
	for _, tt := range []struct {
		tok  Token
		want time.Duration
		err  bool
	}{
		{tok: num("1"), want: time.Second},
		{tok: num(".5"), want: 500 * time.Millisecond},
		{tok: num("500ms"), want: 500 * time.Millisecond},
		{tok: num("2m"), want: 2 * time.Minute},
		{tok: num("1h"), err: true},
		{tok: num("1.2.3"), err: true},
		{tok: Token{Kind: Word, Text: "soon"}, err: true},
	} {
		got, err := tt.tok.Duration()
		if got != tt.want || (err != nil) != tt.err {
			t.Errorf("%q.Duration() = %v, %v", tt.tok.Text, got, err)
		}
	}
	if n, err := num("3").Int(); n != 3 || err != nil {
		t.Errorf("Int() = %v, %v", n, err)
	}
	if _, err := num("3px").Int(); err == nil {
		t.Error("3px is a whole number")
	}
	if f, err := num("1.5").Float(); f != 1.5 || err != nil {
		t.Errorf("Float() = %v, %v", f, err)
	}
	if b, err := (Token{Kind: Word, Text: "true"}).Bool(); !b || err != nil {
		t.Errorf("Bool() = %v, %v", b, err)
	}
	if _, err := (Token{Kind: String, Text: "true"}).Bool(); err == nil {
		t.Error(`"true" is a bool`)
	}
}

// TestParseVHSExamples parses VHS's example tapes, which testdata/vhs holds
// with VHS's license. Each parses, even the one written to fail, whose
// errors are in what its commands mean.
func TestParseVHSExamples(t *testing.T) {
	root := os.DirFS("testdata/vhs")
	n := 0
	err := fs.WalkDir(root, ".", func(name string, _ fs.DirEntry, err error) error {
		if err != nil || filepath.Ext(name) != ".tape" {
			return err
		}
		n++
		_, err = ParseFile(filepath.Join("testdata", "vhs", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
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
