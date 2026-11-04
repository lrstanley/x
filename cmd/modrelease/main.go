// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command modrelease releases every Go module in a repository at one version.
//
// It rehearses the whole release locally against the versions about to be
// published, then publishes one commit and all of its tags in a single atomic
// push. Without --apply, it stops after the rehearsal.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/lrstanley/clix/v2"
)

// Flags are the command's arguments. --apply deliberately has no env var, so a
// shell profile or task runner can't turn a plan into a publish.
type Flags struct {
	Bump   string   `arg:"" name:"bump" help:"major, minor, patch, or an explicit version like v1.2.3-beta.1."`
	Apply  bool     `name:"apply" help:"Publish the release. Without this, only checks, rehearses, and prints the plan."`
	Alpha  bool     `name:"alpha" xor:"channel" help:"Release the bump as an alpha, like v1.3.0-alpha.0."`
	Beta   bool     `name:"beta" xor:"channel" help:"Release the bump as a beta, like v1.3.0-beta.0."`
	RC     bool     `name:"rc" xor:"channel" help:"Release the bump as a release candidate, like v1.3.0-rc.0."`
	Skip   []string `name:"skip" sep:"," env:"MODRELEASE_SKIP" placeholder:"DIR" help:"Module directories to leave out of the release."`
	Remote string   `name:"remote" default:"origin" env:"MODRELEASE_REMOTE" help:"Git remote to publish to."`
}

// channel returns the prerelease channel the flags ask for, or "".
func (f Flags) channel() string {
	switch {
	case f.Alpha:
		return "alpha"
	case f.Beta:
		return "beta"
	case f.RC:
		return "rc"
	default:
		return ""
	}
}

// Validate rejects a bad bump before anything touches the repository. Kong calls
// it during parsing.
func (f Flags) Validate() error {
	if slices.Contains(bumpKeywords, f.Bump) {
		return nil
	}
	if channel := f.channel(); channel != "" {
		return fmt.Errorf("--%s can't be combined with the explicit version %s", channel, f.Bump)
	}
	if _, err := parseVersion(f.Bump); err != nil {
		return fmt.Errorf("%w, or one of major, minor, or patch", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

func run() error {
	cli := clix.New(
		clix.WithAppInfo[Flags](clix.AppInfo{
			Name:        "modrelease",
			Description: "Release every Go module in a repository at one version.",
		}),
		clix.WithVersionPlugin[Flags](),
		clix.WithMarkdownPlugin[Flags](),
	)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := &release{
		flags:             *cli.Flags,
		out:               os.Stdout,
		debug:             cli.Debug,
		hermeticGitConfig: os.DevNull,
		publicProxy:       defaultPublicProxy,
		proxyWait:         10 * time.Second,
	}
	return r.run(ctx)
}
