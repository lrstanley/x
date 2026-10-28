// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command export records a terminal session without a program, by writing
// to it directly and typing into it, and exports it as a GIF, an MP4 when
// ffmpeg can encode one, an asciicast file, and a still of the terminal at a
// mark. The recordings open on a title card, show the keys typed, and end
// on a pause badge.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/lrstanley/clix/v2"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
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

// script is what the session shows: after a pause, each step types its
// text, as the host would, and writes its output, as the program would.
// Nothing reads what is typed, so the output echoes it.
var script = []struct {
	pause time.Duration
	typed string
	out   string
	mark  string
}{
	{0, "", "\x1b]2;reel export\x07\x1b[1;36m$\x1b[0m ", ""},
	{400 * time.Millisecond, "make build\r", "make build\r\n", ""},
	{200 * time.Millisecond, "", "\x1b]9;4;3\x07compiling \x1b[33m...\x1b[0m", ""},
	{1200 * time.Millisecond, "", "\r\x1b[K\x1b]9;4;0\x07\x1b[32m✔\x1b[0m built \x1b[1mreel\x1b[0m in 1.2s\r\n", "built"},
	{300 * time.Millisecond, "", "\x1b[1;36m$\x1b[0m ", ""},
}

// run records the script and exports it into dir, returning the paths
// written.
func run(ctx context.Context, dir string) ([]string, error) {
	s, err := reel.Open(ctx, 48, 6, reel.WithTitle("reel export"))
	if err != nil {
		return nil, err
	}
	defer s.Close()
	for _, step := range script {
		time.Sleep(step.pause)
		if step.typed != "" {
			if err = s.Type(step.typed); err != nil {
				return nil, err
			}
		}
		if _, err = s.Write([]byte(step.out)); err != nil {
			return nil, err
		}
		if step.mark != "" {
			if err = s.Mark(step.mark); err != nil {
				return nil, err
			}
		}
	}

	files := []string{
		filepath.Join(dir, "demo.gif"),
		filepath.Join(dir, "demo.cast"),
	}
	if sink.CheckVideo(sink.FormatMP4) == nil {
		files = append(files, filepath.Join(dir, "demo.mp4"))
	} else {
		fmt.Fprintln(os.Stderr, "skipping demo.mp4: ffmpeg cannot encode MP4 here")
	}
	outputs := append(reel.Files(files...), reel.File{
		Path:  filepath.Join(dir, "demo-{mark}.png"),
		Marks: []string{"built"},
	})
	err = reel.Export(ctx, s.Timeline(), reel.Profile{
		Outputs: outputs,
		Render:  []render.Option{render.WithWindowChrome(true)},
		Effects: []reel.Effect{effects.Intro(time.Second), effects.Keys(time.Second), effects.Pause()},
	})
	if err != nil {
		return nil, err
	}
	return append(files, filepath.Join(dir, "demo-built.png")), nil
}
