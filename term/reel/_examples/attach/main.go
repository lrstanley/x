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
// on a terminal of 48 by 6 cells. Pass -typescript and -timing to replay
// another, and -cols and -rows for the size of the terminal it was recorded
// on.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

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

func main() {
	dir := flag.String("dir", ".", "directory to write the files to")
	typescript := flag.String("typescript", "", "typescript to replay instead of the bundled one")
	timing := flag.String("timing", "", "timing file of -typescript")
	cols := flag.Int("cols", 48, "terminal width the recording was made on")
	rows := flag.Int("rows", 6, "terminal height the recording was made on")
	flag.Parse()

	ts, tm := io.Reader(bytes.NewReader(demoTypescript)), io.Reader(bytes.NewReader(demoTiming))
	if *typescript != "" {
		f, err := os.Open(*typescript)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		g, err := os.Open(*timing)
		if err != nil {
			log.Fatal(err)
		}
		defer g.Close()
		ts, tm = f, g
	}
	files, err := run(context.Background(), *dir, *cols, *rows, ts, tm)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		fmt.Println(f)
	}
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
		Effects: []reel.Effect{effects.PauseBadge()},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
