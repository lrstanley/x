// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command attach replays a recording util-linux script(1) made, a
// typescript and its timing file, into a session with reel.Attach, and
// exports it as a GIF and an asciicast file. The bundled recording was made
// by running demo.sh with:
//
//	script -q --timing=demo.timing -c 'bash --norc --noprofile demo.sh' demo.typescript
//
// on a terminal of 48 by 6 cells. Pass --typescript and --timing to replay
// another, and --cols and --rows for the size of the terminal it was
// recorded on.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lrstanley/clix/v2"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
)

var (
	//go:embed demo.typescript
	demoTypescript []byte
	//go:embed demo.timing
	demoTiming []byte
)

// Flags are the command's flags.
type Flags struct {
	Dir        string `name:"dir" type:"path" default:"." help:"Directory to write the files to."`
	Typescript string `name:"typescript" type:"existingfile" and:"recording" help:"Typescript to replay instead of the bundled one."`
	Timing     string `name:"timing" type:"existingfile" and:"recording" help:"Timing file of --typescript."`
	Cols       int    `name:"cols" default:"48" help:"Terminal width the recording was made on."`
	Rows       int    `name:"rows" default:"6" help:"Terminal height the recording was made on."`
}

func main() {
	cli := clix.NewWithDefaults[Flags]()
	cli.Execute(func(ctx context.Context, _ *slog.Logger) error {
		ts, tm := io.Reader(bytes.NewReader(demoTypescript)), io.Reader(bytes.NewReader(demoTiming))
		if cli.Flags.Typescript != "" {
			f, err := os.Open(cli.Flags.Typescript)
			if err != nil {
				return err
			}
			defer f.Close()
			g, err := os.Open(cli.Flags.Timing)
			if err != nil {
				return err
			}
			defer g.Close()
			ts, tm = f, g
		}
		files, err := run(ctx, cli.Flags.Dir, cli.Flags.Cols, cli.Flags.Rows, ts, tm)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println(f)
		}
		return nil
	})
}

// run replays the typescript ts, timed by tm, on a terminal of cols by rows
// cells, and exports it into dir, returning the paths written.
func run(ctx context.Context, dir string, cols, rows int, ts, tm io.Reader) ([]string, error) {
	s, err := reel.Open(ctx, cols, rows, reel.WithTitle("reel attach"))
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err = reel.Attach(ctx, s, ts, reel.WithTiming(tm)); err != nil {
		return nil, err
	}

	files := []string{filepath.Join(dir, "attach.gif"), filepath.Join(dir, "attach.cast")}
	err = reel.Export(ctx, s.Timeline(), reel.Profile{
		Outputs: reel.Files(files...),
		Render:  []render.Option{render.WithWindowChrome(true)},
		Effects: []reel.Effect{effects.Pause()},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
