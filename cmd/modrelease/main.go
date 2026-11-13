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

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/kong"
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

	NoDispatch bool `name:"no-dispatch" help:"Don't start the tag workflows GitHub skips for a push of more than 3 tags."`

	GitHubRelease bool   `name:"github-release" env:"MODRELEASE_GITHUB_RELEASE" help:"After the push, create a GitHub release for the root tag, with notes grouped from the commits since the last release. Needs GH_TOKEN or GITHUB_TOKEN."`
	Highlights    string `name:"highlights" type:"path" placeholder:"FILE" help:"Markdown file to start the release notes with, above the changelog. An empty file adds nothing. See \"Writing highlights\" below. Needs --github-release."`
	Footer        string `name:"footer" type:"path" placeholder:"FILE" env:"MODRELEASE_FOOTER" help:"Go text/template file of Markdown to end the release notes with, instead of the default links. See the README for its fields. Needs --github-release."`
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
	if !f.GitHubRelease {
		switch {
		case f.Highlights != "":
			return errors.New("--highlights needs --github-release")
		case f.Footer != "":
			return errors.New("--footer needs --github-release")
		}
	}
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
			_, _ = lipgloss.Fprintf(os.Stderr, "%s %v\n", styleFail.Render("error:"), err)
		}
		os.Exit(1)
	}
}

// highlightsHelp ends --help, for whoever writes a release's highlights, which
// is often an AI agent. It's not in the app description, which --version also
// prints.
const highlightsHelp = `
Writing highlights (--highlights):
  Highlights are for a reader deciding whether to upgrade, and how. The
  changelog below them already lists every commit, so don't repeat it.
  Run the plan without them first, and read the changelog it prints.

  - Skip them when nothing stands out, like a release of only fixes and
    chores. No highlights beats filler.
  - Tell the story of the few changes that matter: what someone can do now
    that they couldn't before, or what behaves differently, and why.
    Combine the commits behind one change into one point.
  - Show, don't list: a short code example of a new API, or a before and
    after. Link to the docs or examples that cover it.
  - For a breaking change, say who it affects and what to change to upgrade.
  - Add a screenshot, GIF, or recording when a change is visual, like UI
    or terminal output. Use absolute URLs, like a file committed to the
    repository; local paths don't work in a GitHub release.
  - Leave out what users never see: refactors, CI, tests, and dependency
    bumps that change nothing for them.
  - Use "##" for headings, like the rest of the notes, and keep it short.`

func run() error {
	cli := clix.New(
		clix.WithAppInfo[Flags](clix.AppInfo{
			Name:        "modrelease",
			Description: "Release every Go module in a repository at one version.",
		}),
		clix.WithVersionPlugin[Flags](),
		clix.WithMarkdownPlugin[Flags](),
		clix.WithKongOptions[Flags](kong.Help(func(opts kong.HelpOptions, ctx *kong.Context) error {
			if err := kong.DefaultHelpPrinter(opts, ctx); err != nil {
				return err
			}
			_, err := fmt.Fprintln(ctx.Stdout, highlightsHelp)
			return err
		})),
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
