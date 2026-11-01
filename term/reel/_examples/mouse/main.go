// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command mouse records a list of files that is clicked, drag selected, and
// right clicked, and exports it as a GIF that draws the mouse with
// effects.Mouse: the pointer gliding from cell to cell, a ripple at each
// press in the button's color, a trail along the drag, and the pointer
// hiding once the mouse is idle. The example
// draws the list itself, writing output to the session as a program would,
// and turns on mouse reporting so the session sends the mouse events.
package main

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/lrstanley/clix/v2"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
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

// names are the files listed, one per row from the second.
var names = []string{"README.md", "go.mod", "main.go", "main_test.go"}

// list draws the files, those in selected in reverse video, under a title,
// with status on the last row.
func list(selected map[int]bool, status string) string {
	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J\x1b[1mfiles\x1b[0m\r\n")
	for i, name := range names {
		if selected[i] {
			b.WriteString("\x1b[7m")
		}
		fmt.Fprintf(&b, " %-14s\x1b[0m\r\n", name)
	}
	fmt.Fprintf(&b, "\x1b[7;1H\x1b[2m%s\x1b[0m", status)
	return b.String()
}

// run records the session and exports it into dir, returning the paths
// written.
func run(ctx context.Context, dir string) ([]string, error) {
	s, err := reel.Open(ctx, 36, 7, reel.WithTitle("reel mouse"))
	if err != nil {
		return nil, err
	}
	defer s.Close()

	// Button-event mouse tracking (1002) with SGR reports (1006), so the
	// session sends presses, releases, and motion while a button is held.
	steps := []func() error{
		func() error { return write(s, "\x1b[?25l\x1b[?1002h\x1b[?1006h"+list(nil, "click a file")) },
		func() error { return s.Click(vt.MouseLeft, 4, 1) },
		func() error { return write(s, list(map[int]bool{0: true}, "README.md")) },
		func() error {
			return s.Drag(vt.MouseLeft, image.Pt(3, 2), image.Pt(10, 4), reel.Delay(40*time.Millisecond))
		},
		func() error { return write(s, list(map[int]bool{1: true, 2: true, 3: true}, "3 files")) },
		func() error { return s.Click(vt.MouseRight, 6, 3) },
		func() error {
			return write(s, list(map[int]bool{1: true, 2: true, 3: true}, "open · rename · delete"))
		},
	}
	for _, step := range steps {
		if err = step(); err != nil {
			return nil, err
		}
		time.Sleep(500 * time.Millisecond)
	}
	// The recording ends at its last event, so the status is written again
	// once the idle pointer has hidden.
	time.Sleep(2500 * time.Millisecond)
	if err = write(s, list(map[int]bool{1: true, 2: true, 3: true}, "3 files")); err != nil {
		return nil, err
	}

	files := []string{filepath.Join(dir, "mouse.gif")}
	err = reel.Export(ctx, s.Timeline(), reel.Profile{
		Outputs: reel.MustFiles(files...),
		Render:  []render.Option{render.WithWindowChrome(true), render.WithPadding(14)},
		Effects: []reel.Effect{effects.Mouse()},
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// write writes output to s, as a program would.
func write(s *reel.Session, output string) error {
	_, err := s.Write([]byte(output))
	return err
}
