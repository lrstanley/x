// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command zoom exports a recording of steep's reel-showcase example as a
// GIF that zooms in on the deploy pipeline while billing deploys: once the
// deploy key is pressed, the grid eases in to show the pipeline panel as
// large as it fits, drawn again at that size so the text stays sharp,
// inside the window chrome, and eases back out once the tests have failed.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"log"
	"path/filepath"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/_examples/internal/showcase"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
)

func main() {
	dir := flag.String("dir", ".", "directory to write the files to")
	flag.Parse()
	files, err := run(context.Background(), *dir)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		fmt.Println(f)
	}
}

// run exports the showcase into dir, returning the paths written.
func run(ctx context.Context, dir string) ([]string, error) {
	tl, err := showcase.Timeline()
	if err != nil {
		return nil, err
	}
	files := []string{filepath.Join(dir, "zoom.gif")}
	err = reel.Export(ctx, tl, reel.Profile{
		Outputs: reel.Files(files...),
		Render:  []render.Option{render.WithWindowChrome(true), render.WithPadding(14)},
		Effects: []reel.Effect{
			// The pipeline panel, at the right of the middle row. "input-8"
			// is the mark of the key that deploys billing, and the tests
			// have failed by "settle-13".
			effects.Zoom(
				effects.Cells(image.Rect(64, 11, 109, 22)),
				effects.Between("input-8", "settle-13"),
				effects.Ease(300*time.Millisecond),
			),
			// After the zoom, so the keys keep their size and place.
			effects.Keys(time.Second),
			effects.PauseBadge(),
		},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
