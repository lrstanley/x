// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command exec records an interactive bash on a pseudo-terminal with
// reel.Exec, typing commands into it as a person would, and exports the
// recording as a GIF that shows the keys typed, an asciicast file, and a
// PNG of its last frame.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lrstanley/clix/v2"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
)

// Flags are the command's flags.
type Flags struct {
	Dir string `name:"dir" type:"path" default:"." help:"Directory to write the files to."`
}

func main() {
	cli := clix.NewWithDefaults[Flags]()
	cli.Execute(func(ctx context.Context, _ *slog.Logger) error {
		files, err := run(ctx, cli.Flags.Dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println(f)
		}
		return nil
	})
}

// commands are typed at the prompt, each once the one before has finished.
// Each fits on the line after the prompt, since bash redraws a line that
// wraps by moving the cursor, which asciinema's play command shows wrongly
// in a terminal wider than the recording (see [reel.Timeline.WriteCast]).
var commands = []string{
	"echo hello from reel",
	`for p in 25 50 75 100; do printf '\rbuilding %d%%' $p; sleep .2; done; echo`,
	"exit",
}

// run records bash running the commands and exports it into dir, returning
// the paths written.
func run(ctx context.Context, dir string) ([]string, error) {
	path, err := exec.LookPath("bash")
	if err != nil {
		return nil, err
	}
	s, err := reel.Open(ctx, 80, 8, reel.WithTitle("reel exec"))
	if err != nil {
		return nil, err
	}
	defer s.Close()

	cmd := exec.Command(path, "--norc", "--noprofile", "-i")
	// The host's environment, with a prompt of its own, and no history
	// written. Exec sets TERM and COLORTERM.
	cmd.Env = append(os.Environ(), `PS1=\[\e[1;36m\]$\[\e[0m\] `, "HISTFILE=/dev/null")
	p, err := reel.Exec(ctx, s, cmd)
	if err != nil {
		return nil, err
	}
	for i, c := range commands {
		if err = waitPrompts(ctx, s, i+1); err != nil {
			return nil, err
		}
		time.Sleep(400 * time.Millisecond)
		if err = s.Type(c + "\r"); err != nil {
			return nil, err
		}
	}
	if status, werr := p.Wait(); werr != nil || status != 0 {
		return nil, fmt.Errorf("bash exited with %d: %w", status, werr)
	}

	files := []string{
		filepath.Join(dir, "exec.gif"),
		filepath.Join(dir, "exec.cast"),
		filepath.Join(dir, "exec.png"),
	}
	err = reel.Export(ctx, s.Timeline(), reel.Profile{
		Outputs: reel.Files(files...),
		Render:  []render.Option{render.WithWindowChrome(true)},
		Effects: []reel.Effect{effects.Keys(time.Second), effects.Pause()},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// prompt starts the prompt bash writes.
const prompt = "\x1b[1;36m$"

// waitPrompts waits for bash to have written n prompts.
func waitPrompts(ctx context.Context, s *reel.Session, n int) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out strings.Builder
	read := 0
	for {
		updated := s.Timeline().Updated()
		for ev := range s.Timeline().EventsFrom(read) {
			read++
			if o, ok := ev.(reel.Output); ok {
				out.Write(o.Data)
			}
		}
		if strings.Count(out.String(), prompt) >= n {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for prompt %d: %w", n, ctx.Err())
		case <-updated:
		}
	}
}
