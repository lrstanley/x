// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/lrstanley/x/term/reel"
)

// parse parses args as reel's command line, with clix's global flags.
func parse(t *testing.T, args ...string) (*Flags, error) {
	t.Helper()
	var cli struct {
		Flags `embed:""`

		Version bool `name:"version" short:"v"`
	}
	p, err := kong.New(&cli, kong.Vars{"spec": specHelp}, kong.Exit(func(int) { t.Fatal("exited") }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Parse(args)
	return &cli.Flags, err
}

// TestParse checks that the command line parses, with flags over the
// environment, and that unset look flags stay unset for a tape's.
func TestParse(t *testing.T) {
	t.Setenv("REEL_THEME", "Dracula")
	t.Setenv("REEL_FPS", "12")
	tape := filepath.Join(t.TempDir(), "a.tape")
	if err := os.WriteFile(tape, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := parse(t, "run", tape, "--fps", "30", "--no-chrome", "-o", "x.gif", "--zoom", "row=1")
	if err != nil {
		t.Fatal(err)
	}
	r := f.Run
	if *r.FPS != 30 || *r.Theme != "Dracula" || *r.WindowBar || r.FontSize != nil || r.Title != nil {
		t.Errorf("run look = fps %v theme %v chrome %v font size %v title %v", *r.FPS, *r.Theme, *r.WindowBar, r.FontSize, r.Title)
	}
	if _, err = r.effects(); err != nil {
		t.Error(err)
	}
	for _, args := range [][]string{
		{"session", "start", "--", "bash", "--norc"},
		{"s", "-s", "x", "wait", "--text", "hi"},
		{"s", "key", "enter", "ctrl+c", "-r", "2"},
		{"s", "stop", "-o", "a.gif"},
		{"export", "a.cast", "-o", "a.gif", "--between", "a,b", "--redact-env", "HOME"},
		{"still", "a.cast", "--at", "2s", "-o", "a.png"},
		{"cut", "a.cast", "--from", "a", "-o", "b.cast", "--cast-version", "3"},
		{"rec", "-o", "a.cast", "--", "htop", "-d", "5"},
		{"themes", "--dark", "--hue", "blue"},
	} {
		if _, parseErr := parse(t, args...); parseErr != nil {
			t.Errorf("%q: %v", args, parseErr)
		}
	}
	f, err = parse(t, "session", "start", "--", "bash", "--norc")
	if err == nil && strings.Join(f.Session.Start.Command, " ") != "bash --norc" {
		t.Errorf("session command = %q", f.Session.Start.Command)
	}
	if _, err = parse(t, "s", "wait", "--text", "a", "--gone", "b"); err == nil {
		t.Error("wait with two conditions parsed")
	}
}

func TestEffectFlags(t *testing.T) {
	for flags, want := range map[*EffectFlags]string{
		{Hide: []string{"a"}}:             `--hide "a" is not FROM,TO`,
		{Zoom: []string{"row=x"}}:         "--zoom",
		{RedactRegexp: []string{"("}}:     "--redact-regexp",
		{Intro: time.Second, Pause: true}: "",
	} {
		_, err := flags.effects()
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("effects(%+v) = %v, want %q", *flags, err, want)
		}
	}
}

func TestLineDiff(t *testing.T) {
	if d := lineDiff("a\nb\n", "a\nb\n"); d != "" {
		t.Errorf("equal diff = %q", d)
	}
	if d := lineDiff("a\nb\n", "a\nc\nd\n"); d != "  row 1:\n  - b\n  + c\n  row 2:\n  - \n  + d" {
		t.Errorf("diff = %q", d)
	}
}

// TestStillEvents checks how many events lead up to a still's moment.
func TestStillEvents(t *testing.T) {
	cast := `{"version": 2, "width": 10, "height": 2}
[0.1, "o", "a"]
[0.2, "m", "here"]
[0.3, "o", "b"]
`
	tl, err := reel.ReadTimeline(strings.NewReader(cast))
	if err != nil {
		t.Fatal(err)
	}
	at := 150 * time.Millisecond
	for c, want := range map[*StillCmd]int{{}: 3, {At: &at}: 1, {Mark: "here"}: 2} {
		if n, eventsErr := c.events(tl); eventsErr != nil || n != want {
			t.Errorf("events(%+v) = %d, %v; want %d", c, n, eventsErr, want)
		}
	}
	var nerr *reel.NoMarkError
	if _, err = (&StillCmd{Mark: "gone"}).events(tl); !errors.As(err, &nerr) {
		t.Errorf("missing mark error = %v", err)
	}
}

func TestTapesIn(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.tape", "a.tape", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := tapesIn([]string{dir, filepath.Join(dir, "notes.txt")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.tape"), filepath.Join(dir, "b.tape"), filepath.Join(dir, "notes.txt")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tapesIn = %q, want %q", got, want)
	}
}
