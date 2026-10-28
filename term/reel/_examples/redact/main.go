// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command redact records a login prompt that a token is typed into, a key
// at a time, echoed as it is typed and printed once accepted, and exports
// it as a GIF that never shows the token. The token is the value of
// REEL_DEMO_TOKEN, a made-up one when it is unset: effects.Redact hides it
// wherever the screen shows it, from its third character on while it is
// typed, and effects.RedactInputs hides it from the keys overlay. A
// redacted recording can not be saved as asciicast, which is the timeline
// as recorded, so the profile has no ".cast" output.
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
)

// tokenEnv is the environment variable that holds the token.
const tokenEnv = "REEL_DEMO_TOKEN"

// Flags are the command's flags.
type Flags struct {
	Dir string `name:"dir" type:"path" default:"." help:"Directory to write the files to."`
}

func main() {
	cli := clix.NewWithDefaults[Flags]()
	cli.Execute(func(ctx context.Context, _ *slog.Logger) error {
		if os.Getenv(tokenEnv) == "" {
			if err := os.Setenv(tokenEnv, "ghp_r33lD3m0T0k3n4U"); err != nil {
				return err
			}
		}
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

// run records the login and exports it into dir, returning the paths
// written.
func run(ctx context.Context, dir string) ([]string, error) {
	tl, err := record(ctx, os.Getenv(tokenEnv))
	if err != nil {
		return nil, err
	}
	p := profile(dir)
	if err = reel.Export(ctx, tl, p); err != nil {
		return nil, err
	}
	return []string{p.Outputs[0].Path}, nil
}

// record records a session without a program, by writing to it directly
// and typing into it: a login that asks for token, which is typed a key at
// a time and echoed, as a prompt would, and then printed.
func record(ctx context.Context, token string) (*reel.Timeline, error) {
	s, err := reel.Open(ctx, 52, 6, reel.WithTitle("reel redact"))
	if err != nil {
		return nil, err
	}
	defer s.Close()
	// step pauses, types typed, as the host would, and writes out, as the
	// program would.
	step := func(pause time.Duration, typed, out string) error {
		time.Sleep(pause)
		if typed != "" {
			if err := s.Type(typed); err != nil {
				return err
			}
		}
		_, err := s.Write([]byte(out))
		return err
	}
	if err = step(0, "", "\x1b]2;reel redact\x07\x1b[1;36m$\x1b[0m "); err != nil {
		return nil, err
	}
	if err = step(300*time.Millisecond, "login\r", "login\r\nToken: "); err != nil {
		return nil, err
	}
	for _, r := range token {
		if err = step(70*time.Millisecond, string(r), string(r)); err != nil {
			return nil, err
		}
	}
	err = step(300*time.Millisecond, "\r", "\r\n\x1b[32m✔\x1b[0m logged in with "+token+"\r\n\x1b[1;36m$\x1b[0m ")
	if err != nil {
		return nil, err
	}
	time.Sleep(500 * time.Millisecond)
	return s.Timeline(), nil
}

// profile writes a GIF of the login into dir, with the token hidden.
func profile(dir string) reel.Profile {
	return reel.Profile{
		Outputs: reel.Files(filepath.Join(dir, "redact.gif")),
		Render:  []render.Option{render.WithWindowChrome(true), render.WithPadding(14)},
		Effects: []reel.Effect{
			// Before every other effect, so none of them sees the token.
			effects.Redact(effects.Env(tokenEnv), effects.Replace('•')),
			effects.RedactInputs(effects.Env(tokenEnv)),
			effects.Keys(time.Second),
			effects.Pause(),
		},
	}
}
