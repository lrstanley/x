// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command reel records terminals and renders recordings without writing Go:
// it runs VHS tapes, records commands, drives programs for agents and
// tests, and turns asciicast files into GIFs, videos, and stills.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/lrstanley/clix/v2"
	"github.com/lrstanley/x/term/reel"
)

// Flags are reel's commands.
type Flags struct {
	Run     RunCmd     `cmd:"" help:"Run VHS tapes and write their outputs."`
	Test    TestCmd    `cmd:"" help:"Run tapes as tests: a failed Wait or Snapshot fails the tape."`
	Rec     RecCmd     `cmd:"" help:"Record a command, or your shell, in this terminal."`
	Session SessionCmd `cmd:"" aliases:"s" help:"Drive a program in a detached, recorded terminal, one command at a time."`
	Export  ExportCmd  `cmd:"" help:"Render an asciicast file through reel's renderer and effects."`
	Still   StillCmd   `cmd:"" help:"Render one moment of an asciicast file."`
	Cut     CutCmd     `cmd:"" help:"Cut an asciicast file down to the window between two marks."`
	Convert ConvertCmd `cmd:"" help:"Convert an asciicast file between versions 2 and 3."`
	Attach  AttachCmd  `cmd:"" help:"Render a script(1) typescript."`
	Play    PlayCmd    `cmd:"" help:"Replay an asciicast file in this terminal."`
	Info    InfoCmd    `cmd:"" help:"Describe an asciicast file."`
	Fonts   FontsCmd   `cmd:"" help:"List the font families reel can draw with."`
	Themes  ThemesCmd  `cmd:"" help:"List the named themes."`
}

func main() {
	cli := clix.New(
		clix.WithAppInfo[Flags](clix.AppInfo{
			Name:        "reel",
			Description: "Record terminals, and render recordings.",
		}),
		clix.WithVersionPlugin[Flags](),
		clix.WithMarkdownPlugin[Flags](),
		clix.WithKongOptions[Flags](kong.Vars{"spec": specHelp}),
	)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cli.Context.BindTo(ctx, (*context.Context)(nil))
	err := cli.Context.Run()
	stop()
	if err == nil {
		return
	}
	if msg := err.Error(); msg != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	}
	if hint := hintFor(err); hint != "" {
		fmt.Fprintf(os.Stderr, "hint: %s\n", hint)
	}
	var coder kong.ExitCoder
	if errors.As(err, &coder) {
		os.Exit(coder.ExitCode())
	}
	os.Exit(1)
}

// specHelp describes the SPEC of the region effects (see package spec).
const specHelp = "SPEC is REGION[;KEY=VALUE]..., with REGION one of row=Y, rows=Y0-Y1, cells=X0,Y0,X1,Y1, " +
	"text=TEXT, re=PATTERN, or env=NAME; every effect takes between=FROM,TO (marks), and highlight takes " +
	"ease=DUR dim=FACTOR outline=#RRGGBB[:PX] padding=PX, zoom ease=DUR scale=FACTOR anchor=X,Y, " +
	"and redact style=blur|block|replace:CHAR."

// Exit codes, beyond 1 for any other error and kong's 80 for usage.
const (
	exitWaitTimeout = 3 // A wait timed out, or a test failed.
	exitNoSession   = 4 // No such session.
)

// exitError is an error with its exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }
func (e *exitError) ExitCode() int { return e.code }

// hintFor returns what to do about err, or "".
func hintFor(err error) string {
	if errors.Is(err, reel.ErrRawOutput) {
		return "a .cast output stores the raw recording, so redaction and cuts (--redact*, --hide, a tape's " +
			"Hide or Redact) cannot apply to it; write the .cast in a separate run without them, or drop it"
	}
	return ""
}

// warnf prints a warning.
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "reel: warning: "+format+"\n", args...)
}
