// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command export records a terminal session without a program, by writing
// to it directly and typing into it, and exports it as a GIF, an MP4 when
// ffmpeg can encode one, an asciicast file, and a still of the terminal at a
// mark. The recordings open on a title card, show the keys typed, and end
// on a pause badge.
//
// The asciicast file records those effects with the session. Pass --cast to
// export an asciicast file instead, such as the one written before: its
// recordings get the effects the file holds, and look the same as those of
// the session it was written from.
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
	Dir  string `name:"dir" type:"path" default:"." help:"Directory to write the files to."`
	Cast string `name:"cast" type:"existingfile" help:"Asciicast file to export, with the effects it holds, instead of recording the session."`
}

func main() {
	cli := clix.NewWithDefaults[Flags]()
	cli.Execute(func(ctx context.Context, _ *slog.Logger) error {
		var files []string
		var err error
		if cli.Flags.Cast != "" {
			files, err = runCast(ctx, cli.Flags.Dir, cli.Flags.Cast)
		} else {
			files, err = run(ctx, cli.Flags.Dir)
		}
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

	files := recordings(dir, "demo")
	outputs := append(reel.MustFiles(files...), reel.File{
		Path:  filepath.Join(dir, "demo-{mark}.png"),
		Marks: []string{"built"},
	})
	fx := []reel.Effect{effects.Intro(time.Second), effects.Keys(time.Second), effects.Pause()}
	if err = reel.Export(ctx, s.Timeline(), profile(outputs, fx)); err != nil {
		return nil, err
	}
	return append(files, filepath.Join(dir, "demo-built.png")), nil
}

// runCast exports the asciicast file at path into dir, with the effects it
// holds, returning the paths written.
func runCast(ctx context.Context, dir, path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(f)
	if err != nil {
		return nil, err
	}
	fx, err := effects.FromTimeline(tl)
	if err != nil {
		return nil, err
	}
	files := recordings(dir, "cast")
	if err = reel.Export(ctx, tl, profile(reel.MustFiles(files...), fx)); err != nil {
		return nil, err
	}
	return files, nil
}

// recordings returns the paths of the recordings named name in dir: a GIF,
// an asciicast file, and an MP4 when ffmpeg can encode one.
func recordings(dir, name string) []string {
	files := []string{
		filepath.Join(dir, name+".gif"),
		filepath.Join(dir, name+".cast"),
	}
	if sink.CheckVideo(sink.FormatMP4) == nil {
		files = append(files, filepath.Join(dir, name+".mp4"))
	} else {
		fmt.Fprintf(os.Stderr, "skipping %s.mp4: ffmpeg cannot encode MP4 here\n", name)
	}
	return files
}

// profile is the profile both exports use, with outputs and fx.
func profile(outputs []reel.File, fx []reel.Effect) reel.Profile {
	return reel.Profile{
		Outputs: outputs,
		Render:  []render.Option{render.WithWindowChrome(true)},
		Effects: fx,
	}
}
