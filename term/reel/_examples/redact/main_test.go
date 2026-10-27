// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lrstanley/x/term/reel"
)

// token is a made-up token, with a prefix of three characters, "ghp", that
// the rest of the screen does not show.
const token = "ghp_t3stT0k3nN0tR3al"

func TestRun(t *testing.T) {
	t.Setenv(tokenEnv, token)
	files, err := run(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if fi, err := os.Stat(f); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// TestRedacted checks that no frame of the recording shows the token, or
// any of its first three characters or more, on the screen or in the
// title, though the timeline holds it, and that it can not be saved as
// asciicast.
func TestRedacted(t *testing.T) {
	t.Setenv(tokenEnv, token)
	tl, err := record(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if text := castText(t, tl); !strings.Contains(text, token) {
		t.Fatal("the timeline does not hold the token")
	}
	p := profile(t.TempDir())
	var frames int
	p.Effects = append(p.Effects, check(func(f reel.Frame) {
		if f.Cells == nil {
			return
		}
		frames++
		for _, text := range []string{f.Cells.Screen.Text(), f.Cells.State.Title} {
			for n := 3; n <= len(token); n++ {
				if strings.Contains(text, token[:n]) {
					t.Fatalf("frame at %v shows %q:\n%s", f.At, token[:n], text)
				}
			}
		}
	}))
	if err = reel.Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	if frames < len(token) {
		t.Fatalf("%d frames, want one at least for each key typed", frames)
	}

	p = profile(t.TempDir())
	p.Outputs = append(p.Outputs, reel.File{Path: filepath.Join(t.TempDir(), "redact.cast")})
	if err = reel.Export(t.Context(), tl, p); err == nil {
		t.Fatal("Export wrote a redacted recording as asciicast")
	}
}

// check is an [reel.Effect] that calls fn with every frame and passes it
// on.
type check func(reel.Frame)

func (c check) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		c(f)
		emit(f)
	}, nil
}

// castText returns tl as an asciicast file.
func castText(t *testing.T, tl *reel.Timeline) string {
	t.Helper()
	var b strings.Builder
	if _, err := tl.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
