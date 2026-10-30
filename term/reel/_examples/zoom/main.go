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
	"fmt"
	"image"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/lrstanley/clix/v2"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/_examples/internal/showcase"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
)

// Flags are the command's flags.
type Flags struct {
	Dir string `name:"dir" type:"path" default:"." help:"Directory to write the files to."`
}

func main() {
	clix.NewWithDefaults[Flags]().Execute(func(ctx context.Context, cli *clix.CLI[Flags], _ *slog.Logger) error {
		files, err := run(ctx, cli.Flags.Dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Println(f) //nolint:forbidigo // The example lists the files it wrote.
		}
		return nil
	})
}

// run exports the showcase into dir, returning the paths written.
func run(ctx context.Context, dir string) ([]string, error) {
	tl, err := showcase.Timeline()
	if err != nil {
		return nil, err
	}
	files := []string{filepath.Join(dir, "zoom.gif")}
	err = reel.Export(ctx, tl, reel.Profile{
		Outputs: reel.MustFiles(files...),
		Render:  []render.Option{render.WithWindowChrome(true), render.WithPadding(14)},
		Effects: []reel.Effect{
			// The pipeline panel, at the right of the middle row. "input-8"
			// is the mark of the key that deploys billing, and the tests
			// have failed by "wait-13".
			effects.Zoom(
				effects.Cells(image.Rect(64, 11, 109, 22)),
				effects.Between("input-8", "wait-13"),
				effects.Ease(300*time.Millisecond),
			),
			// After the zoom, so the keys keep their size and place.
			effects.Keys(time.Second),
			effects.Pause(),
		},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
