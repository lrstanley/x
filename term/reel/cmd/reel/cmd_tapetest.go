// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/play"
	"github.com/lrstanley/x/term/tape"
)

// TestCmd runs tapes as tests.
type TestCmd struct {
	TapeFlags `embed:""`

	Paths     []string `arg:"" type:"path" placeholder:"TAPE|DIR" help:"Tapes to run, and directories of *.tape files."`
	Update    bool     `name:"update" short:"u" help:"Write each Snapshot's screen to its golden file, instead of comparing them."`
	Artifacts string   `name:"artifacts" type:"path" placeholder:"DIR" help:"Write a .cast and .gif of each failed tape here."`
}

// Run runs each tape. A tape fails at the first command that does: a Wait
// that times out, a Snapshot that differs, or any other error.
func (c *TestCmd) Run(ctx context.Context) error {
	paths, err := tapesIn(c.Paths)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("no tapes")
	}
	failed := 0
	for _, path := range paths {
		start := time.Now()
		runErr := c.run(ctx, path)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		took := time.Since(start).Round(10 * time.Millisecond)
		if runErr == nil {
			fmt.Fprintf(os.Stdout, "ok    %s\t%v\n", path, took)
			continue
		}
		failed++
		fmt.Fprintf(os.Stdout, "FAIL  %s\t%v\n      %s\n", path, took, strings.ReplaceAll(runErr.Error(), "\n", "\n      "))
	}
	fmt.Fprintf(os.Stdout, "%d passed, %d failed\n", len(paths)-failed, failed)
	if failed > 0 {
		return &exitError{code: exitWaitTimeout, err: errors.New("")}
	}
	return nil
}

func (c *TestCmd) run(ctx context.Context, path string) error {
	golden := filepath.Join(filepath.Dir(path), "testdata", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	tp, err := c.compile(path, play.WithCommand("Snapshot", func(cmd tape.Command) (play.Step, error) {
		return snapshot(cmd, golden, c.Update)
	}))
	if err != nil {
		return err
	}
	res, err := tp.Run(ctx, play.WithoutRecording())
	if err == nil || c.Artifacts == "" || res.Timeline == nil {
		return err
	}
	return errors.Join(err, c.writeArtifacts(ctx, path, res))
}

// writeArtifacts writes the recording of a failed tape: its timeline as a
// .cast, and a .gif as the tape would have written it.
func (c *TestCmd) writeArtifacts(ctx context.Context, path string, res *play.Result) error {
	base := filepath.Join(c.Artifacts, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	err := writeCast(res.Timeline, base+".cast", reel.CastV2)
	if err != nil {
		return err
	}
	p := res.Profile
	p.Outputs = reel.MustFiles(base + ".gif")
	err = reel.Export(ctx, res.Timeline, p)
	if err != nil {
		return fmt.Errorf("artifacts: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s: wrote %s.cast and %s.gif\n", path, base, base)
	return nil
}

// snapshot compiles Snapshot <name>, which compares the screen, each line
// without trailing spaces, to the golden file golden/<name>.snap, or with
// update writes it there.
func snapshot(cmd tape.Command, golden string, update bool) (play.Step, error) {
	if len(cmd.Args) != 1 || !cmd.Args[0].IsString() || cmd.Args[0].Text == "" {
		return nil, &tape.Error{Pos: cmd.Name.Pos, Err: errors.New("expected Snapshot <name>")}
	}
	name := cmd.Args[0].Text
	if !fs.ValidPath(name) || strings.Contains(name, "/") {
		return nil, &tape.Error{Pos: cmd.Args[0].Pos, Err: fmt.Errorf("the name of a Snapshot, %q, must be a file name", name)}
	}
	file := filepath.Join(golden, name+".snap")
	return func(_ context.Context, s *reel.Session) error {
		screen, err := s.ScreenText()
		if err != nil {
			return err
		}
		got := play.TrimScreen(screen.Text) + "\n"
		if update {
			err = os.MkdirAll(golden, 0o750)
			if err != nil {
				return err
			}
			return os.WriteFile(file, []byte(got), 0o600)
		}
		want, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no golden file %s for Snapshot %s; run with --update to write it", file, name)
		}
		if err != nil {
			return err
		}
		if diff := lineDiff(string(want), got); diff != "" {
			return fmt.Errorf("the screen differs from Snapshot %s in %s (- want, + got):\n%s", name, file, diff)
		}
		return nil
	}, nil
}

// lineDiff returns the lines of want and got that differ, row by row, or
// "" when they are equal. Screens are the same size, so rows line up.
func lineDiff(want, got string) string {
	w := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	g := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	var b strings.Builder
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "  row %d:\n  - %s\n  + %s\n", i, wl, gl)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// tapesIn returns the tapes paths name: each file, and the *.tape files in
// each directory, sorted.
func tapesIn(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(p, "*.tape"))
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
	}
	return out, nil
}
