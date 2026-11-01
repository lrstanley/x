// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/look"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	syntax "github.com/lrstanley/x/term/tape"
)

// Result is what [Tape.Run] recorded.
type Result struct {
	// Timeline is the session's timeline, as recorded, including the marks
	// of Hide, Show, Screenshot, and Mark commands and one labeled
	// "tape:end" after the last command.
	Timeline *reel.Timeline
	// Profile is what the run's outputs were, or with [WithoutRecording]
	// would have been, written with.
	Profile reel.Profile
	// Warnings are about settings reel ignores or only approximates, each
	// starting with the position of its command.
	Warnings []string
}

// RunOption configures [Tape.Run].
type RunOption func(*runConfig)

type runConfig struct {
	program  func(ctx context.Context, s *reel.Session) error
	look     look.Look
	profile  reel.Profile
	noRecord bool
}

// WithProgram runs the tape's commands against an in-process program
// instead of its Set Shell shell: fn is started on the session in its own
// goroutine, and should read the session's input and write its output
// until ctx is done, which it is once the commands have run. An error fn
// returns before then fails the run. A tape with Set Shell, Env, or Require
// is an error, since there is no shell for them.
func WithProgram(fn func(ctx context.Context, s *reel.Session) error) RunOption {
	return func(cfg *runConfig) { cfg.program = fn }
}

// WithLook sets how the recording looks over the tape's settings: the
// fields l sets replace the tape's.
func WithLook(l look.Look) RunOption {
	return func(cfg *runConfig) { cfg.look = l }
}

// WithProfile sets outputs and effects over the tape's: p's outputs, when
// it has any, replace the tape's Output commands (but not its Screenshots),
// and its effects are added to the tape's. Its encoding options and path
// variables are kept; the rest of it comes from the tape and [WithLook].
func WithProfile(p reel.Profile) RunOption {
	return func(cfg *runConfig) { cfg.profile = p }
}

// WithoutRecording runs the tape without writing its outputs, such as to
// export [Result.Timeline] with [Result.Profile] only when the run fails.
func WithoutRecording() RunOption {
	return func(cfg *runConfig) { cfg.noRecord = true }
}

// Run runs the tape and writes its outputs. It checks every Require and
// output first, then opens a session, starts the target (its shell, or see
// [WithProgram]), runs the commands, and writes the outputs with
// [reel.Record] as the commands run.
//
// Timing is wall-clock: Sleep and typing really wait. An error from a
// command is a [*syntax.Error] at its position; a Wait that times out wraps
// the [*reel.WaitError], and reports the screen it last saw. Once the
// session is open, the Result holds its timeline even when Run fails.
//
// The screen is Columns by Rows when both are set. Otherwise it is the
// most cells of FontSize that fit Width by Height pixels with the padding,
// margin, and window bar (see [render.Renderer.Fit]). With Columns and Rows
// set, a Width or Height without FontSize sizes the font to fit them
// instead (see [reel.Profile.Fit]).
func (t *Tape) Run(ctx context.Context, opts ...RunOption) (*Result, error) {
	var cfg runConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	res := &Result{Warnings: slices.Clone(t.warnings)}
	if cfg.program != nil && t.host.Text != "" {
		return res, &syntax.Error{Pos: t.host.Pos, Err: fmt.Errorf("%s needs a shell, and the target is a program", t.host.Text)}
	}
	for _, req := range t.requires {
		if _, err := exec.LookPath(req.Text); err != nil {
			return res, &syntax.Error{Pos: req.Pos, Err: fmt.Errorf("unmet Require: %w", err)}
		}
	}
	st := t.settings
	st.Look = st.Merge(cfg.look)
	pl, err := t.plan(st, cfg, &res.Warnings)
	if err != nil {
		return res, err
	}
	res.Profile = pl.profile

	s, err := reel.Open(ctx, pl.cols, pl.rows, reel.WithTitle(pl.profile.Title))
	if err != nil {
		return res, err
	}
	defer s.Close()
	res.Timeline = s.Timeline()
	err = setColors(s, pl.palette)
	if err != nil {
		return res, err
	}

	stop := func() error { return nil }
	if !cfg.noRecord {
		recCtx, abandon := context.WithCancel(ctx)
		defer abandon()
		rec, recErr := reel.Record(recCtx, s, pl.profile)
		if recErr != nil {
			return res, recErr
		}
		stopped := false
		defer func() {
			if !stopped {
				abandon()
				_ = rec.Stop()
			}
		}()
		stop = func() error {
			stopped = true
			return rec.Stop()
		}
	}

	runCtx, fail := context.WithCancelCause(ctx)
	defer fail(nil)
	teardown, err := t.start(ctx, s, st, cfg.program, fail)
	if err != nil {
		return res, err
	}
	defer teardown()

	err = t.Play(runCtx, s)
	if err != nil {
		return res, err
	}
	err = s.Mark(endMark)
	if err != nil {
		return res, err
	}
	// teardown waits for a program, so its error, from any time before it
	// was stopped, is known after it.
	teardown()
	if err = context.Cause(runCtx); err != nil {
		return res, fmt.Errorf("tape: %w", err)
	}
	return res, stop()
}

// Play runs the tape's commands on s, which is already running its
// program, as [Tape.Run] does once it has started one. The tape's settings
// and outputs are not used, beyond the typing speeds and wait timeouts its
// commands were compiled with. An error is a [*syntax.Error] at the
// position of the command that failed, of the cause of ctx once it is
// done, such as a program that failed.
func (t *Tape) Play(ctx context.Context, s *reel.Session) error {
	r := &runner{s: s}
	for _, l := range t.steps {
		err := context.Cause(ctx)
		if err == nil {
			err = l.step.run(ctx, r)
		}
		if err != nil {
			return &syntax.Error{Pos: l.pos, Err: cmp.Or(context.Cause(ctx), err)}
		}
	}
	return nil
}

// start starts the target on s, and returns what stops it. fail is called
// with the error of a program that ends before it is stopped.
func (t *Tape) start(ctx context.Context, s *reel.Session, st settings, program func(context.Context, *reel.Session) error, fail context.CancelCauseFunc) (func(), error) {
	ctx, stop := context.WithCancel(ctx)
	if program == nil {
		sh := shells[st.shell]
		cmd := exec.CommandContext(ctx, sh.args[0], sh.args[1:]...) //nolint:gosec // A tape runs what it says.
		cmd.Env = slices.Concat(hostEnv(), sh.env, t.env)
		since := s.Terminal().OutputSeq()
		p, err := reel.Exec(ctx, s, cmd)
		if err != nil {
			stop()
			return nil, err
		}
		// Closing the session hangs up on the shell, and stop kills what
		// ignores that.
		teardown := sync.OnceFunc(func() {
			_ = s.Close()
			stop()
			_, _ = p.Wait()
		})
		// Keys typed before the shell reads them are echoed ahead of its
		// prompt, so wait for it to start: for its first output to go quiet,
		// or for at most WaitTimeout, as output that never stops never goes
		// quiet.
		wctx, cancel := context.WithTimeout(ctx, st.waitTimeout)
		defer cancel()
		err = s.WaitIdle(wctx, since, shellQuiet, st.waitTimeout)
		if err != nil && !errors.Is(wctx.Err(), context.DeadlineExceeded) {
			teardown()
			return nil, err
		}
		return teardown, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := program(ctx, s); err != nil && ctx.Err() == nil {
			fail(fmt.Errorf("program: %w", err))
		}
	}()
	return sync.OnceFunc(func() {
		stop()
		_ = s.Close()
		<-done
	}), nil
}

// shellQuiet is how long a starting shell's output must go quiet before
// the commands run.
const shellQuiet = 200 * time.Millisecond

// hostEnv is the host's environment without TERM and COLORTERM, which
// describe the host's terminal, so [reel.Exec] sets its own.
func hostEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" { // Names are case-insensitive there.
			name = strings.ToUpper(name)
		}
		return name == "TERM" || name == "COLORTERM"
	})
}

// setColors makes the theme's colors the terminal's defaults, so a program
// that asks for them, such as to pick light or dark colors, gets them.
func setColors(s *reel.Session, pal render.Palette) error {
	var errs []error
	if pal.DefaultForeground != nil {
		errs = append(errs, s.SetDefaultForeground(pal.DefaultForeground))
	}
	if pal.DefaultBackground != nil {
		errs = append(errs, s.SetDefaultBackground(pal.DefaultBackground))
	}
	if pal.Cursor != nil {
		errs = append(errs, s.SetDefaultCursorColor(pal.Cursor))
	}
	return errors.Join(errs...)
}

// plan is what a run needs from the settings: the screen, the theme, and
// the profile.
type plan struct {
	cols, rows int
	palette    render.Palette
	profile    reel.Profile
}

func (t *Tape) plan(st settings, cfg runConfig, warnings *[]string) (plan, error) {
	pl := plan{cols: st.cols, rows: st.rows, profile: cfg.profile}
	pal, _, err := st.Palette()
	if err != nil {
		return pl, err
	}
	pl.palette = pal

	// With Columns and Rows, a Width or Height without FontSize sizes the
	// font to fit them; otherwise the font sizes the screen.
	fontSize := defaultFontSize
	if st.FontSize != nil {
		fontSize = *st.FontSize
	}
	fit := pl.cols > 0 && pl.rows > 0 && st.sizeSet && st.FontSize == nil
	if fit {
		fontSize = 0
		pl.profile.Fit = image.Pt(st.width, st.height)
	}
	opts, warns, err := st.Render(fontSize)
	if err != nil {
		return pl, err
	}
	*warnings = append(*warnings, warns...)
	pl.profile.Render = opts
	if pl.cols == 0 || pl.rows == 0 {
		r, renderErr := render.New(opts...)
		if renderErr != nil {
			return pl, renderErr
		}
		cols, rows := r.Fit(st.width, st.height)
		_ = r.Close()
		pl.cols, pl.rows = cmp.Or(pl.cols, cols), cmp.Or(pl.rows, rows)
		if pl.cols == 0 || pl.rows == 0 {
			return pl, fmt.Errorf("tape: Width %d by Height %d has no room for text at FontSize %v, with the padding and margin", st.width, st.height, fontSize)
		}
	}

	outputs := t.outputs
	if len(cfg.profile.Outputs) > 0 {
		if len(t.outputs) > 0 {
			*warnings = append(*warnings, fmt.Sprintf("the outputs given replace the tape's %d Output commands", len(t.outputs)))
		}
		outputs = cfg.profile.Outputs
	}
	pl.profile.Outputs = slices.Concat(outputs, t.shots)
	if len(pl.profile.Outputs) == 0 && !cfg.noRecord {
		return pl, errors.New("tape: no outputs; add one, such as Output demo.gif")
	}
	if t.hide != nil {
		for _, f := range outputs {
			if filepath.Ext(f.Path) == ".cast" {
				return pl, &syntax.Error{Pos: *t.hide, Err: fmt.Errorf("an asciicast output (%s) holds the recording uncut, so the tape cannot Hide", f.Path)}
			}
		}
	}
	st.Profile(&pl.profile)
	pl.profile.Effects = look.Arrange(slices.Concat(cfg.profile.Effects, t.effects, t.hideEffects(), st.Effects()))
	return pl, nil
}

// hideEffects returns the [effects.Hide] that cuts what the tape's Hide
// and Show commands hide, if any.
func (t *Tape) hideEffects() []reel.Effect {
	var rules []effects.HideRule
	if t.leadingHide {
		rules = append(rules, effects.HideBetween("", showMark))
	}
	if t.spans {
		rules = append(rules, effects.HideBetween(hideMark, showMark))
	}
	if t.hideRest {
		rules = append(rules, effects.HideBetween(hideRestMark, ""))
	}
	if len(rules) == 0 {
		return nil
	}
	return []reel.Effect{effects.Hide(rules...)}
}

// step is one command a run carries out.
type step interface {
	run(ctx context.Context, r *runner) error
}

// runner is what steps share in a run.
type runner struct {
	s *reel.Session
	// clipboard is what Copy copied, for Paste. It is the run's own, not the
	// host's clipboard, as VHS uses.
	clipboard string
}

// sleep waits for d, or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// repeat runs press n times, each followed by a pause of d that ctx ends,
// unlike [reel.Delay]'s.
func repeat(ctx context.Context, n int, d time.Duration, press func() error) error {
	for range n {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if err := press(); err != nil {
			return err
		}
		if err := sleep(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

type typeStep struct {
	text  string
	delay time.Duration
}

func (c typeStep) run(ctx context.Context, r *runner) error { return Type(ctx, r.s, c.text, c.delay) }

// Type types text into s, pausing for delay after each key, until ctx is
// done; unlike [reel.Delay]'s, the pause ends with ctx.
func Type(ctx context.Context, s *reel.Session, text string, delay time.Duration) error {
	if delay <= 0 {
		return s.Type(text)
	}
	for _, key := range text {
		if err := s.Type(string(key)); err != nil {
			return err
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
	}
	return nil
}

// keyStep presses a key, named as [vt.ParseKey] names it.
type keyStep struct {
	key    string
	delay  time.Duration
	repeat int
}

func (c keyStep) run(ctx context.Context, r *runner) error {
	return Press(ctx, r.s, c.key, c.repeat, c.delay)
}

// Press presses the key [vt.ParseKey] names n times, pausing for delay
// after each press, until ctx is done.
func Press(ctx context.Context, s *reel.Session, key string, n int, delay time.Duration) error {
	return repeat(ctx, n, delay, func() error { return s.Key(key) })
}

// scrollStep scrolls the viewport a row at a time, as VHS does.
type scrollStep struct {
	up     bool
	delay  time.Duration
	repeat int
}

func (c scrollStep) run(ctx context.Context, r *runner) error {
	scroll := r.s.ScrollDown
	if c.up {
		scroll = r.s.ScrollUp
	}
	return repeat(ctx, c.repeat, c.delay, func() error { return scroll(1) })
}

type sleepStep struct{ d time.Duration }

func (c sleepStep) run(ctx context.Context, _ *runner) error { return sleep(ctx, c.d) }

// waitStep waits for a regular expression to match the cursor's line, or
// with screen, the screen, each line without its trailing spaces, as VHS
// matches.
type waitStep struct {
	screen  bool
	timeout time.Duration
	re      *regexp.Regexp
}

func (c waitStep) run(ctx context.Context, r *runner) error {
	var cond reel.Condition = trimmedMatch{c.re}
	if !c.screen {
		cond = reel.OnCursorLine(cond)
	}
	wctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := r.s.Wait(wctx, cond)
	var werr *reel.WaitError
	if errors.As(err, &werr) && ctx.Err() == nil && errors.Is(werr.Err, context.DeadlineExceeded) {
		what := cond.String()
		if c.screen {
			what += " on the screen"
		}
		return fmt.Errorf("wait for %s timed out after %v; the screen was:\n%s\n%w", what, c.timeout, TrimScreen(werr.Last), err)
	}
	return err
}

// trimmedMatch is a [reel.Condition] that holds when re matches the text,
// each line without its trailing spaces.
type trimmedMatch struct{ re *regexp.Regexp }

func (m trimmedMatch) Check(screen reel.ScreenText) ([]reel.Match, bool) {
	return nil, m.re.MatchString(trimLines(screen.Text))
}

func (m trimmedMatch) String() string { return fmt.Sprintf("/%s/", m.re) }

// trimLines removes the spaces at the end of each line of text.
func trimLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// TrimScreen is a screen's text without the spaces at the end of each line,
// or blank lines at its end.
func TrimScreen(text string) string {
	return strings.TrimRight(trimLines(text), "\n")
}

type markStep struct{ label string }

func (c markStep) run(_ context.Context, r *runner) error { return r.s.Mark(c.label) }

type copyStep struct{ text string }

func (c copyStep) run(_ context.Context, r *runner) error {
	r.clipboard = c.text
	return nil
}

type pasteStep struct{}

func (pasteStep) run(_ context.Context, r *runner) error {
	if r.clipboard == "" {
		return nil
	}
	return r.s.Paste(r.clipboard)
}
