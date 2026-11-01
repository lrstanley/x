// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/play"
	"github.com/lrstanley/x/term/tape"
)

// TapeFlags are how tapes are read.
type TapeFlags struct {
	Deny []string `name:"deny" sep:"," placeholder:"COMMAND,..." help:"Make tapes that use these commands fail, such as Env,Source,Require for tapes you do not trust."`
	Skip []string `name:"skip" sep:"," placeholder:"COMMAND,..." help:"Ignore these commands, such as Screenshot."`
}

// compile parses and compiles the tape at path, with opts.
func (f TapeFlags) compile(path string, opts ...play.CompileOption) (*play.Tape, error) {
	opts = append(opts,
		play.WithCommands(func(cmds tape.Commands) {
			for _, name := range f.Deny {
				cmds[name] = tape.Fail
			}
			for _, name := range f.Skip {
				cmds[name] = tape.Skip
			}
		}),
		play.WithReport(func(cmd tape.Command, action tape.Action) {
			if action == tape.Skip {
				warnf("%s: skipped %s", cmd.Name.Pos, cmd.Name.Text)
			}
		}),
	)
	return play.ParseFile(path, opts...)
}

// RunCmd runs tapes.
type RunCmd struct {
	TapeFlags   `embed:""`
	RenderFlags `embed:""`

	Tapes []string `arg:"" type:"existingfile" placeholder:"TAPE" help:"Tapes to run, in order."`
}

// Run runs each tape, and stops at the first that fails.
func (c *RunCmd) Run(ctx context.Context) error {
	for _, path := range c.Tapes {
		if err := c.run(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (c *RunCmd) run(ctx context.Context, path string) error {
	tp, err := c.compile(path)
	if err != nil {
		return err
	}
	err = c.Validate()
	if err != nil {
		return err
	}
	outputs, err := c.files()
	if err != nil {
		return err
	}
	fx, err := c.effects()
	if err != nil {
		return err
	}
	res, err := tp.Run(ctx,
		play.WithLook(c.Look),
		play.WithProfile(reel.Profile{Outputs: outputs, Effects: fx, Vars: tapeVars(c.Vars, path)}),
	)
	for _, w := range res.Warnings {
		warnf("%s", w)
	}
	if err != nil {
		return err
	}
	vars, err := res.Timeline.PathVars(res.Profile.Vars)
	if err != nil {
		return err
	}
	for _, f := range res.Profile.Outputs {
		if p, expandErr := reel.ExpandPath(f.Path, vars); expandErr == nil {
			f.Path = p // Outputs of marks keep their {mark}.
		}
		fmt.Fprintf(os.Stderr, "%s: wrote %s\n", path, f.Path)
	}
	return nil
}

// tapeVars returns vars with {tape}, the tape's file name without its
// extension, so one -o can name the outputs of several tapes.
func tapeVars(vars map[string]string, path string) map[string]string {
	out := map[string]string{"tape": strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	maps.Copy(out, vars)
	return out
}
