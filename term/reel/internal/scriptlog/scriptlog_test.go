// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scriptlog

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestSkipHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, rest string
		err            error
	}{
		{"header", "Script started on now [COMMAND=\"sh\"]\nab", "ab", nil},
		{"none", "Script ab", "Script ab", nil},
		{"short", "Scr", "Scr", nil},
		{"empty", "", "", nil},
		{"unfinished", "Script started on now", "", io.EOF},
	}
	for _, tt := range tests {
		br := bufio.NewReader(strings.NewReader(tt.in))
		if err := SkipHeader(br); !errors.Is(err, tt.err) {
			t.Errorf("%s: SkipHeader = %v, want %v", tt.name, err, tt.err)
		}
		if rest, _ := io.ReadAll(br); string(rest) != tt.rest {
			t.Errorf("%s: left %q, want %q", tt.name, rest, tt.rest)
		}
	}

	// The reader's errors are returned as they are.
	boom := errors.New("boom")
	br := bufio.NewReader(io.MultiReader(strings.NewReader(header), iotest.ErrReader(boom)))
	if err := SkipHeader(br); !errors.Is(err, boom) {
		t.Errorf("SkipHeader of a failing reader = %v, want %v", err, boom)
	}

	// A typescript that differs from the line is not waited on.
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	go func() { _, _ = pw.Write([]byte("ab")) }()
	done := make(chan error, 1)
	go func() { done <- SkipHeader(bufio.NewReader(pr)) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("SkipHeader of a blocking typescript = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("SkipHeader waited on a typescript without the line")
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		want Entry
	}{
		{"0.25 12", Entry{Kind: 'O', Delay: 250 * time.Millisecond, Count: 12}},
		{"O 1 3", Entry{Kind: 'O', Delay: time.Second, Count: 3}},
		{"I 0.5 0", Entry{Kind: 'I', Delay: 500 * time.Millisecond}},
		{"31536000 1", Entry{Kind: 'O', Delay: 365 * 24 * time.Hour, Count: 1}},
		{"S 0 SIGWINCH ROWS=24 COLS=80", Entry{Kind: 'S', Name: "SIGWINCH", Value: "ROWS=24 COLS=80"}},
		{"S 0 SIGTERM", Entry{Kind: 'S', Name: "SIGTERM"}},
		{"H 0 TERM xterm 256", Entry{Kind: 'H', Name: "TERM", Value: "xterm 256"}},
	}
	for _, tt := range tests {
		if got, err := Parse(tt.line); err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %+v, %v, want %+v", tt.line, got, err, tt.want)
		}
	}

	for line, want := range map[string]string{
		"0":                      `malformed entry "0"`,
		"0 1 2":                  `malformed entry "0 1 2"`,
		"O 0":                    `malformed entry "O 0"`,
		"H 0":                    `malformed entry "H 0"`,
		"x 1":                    `delay "x" is not a number of seconds`,
		"-1 1":                   `delay "-1" is not a number of seconds`,
		"NaN 1":                  `delay "NaN" is not a number of seconds`,
		"31536001 1":             `delay "31536001" is not a number of seconds`,
		"0 -1":                   `count "-1" is not a number of bytes`,
		"I 0 lots":               `count "lots" is not a number of bytes`,
		"0 99999999999999999999": `count "99999999999999999999" is not a number of bytes`,
	} {
		if _, err := Parse(line); err == nil || err.Error() != want {
			t.Errorf("Parse(%q) = %v, want %q", line, err, want)
		}
	}
}

func TestParseWinch(t *testing.T) {
	t.Parallel()
	for info, want := range map[string][2]int{
		"ROWS=24 COLS=80":   {80, 24},
		"COLS=80 ROWS=24":   {80, 24},
		"ROWS=65535 COLS=1": {1, 65535},
	} {
		if cols, rows, err := ParseWinch(info); err != nil || cols != want[0] || rows != want[1] {
			t.Errorf("ParseWinch(%q) = %d, %d, %v, want %d, %d", info, cols, rows, err, want[0], want[1])
		}
	}
	for _, info := range []string{"", "ROWS=24", "ROWS=x COLS=80", "ROWS=70000 COLS=80", "ROWS=+5 COLS=80", "ROWS=0 COLS=80"} {
		if _, _, err := ParseWinch(info); err == nil || !strings.Contains(err.Error(), "each from 1 to 65535") {
			t.Errorf("ParseWinch(%q) = %v, want an error", info, err)
		}
	}
}
