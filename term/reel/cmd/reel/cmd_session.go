// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/live"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/look"
)

// SessionCmd drives programs in detached sessions.
type SessionCmd struct {
	Name string `name:"session" short:"s" env:"REEL_SESSION" placeholder:"NAME" help:"Session to drive; defaults to the only one running."`
	JSON bool   `name:"json" help:"Print responses, and errors, as JSON."`

	Start  SessionStartCmd  `cmd:"" help:"Start a program in a new session, and print its name."`
	Serve  SessionServeCmd  `cmd:"" hidden:"" help:"Run a session's server (started by start)."`
	List   SessionListCmd   `cmd:"" aliases:"ls" help:"List the running sessions."`
	Type   SessionTypeCmd   `cmd:"" help:"Type text."`
	Key    SessionKeyCmd    `cmd:"" help:"Press keys, such as enter, ctrl+c, shift+tab, f5, or a."`
	Paste  SessionPasteCmd  `cmd:"" help:"Paste text."`
	Click  SessionClickCmd  `cmd:"" help:"Click a cell."`
	Scroll SessionScrollCmd `cmd:"" help:"Scroll the viewport through the scrollback."`
	Resize SessionResizeCmd `cmd:"" help:"Resize the terminal."`
	Mark   SessionMarkCmd   `cmd:"" help:"Mark this moment in the recording."`
	Wait   SessionWaitCmd   `cmd:"" help:"Wait for the screen to show, or stop showing, something."`
	Screen SessionScreenCmd `cmd:"" help:"Print the screen."`
	Still  SessionStillCmd  `cmd:"" help:"Render the screen as it is now."`
	Do     SessionDoCmd     `cmd:"" help:"Run tape commands, such as Type, Enter, and Wait."`
	Export SessionExportCmd `cmd:"" help:"Render what the session has recorded so far."`
	Stop   SessionStopCmd   `cmd:"" help:"End the program and the session, optionally rendering its recording first."`
}

// AfterApply binds the session flags for the subcommands.
func (c *SessionCmd) AfterApply(kctx *kong.Context) error { //nolint:unparam // kong's hook signature.
	kctx.Bind(c)
	return nil
}

// target returns the session to drive: the one named, or the only one
// running.
func (c *SessionCmd) target(ctx context.Context) (dir, name string, err error) {
	if dir, err = live.Dir(); err != nil {
		return "", "", err
	}
	if c.Name != "" {
		return dir, c.Name, nil
	}
	sessions, err := live.List(ctx, dir)
	if err != nil {
		return "", "", err
	}
	switch len(sessions) {
	case 1:
		return dir, sessions[0].Name, nil
	case 0:
		return "", "", &exitError{code: exitNoSession, err: errors.New("no sessions are running; start one with \"reel session start -- COMMAND\"")}
	}
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Name
	}
	return "", "", &exitError{code: exitNoSession, err: fmt.Errorf("%d sessions are running (%s); pick one with --session or REEL_SESSION", len(names), strings.Join(names, ", "))}
}

// call sends req to the target session, and prints the response as JSON
// with --json. Errors get their exit code.
func (c *SessionCmd) call(ctx context.Context, req live.Request) (*live.Response, error) {
	dir, name, err := c.target(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := live.Call(ctx, dir, name, req)
	printed := c.JSON && resp != nil
	if printed {
		if perr := printJSON(resp); perr != nil {
			return nil, perr
		}
	}
	var lerr *live.Error
	switch {
	case errors.Is(err, live.ErrNoSession):
		return nil, &exitError{code: exitNoSession, err: err}
	case errors.As(err, &lerr) && lerr.Code == live.CodeTimeout:
		return nil, &exitError{code: exitWaitTimeout, err: quiet(printed, err)}
	case err != nil:
		return nil, quiet(printed, err)
	}
	return resp, nil
}

// quiet returns err without its message when it was already printed as
// JSON, so it is not printed twice.
func quiet(printed bool, err error) error {
	if printed {
		return &exitError{code: 1, err: errors.New("")}
	}
	return err
}

func printJSON(v any) error {
	return jsonv1.NewEncoder(os.Stdout).Encode(v)
}

// timeline fetches what the target session has recorded so far.
func (c *SessionCmd) timeline(ctx context.Context) (*reel.Timeline, error) {
	quiet := *c
	quiet.JSON = false // The cast is not for printing.
	resp, err := quiet.call(ctx, live.Request{Op: live.OpCast})
	if err != nil {
		return nil, err
	}
	return reel.ReadTimeline(bytes.NewReader(resp.Cast))
}

// SessionStartCmd starts a session.
type SessionStartCmd struct {
	Name    string        `name:"name" short:"n" placeholder:"NAME" help:"Session name; defaults to the program's, made unique."`
	Size    string        `name:"size" default:"80x24" placeholder:"COLSxROWS" help:"Terminal size."`
	Env     []string      `name:"env" short:"e" placeholder:"KEY=VALUE" help:"Environment variable for the program, repeatable."`
	Dir     string        `name:"dir" type:"existingdir" placeholder:"DIR" help:"Working directory for the program."`
	Linger  time.Duration `name:"linger" default:"10m" help:"How long the session stays up after the program exits, to read its last screen and recording."`
	Command []string      `arg:"" placeholder:"COMMAND" help:"Program to run, and its arguments (after --)."`
}

// Run starts the session's server in the background, and prints the
// session's name once it is listening.
func (c *SessionStartCmd) Run(ctx context.Context, sc *SessionCmd) error {
	if _, _, err := parseSize(c.Size); err != nil {
		return err
	}
	dir, err := live.Dir()
	if err != nil {
		return err
	}
	name := c.Name
	if name == "" {
		if name, err = freeName(ctx, dir, filepath.Base(c.Command[0])); err != nil {
			return err
		}
	}
	if !live.ValidName(name) {
		return fmt.Errorf("invalid session name %q: use letters, digits, '.', '_', or '-'", name)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"session", "serve", "--name", name, "--size", c.Size, "--linger", c.Linger.String()}
	for _, e := range c.Env {
		args = append(args, "--env", e)
	}
	if c.Dir != "" {
		args = append(args, "--dir", c.Dir)
	}
	args = append(args, "--")
	args = append(args, c.Command...)

	// The server says it is ready, or why it is not, on its standard
	// output, then closes it; its log goes to a file next to its socket,
	// appended to, as a server of the same name may be running.
	logFile, err := os.OpenFile(live.Log(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	// The server outlives this command, so it is not canceled with it.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), self, args...) //nolint:gosec // reel runs itself.
	cmd.Stderr = logFile
	cmd.SysProcAttr = detached()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		return err
	}
	status, _ := io.ReadAll(io.LimitReader(out, 64<<10))
	_ = cmd.Process.Release()
	if msg := strings.TrimSpace(string(status)); msg != "ready" {
		if msg == "" {
			msg = "the session's server exited; see " + logFile.Name()
		}
		return errors.New(msg)
	}
	if sc.JSON {
		return printJSON(map[string]string{"name": name})
	}
	_, err = fmt.Fprintln(os.Stdout, name)
	return err
}

// freeName returns base, or base-2, base-3, and so on, whichever no
// running session has.
func freeName(ctx context.Context, dir, base string) (string, error) {
	base = strings.Map(func(r rune) rune {
		if live.ValidName(string(r)) {
			return r
		}
		return '-'
	}, base)
	if !live.ValidName(base) {
		base = "session"
	}
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		_, err := live.Call(ctx, dir, name, live.Request{Op: live.OpInfo})
		switch {
		case errors.Is(err, live.ErrNoSession):
			return name, nil
		case err != nil:
			return "", err
		}
	}
}

// SessionServeCmd runs a session's server.
type SessionServeCmd struct {
	Name    string        `name:"name" required:""`
	Size    string        `name:"size" required:""`
	Env     []string      `name:"env"`
	Dir     string        `name:"dir"`
	Linger  time.Duration `name:"linger"`
	Command []string      `arg:""`
}

// Run serves the session until it stops.
func (c *SessionServeCmd) Run(ctx context.Context) error {
	cols, rows, err := parseSize(c.Size)
	if err != nil {
		return err
	}
	dir, err := live.Dir()
	if err != nil {
		return err
	}
	err = live.Serve(ctx, live.Config{
		Dir: dir, Name: c.Name, Cols: cols, Rows: rows, Command: c.Command,
		Env: c.Env, WorkDir: c.Dir, Linger: c.Linger,
		Ready: func(err error) {
			msg := "ready"
			if err != nil {
				msg = err.Error()
			}
			fmt.Fprintln(os.Stdout, msg)
			_ = os.Stdout.Close()
		},
	})
	if err == nil {
		// The log, which start opened for it, is only kept for a failure.
		_ = os.Remove(live.Log(dir, c.Name))
	}
	return err
}

// SessionListCmd lists sessions.
type SessionListCmd struct{}

// Run prints the sessions.
func (SessionListCmd) Run(ctx context.Context, sc *SessionCmd) error {
	dir, err := live.Dir()
	if err != nil {
		return err
	}
	sessions, err := live.List(ctx, dir)
	if err != nil {
		return err
	}
	if sc.JSON {
		return printJSON(sessions)
	}
	rows := make([][]string, len(sessions))
	for i, s := range sessions {
		status := "running"
		if s.Exited {
			status = fmt.Sprintf("exited %d", s.Status)
		}
		rows[i] = []string{
			s.Name, strconv.Itoa(s.PID), fmt.Sprintf("%dx%d", s.Cols, s.Rows), status,
			s.Started.Local().Format(time.DateTime), strings.Join(s.Command, " "),
		}
	}
	return printTable([]string{"NAME", "PID", "SIZE", "STATUS", "STARTED", "COMMAND"}, rows,
		"No sessions are running.", "reel session start -- COMMAND")
}

// SessionTypeCmd types text.
type SessionTypeCmd struct {
	Text  string        `arg:"" help:"Text to type."`
	Enter bool          `name:"enter" help:"Press enter after it."`
	Delay time.Duration `name:"delay" placeholder:"DUR" help:"Pause after each key, so a recording shows it typed."`
}

// Run types the text.
func (c *SessionTypeCmd) Run(ctx context.Context, sc *SessionCmd) error {
	text := c.Text
	if c.Enter {
		text += "\r"
	}
	_, err := sc.call(ctx, live.Request{Op: live.OpType, Text: text, Delay: c.Delay})
	return err
}

// SessionKeyCmd presses keys.
type SessionKeyCmd struct {
	Keys   []string      `arg:"" placeholder:"KEY" help:"Keys to press, in order."`
	Repeat int           `name:"repeat" short:"r" default:"1" help:"Press each key this many times."`
	Delay  time.Duration `name:"delay" placeholder:"DUR" help:"Pause after each press."`
}

// Run presses the keys.
func (c *SessionKeyCmd) Run(ctx context.Context, sc *SessionCmd) error {
	_, err := sc.call(ctx, live.Request{Op: live.OpKey, Keys: c.Keys, Repeat: c.Repeat, Delay: c.Delay})
	return err
}

// SessionPasteCmd pastes text.
type SessionPasteCmd struct {
	Text string `arg:"" help:"Text to paste."`
}

// Run pastes the text.
func (c *SessionPasteCmd) Run(ctx context.Context, sc *SessionCmd) error {
	_, err := sc.call(ctx, live.Request{Op: live.OpPaste, Text: c.Text})
	return err
}

// SessionClickCmd clicks a cell.
type SessionClickCmd struct {
	X      int    `arg:"" help:"Column, from 0."`
	Y      int    `arg:"" help:"Row, from 0."`
	Button string `name:"button" default:"left" enum:"left,middle,right" help:"Button to click."`
}

// Run clicks.
func (c *SessionClickCmd) Run(ctx context.Context, sc *SessionCmd) error {
	_, err := sc.call(ctx, live.Request{Op: live.OpClick, X: c.X, Y: c.Y, Button: c.Button})
	return err
}

// SessionScrollCmd scrolls the viewport.
type SessionScrollCmd struct {
	Rows int  `arg:"" help:"Rows to scroll."`
	Down bool `name:"down" help:"Scroll down, toward the screen, instead of up into the scrollback."`
}

// Run scrolls.
func (c *SessionScrollCmd) Run(ctx context.Context, sc *SessionCmd) error {
	n := c.Rows
	if c.Down {
		n = -n
	}
	_, err := sc.call(ctx, live.Request{Op: live.OpScroll, Repeat: n})
	return err
}

// SessionResizeCmd resizes the terminal.
type SessionResizeCmd struct {
	Size string `arg:"" placeholder:"COLSxROWS" help:"New size."`
}

// Run resizes.
func (c *SessionResizeCmd) Run(ctx context.Context, sc *SessionCmd) error {
	cols, rows, err := parseSize(c.Size)
	if err != nil {
		return err
	}
	_, err = sc.call(ctx, live.Request{Op: live.OpResize, Cols: cols, Rows: rows})
	return err
}

// SessionMarkCmd marks the recording.
type SessionMarkCmd struct {
	Label string `arg:"" help:"Label, by convention category:value, such as step:2."`
}

// Run marks.
func (c *SessionMarkCmd) Run(ctx context.Context, sc *SessionCmd) error {
	_, err := sc.call(ctx, live.Request{Op: live.OpMark, Label: c.Label})
	return err
}

// SessionWaitCmd waits for the screen.
type SessionWaitCmd struct {
	Text       string        `name:"text" xor:"cond" required:"" placeholder:"TEXT" help:"Wait for this text to show."`
	Regexp     string        `name:"regexp" xor:"cond" required:"" placeholder:"PATTERN" help:"Wait for a match of this regular expression."`
	Gone       string        `name:"gone" xor:"cond" required:"" placeholder:"TEXT" help:"Wait for this text to stop showing."`
	Idle       time.Duration `name:"idle" xor:"cond" required:"" placeholder:"DUR" help:"Wait for the screen not to change for this long."`
	CursorLine bool          `name:"cursor-line" help:"Look only at the cursor's line."`
	Timeout    time.Duration `name:"timeout" default:"15s" help:"Give up after this long, with exit code 3."`
	Print      bool          `name:"print" short:"p" help:"Print the screen once the wait ends."`
}

// Run waits.
func (c *SessionWaitCmd) Run(ctx context.Context, sc *SessionCmd) error {
	resp, err := sc.call(ctx, live.Request{Op: live.OpWait, Wait: &live.Wait{
		Text: c.Text, Regexp: c.Regexp, Gone: c.Gone, Idle: c.Idle, CursorLine: c.CursorLine, Timeout: c.Timeout,
	}})
	if err == nil && c.Print && !sc.JSON {
		_, err = fmt.Fprintln(os.Stdout, resp.Text)
	}
	return err
}

// SessionScreenCmd prints the screen.
type SessionScreenCmd struct {
	Format string `name:"format" short:"f" default:"text" enum:"text,json" help:"text, or json for the cells, styles, and cursor (with --json, the screen is in the response either way)."`
}

// Run prints the screen.
func (c *SessionScreenCmd) Run(ctx context.Context, sc *SessionCmd) error {
	resp, err := sc.call(ctx, live.Request{Op: live.OpScreen, JSON: c.Format == "json" || sc.JSON})
	switch {
	case err != nil || sc.JSON:
		return err
	case c.Format == "json":
		return json.MarshalWrite(os.Stdout, resp.Screen, jsontext.Multiline(true))
	}
	_, err = fmt.Fprintln(os.Stdout, resp.Text)
	return err
}

// SessionStillCmd renders the screen.
type SessionStillCmd struct {
	look.Look `embed:""`

	Output string `short:"o" name:"output" required:"" placeholder:"FILE" help:"File to write: .png, .snap, or .json."`
}

// Run renders the screen as it is now, from the recording so far.
func (c *SessionStillCmd) Run(ctx context.Context, sc *SessionCmd) error {
	tl, err := sc.timeline(ctx)
	if err != nil {
		return err
	}
	still := StillCmd{Output: c.Output, Look: c.Look}
	return still.render(ctx, tl)
}

// SessionDoCmd runs tape commands.
type SessionDoCmd struct {
	Tape string `arg:"" placeholder:"TAPE" help:"Tape file, or - for standard input. Its settings and outputs are ignored."`
}

// Run runs the tape's commands.
func (c *SessionDoCmd) Run(ctx context.Context, sc *SessionCmd) error {
	var src []byte
	var err error
	if c.Tape == "-" {
		src, err = io.ReadAll(bufio.NewReader(os.Stdin))
	} else {
		src, err = os.ReadFile(c.Tape)
	}
	if err != nil {
		return err
	}
	_, err = sc.call(ctx, live.Request{Op: live.OpDo, Text: string(src)})
	return err
}

// SessionExportCmd renders the recording so far.
type SessionExportCmd struct {
	RenderFlags `embed:""`
}

// Run exports the recording.
func (c *SessionExportCmd) Run(ctx context.Context, sc *SessionCmd) error {
	tl, err := sc.timeline(ctx)
	if err != nil {
		return err
	}
	p, err := c.profile()
	if err != nil {
		return err
	}
	return reel.Export(ctx, tl, p)
}

// SessionStopCmd stops a session.
type SessionStopCmd struct {
	RenderFlags `embed:""`
}

// Run renders the recording when outputs are given, then stops the
// session. A failed render leaves the session running, so its recording
// is not lost.
func (c *SessionStopCmd) Run(ctx context.Context, sc *SessionCmd) error {
	// The session the recording is from must be the one stopped, even if
	// "the only one running" changes in between.
	_, name, err := sc.target(ctx)
	if err != nil {
		return err
	}
	sc.Name = name
	if len(c.Outputs) > 0 {
		p, profileErr := c.profile()
		if profileErr != nil {
			return profileErr
		}
		tl, castErr := sc.timeline(ctx)
		if castErr != nil {
			return castErr
		}
		if err = reel.Export(ctx, tl, p); err != nil {
			return fmt.Errorf("%w (session %q is still running)", err, name)
		}
	}
	_, err = sc.call(ctx, live.Request{Op: live.OpStop})
	return err
}
