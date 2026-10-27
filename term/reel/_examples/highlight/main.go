// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command highlight exports a recording of steep's reel-showcase example
// as a GIF that highlights a service as it is deployed: once the deploy key
// is pressed, everything but the cells that show "billing" fades to half
// brightness, and an outline follows the name as the dashboard changes
// around it.
package main

import (
	"context"
	"fmt"
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

// run exports the showcase into dir, returning the paths written.
func run(ctx context.Context, dir string) ([]string, error) {
	tl, err := showcase.Timeline()
	if err != nil {
		return nil, err
	}
	files := []string{filepath.Join(dir, "highlight.gif")}
	err = reel.Export(ctx, tl, reel.Profile{
		Outputs: reel.Files(files...),
		Render:  []render.Option{render.WithWindowChrome(true), render.WithPadding(14)},
		Effects: []reel.Effect{
			// "input-8" is the mark of the key that deploys billing.
			effects.Highlight(
				effects.Text("billing"),
				effects.Outline(nil, 0),
				effects.Between("input-8", ""),
				effects.Ease(300*time.Millisecond),
			),
			// After the highlight, so the keys stay bright.
			effects.Keys(time.Second),
			effects.PauseBadge(),
		},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
