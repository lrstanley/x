// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// promptCmd is a shell prompt, with OSC 133 marks, that runs line, prints
// one line of output, and exits with code.
func promptCmd(line string, code int) string {
	return "\x1b]133;A\x07$ \x1b]133;B\x07" + line + "\r\n\x1b]133;C\x07out\r\n" +
		fmt.Sprintf("\x1b]133;D;%d\x07", code)
}

// TestPromptMarks checks that each primary prompt gets a mark on its row that
// follows it into the scrollback, with how its command ended, and that marks
// go when their rows are erased or the terminal resets.
func TestPromptMarks(t *testing.T) {
	t.Parallel()
	term := openTest(t, 20, 4)
	write := func(s string) []PromptMark {
		t.Helper()
		if _, err := term.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
		return term.State().PromptMarks
	}
	check := func(name string, got, want []PromptMark) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s: marks = %+v, want %+v", name, got, want)
		}
	}

	ok := PromptMark{Row: 0, Ended: true, HasExitCode: true}
	failed := PromptMark{Row: 2, Ended: true, HasExitCode: true, ExitCode: 2}
	check("two commands", write(promptCmd("ok", 0)+promptCmd("bad", 2)), []PromptMark{ok, failed})
	if !failed.Failed() || ok.Failed() || (PromptMark{Ended: true}).Failed() {
		t.Error("Failed is wrong")
	}

	// Rows 4 and 5 scroll the first two into the scrollback.
	running := PromptMark{Row: 4}
	marks := write("\x1b]133;A\x07$ \x1b]133;B\x07sleep\r\n\x1b]133;C\x07")
	check("running", marks, []PromptMark{ok, failed, running})
	if st := term.State(); st.ScrollbackLen != 2 || &st.PromptMarks[0] != &marks[0] {
		t.Errorf("state without writes: scrollback %d, marks not shared", st.ScrollbackLen)
	}

	// A prompt redrawn on its row, and continuation and right prompts, add
	// no mark. An end with no status still ends the command.
	running.Ended = true
	typing := PromptMark{Row: 5}
	check("redrawn", write("\x1b]133;D\x07\x1b]133;A\x07$ \r\x1b]133;A\x07$ \x1b]133;P;k=r\x07"+
		"\x1b]133;B\x07a\r\n\x1b]133;P;k=c\x07> b"), []PromptMark{ok, failed, running, typing})

	if got := write("\x1b[?1049h"); got != nil {
		t.Errorf("alternate screen: marks = %+v", got)
	}
	check("primary again", write("\x1b[?1049l"), []PromptMark{ok, failed, running, typing})
	check("prompt on the alternate screen", write("\x1b[?1049h\x1b[H"+promptCmd("vim", 1)+"\x1b[?1049l"),
		[]PromptMark{ok, failed, running, typing})

	// Erasing the three scrollback rows drops the marks in them, whose
	// references libghostty-vt moves to the top row, and pruning drops the
	// rest.
	check("scrollback erased", write("\x1b[3J"), []PromptMark{{Row: 1, Ended: true}, {Row: 2}})
	check("pruned", write(strings.Repeat("\r\n", 5000)), nil)

	write(promptCmd("x", 1))
	check("reset", write("\x1bc"), nil)
	check("after a reset", write(promptCmd("x", 0)+"\x1bc"+promptCmd("y", 1)),
		[]PromptMark{{Row: 0, Ended: true, HasExitCode: true, ExitCode: 1}})
	check("second end", write("\x1b]133;D;5\x07"), []PromptMark{{Row: 0, Ended: true, HasExitCode: true, ExitCode: 1}})
}

func TestPromptVT(t *testing.T) {
	t.Parallel()
	body := []byte("a\r\nb\r\nc")
	for _, tc := range []struct {
		name  string
		marks []PromptMark
		last  Prompt
		want  string
	}{
		{"none", nil, Prompt{}, "a\r\nb\r\nc"},
		{
			"ended",
			[]PromptMark{{Row: 0, Ended: true, HasExitCode: true, ExitCode: 3}, {Row: 2, Ended: true}},
			Prompt{Kind: PromptCommand},
			"\x1b]133;A\x07a\r\nb\r\n\x1b]133;D;3\x07\x1b]133;A\x07c\x1b]133;D\x07",
		},
		{"typing", []PromptMark{{Row: 1}}, Prompt{Kind: PromptInput}, "a\r\n\x1b]133;A\x07b\r\nc\x1b]133;B\x07"},
		{
			"running",
			[]PromptMark{{Row: 1}},
			Prompt{Kind: PromptOutput, Command: "echo a/b"},
			"a\r\n\x1b]133;A\x07b\r\nc\x1b]133;C;cmdline_url=echo%20a%2Fb\x07",
		},
		{"past the end", []PromptMark{{Row: 5}}, Prompt{}, "a\r\nb\r\nc"},
	} {
		if got := string(appendPromptVT(nil, body, tc.marks, tc.last)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
