// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/spec"
	"github.com/lrstanley/x/term/reel/vt"
	syntax "github.com/lrstanley/x/term/tape"
)

// The marks a run appends for Hide and Show, and at its end. A Hide that
// no Show follows marks hideRestMark instead of hideMark.
const (
	hideMark     = "tape:hide"
	showMark     = "tape:show"
	hideRestMark = "tape:hide-rest"
	endMark      = "tape:end"
)

// Tape is a compiled tape, ready to [Tape.Run].
type Tape struct {
	settings settings
	steps    []located
	outputs  []reel.File
	// shots are the Screenshot outputs, by directory, each one still per
	// mark.
	shots []reel.File
	// requires are the Require programs.
	requires []syntax.Token
	// env is the Env variables, as "KEY=value".
	env []string
	// host is the first command that configures the shell, which
	// [WithProgram] rejects: Set Shell, Env, or Require.
	host syntax.Token
	// effects are those of the tape's Highlight, Zoom, and Redact commands.
	effects []reel.Effect
	// hide is the position of the first Hide, if any; leadingHide, spans,
	// and hideRest are whether the tape hides its start, spans between
	// Hide and Show, and its end (see [effects.HideBetween]).
	hide                         *syntax.Pos
	leadingHide, spans, hideRest bool
	warnings                     []string
}

// located is a step and the position of its command.
type located struct {
	pos  syntax.Pos
	step step
}

// CompileOption configures [Compile], [Parse], and [ParseFile].
type CompileOption func(*compiler)

// WithSourceRoot reads the tapes Source commands name from fsys, instead of
// from the file system relative to the working directory, as VHS does.
func WithSourceRoot(fsys fs.FS) CompileOption {
	return func(c *compiler) { c.root = fsys }
}

// Step is what a command of [WithCommand] does when the tape runs, to the
// run's session.
type Step func(ctx context.Context, s *reel.Session) error

func (s Step) run(ctx context.Context, r *runner) error { return s(ctx, r.s) }

// WithCommand adds the command name, or replaces the built-in one of that
// name. [Compile] calls compile for each use of the command, with its
// modifiers, value, and arguments, and the tape runs the step it returns
// in the command's place. [Parse] and [ParseFile] read name as a command.
func WithCommand(name string, compile func(cmd syntax.Command) (Step, error)) CompileOption {
	return func(c *compiler) {
		c.commands[name] = command{mods: true, at: true, compile: func(c *compiler, cmd syntax.Command) error {
			s, err := compile(cmd)
			if err != nil {
				return err
			}
			c.args = nil
			c.add(cmd.Name.Pos, s)
			return nil
		}}
	}
}

// WithCommands edits the commands of the tapes that [Parse], [ParseFile],
// and Source read. edit is given VHS's, Mark, Home, and those of
// [WithCommand], each kept, and may [syntax.Skip] or [syntax.Fail] any,
// such as to ignore Screenshot or forbid Env, or delete them.
func WithCommands(edit func(cmds syntax.Commands)) CompileOption {
	return func(c *compiler) { c.edits = append(c.edits, edit) }
}

// WithReport calls fn with each command of the tapes that [Parse],
// [ParseFile], and Source read that is skipped or fails, such as to log
// them.
func WithReport(fn func(cmd syntax.Command, action syntax.Action)) CompileOption {
	return func(c *compiler) { c.report = fn }
}

// ParseFile parses and compiles the tape in the file name, whose name its
// errors and warnings are given with. See [Parse].
func ParseFile(name string, opts ...CompileOption) (*Tape, error) {
	c := newCompiler(opts)
	cmds, err := syntax.ParseFile(name, c.parseOptions()...)
	if err != nil {
		return nil, err
	}
	return c.compile(cmds)
}

// Parse parses a tape with [syntax.Parse], reading Mark, Home, and the
// names of [WithCommand] as commands too, as [WithCommands] edits them,
// and compiles it. See [Compile].
func Parse(r io.Reader, opts ...CompileOption) (*Tape, error) {
	c := newCompiler(opts)
	cmds, err := syntax.Parse("", r, c.parseOptions()...)
	if err != nil {
		return nil, err
	}
	return c.compile(cmds)
}

// Compile compiles a parsed tape into what [Tape.Run] runs. Every error is
// a [*syntax.Error], joined with [errors.Join] when there are several.
//
// See the package documentation for the commands and settings it reads.
func Compile(cmds []syntax.Command, opts ...CompileOption) (*Tape, error) {
	return newCompiler(opts).compile(cmds)
}

func newCompiler(opts []CompileOption) *compiler {
	c := &compiler{tape: &Tape{settings: newSettings()}, commands: builtins(), shotNames: map[string]syntax.Pos{}}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

type compiler struct {
	root     fs.FS
	commands map[string]command
	edits    []func(syntax.Commands)
	report   func(syntax.Command, syntax.Action)
	tape     *Tape
	errs     []error
	// cmd is the command being compiled, and args its arguments not yet
	// read.
	cmd  syntax.Command
	args []syntax.Token
	// nested is set while compiling a Source tape.
	nested bool
	// started is set by the first command that is not Set, Output, or
	// Require, after which Set is ignored but for TypingSpeed, as in VHS.
	started bool
	// hidden is set from a Hide to the next Show, and open is the step of
	// the Hide that hid it last, or -1 for one before any step.
	hidden    bool
	open      int
	shotNames map[string]syntax.Pos
	marks     []syntax.Token
}

// command is how a command compiles.
type command struct {
	// mods and at are whether the command takes +modifiers and an @value.
	mods, at bool
	// setup is whether the command may come before the settings end (see
	// compiler.started).
	setup   bool
	compile func(*compiler, syntax.Command) error
}

func (c *compiler) compile(cmds []syntax.Command) (*Tape, error) {
	c.compileAll(cmds)
	c.finish()
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return c.tape, nil
}

// parseOptions are the [syntax.Parse] options for c's commands, edited
// with [WithCommands], and its [WithReport].
func (c *compiler) parseOptions() []syntax.Option {
	cmds := syntax.VHSCommands()
	for name := range c.commands {
		cmds[name] = syntax.Keep
	}
	for _, edit := range c.edits {
		edit(cmds)
	}
	return []syntax.Option{syntax.WithCommands(cmds), syntax.WithReport(c.report)}
}

func (c *compiler) errorf(pos syntax.Pos, format string, args ...any) error {
	return &syntax.Error{Pos: pos, Err: fmt.Errorf(format, args...)}
}

// at returns err as a [*syntax.Error] at pos, unless it is one.
func at(pos syntax.Pos, err error) error {
	var e *syntax.Error
	if err == nil || errors.As(err, &e) {
		return err
	}
	return &syntax.Error{Pos: pos, Err: err}
}

func (c *compiler) warnf(pos syntax.Pos, format string, args ...any) {
	c.tape.warnings = append(c.tape.warnings, pos.String()+": "+fmt.Sprintf(format, args...))
}

// take reads the next argument of the command.
func (c *compiler) take() (syntax.Token, bool) {
	if len(c.args) == 0 {
		return syntax.Token{}, false
	}
	t := c.args[0]
	c.args = c.args[1:]
	return t, true
}

// peek returns the next argument, or the zero token.
func (c *compiler) peek() syntax.Token {
	if len(c.args) == 0 {
		return syntax.Token{}
	}
	return c.args[0]
}

func (c *compiler) compileAll(cmds []syntax.Command) {
	for _, cmd := range cmds {
		if err := c.command(cmd); err != nil {
			c.errs = append(c.errs, at(cmd.Name.Pos, err))
		}
	}
}

func (c *compiler) command(cmd syntax.Command) error {
	name := cmd.Name.Text
	h, ok := c.commands[name]
	switch {
	case !ok:
		return fmt.Errorf("unknown command %q", name)
	case len(cmd.Mods) > 0 && !h.mods:
		return c.errorf(cmd.Mods[0].Pos, "%s takes no +%s", name, cmd.Mods[0].Text)
	case cmd.At != nil && !h.at:
		return c.errorf(cmd.At.Pos, "%s takes no @%s", name, cmd.At.Text)
	}
	c.started = c.started || !h.setup
	c.cmd, c.args = cmd, cmd.Args
	if err := h.compile(c, cmd); err != nil {
		return err
	}
	if t, extra := c.take(); extra {
		return c.errorf(t.Pos, "unexpected %q after %s", t.Text, name)
	}
	return nil
}

func (c *compiler) hostCommand(tok syntax.Token) {
	if c.tape.host.Text == "" {
		c.tape.host = tok
	}
}

func (c *compiler) add(pos syntax.Pos, s step) {
	c.tape.steps = append(c.tape.steps, located{pos: pos, step: s})
}

// keyNames are the key commands, by the [vt.ParseKey] names they press.
// Home is not a VHS command, though End is.
var keyNames = map[string]string{
	"Enter": "enter", "Backspace": "backspace", "Delete": "delete",
	"Insert": "insert", "Tab": "tab", "Escape": "esc", "Space": "space",
	"Up": "up", "Down": "down", "Left": "left", "Right": "right",
	"PageUp": "pgup", "PageDown": "pgdown", "Home": "home", "End": "end",
}

// builtins are the commands of a tape: VHS's, and reel's Mark, Home,
// Highlight, Zoom, and Redact.
func builtins() map[string]command {
	cmds := map[string]command{
		"Output":     {setup: true, compile: (*compiler).output},
		"Require":    {setup: true, compile: (*compiler).require},
		"Source":     {setup: true, compile: (*compiler).source},
		"Set":        {setup: true, compile: (*compiler).setCommand},
		"Env":        {compile: (*compiler).env},
		"Type":       {at: true, compile: (*compiler).typeText},
		"Sleep":      {compile: (*compiler).sleep},
		"ScrollUp":   {at: true, compile: (*compiler).scroll},
		"ScrollDown": {at: true, compile: (*compiler).scroll},
		"Ctrl":       {mods: true, compile: (*compiler).ctrl},
		"Alt":        {mods: true, compile: (*compiler).modified},
		"Shift":      {mods: true, compile: (*compiler).modified},
		"Hide":       {compile: (*compiler).hideShow},
		"Show":       {compile: (*compiler).hideShow},
		"Wait":       {mods: true, at: true, compile: (*compiler).wait},
		"Screenshot": {compile: (*compiler).screenshot},
		"Copy":       {compile: (*compiler).copyText},
		"Paste":      {compile: (*compiler).paste},
		"Mark":       {compile: (*compiler).mark},
		"Highlight":  {setup: true, compile: (*compiler).effect},
		"Zoom":       {setup: true, compile: (*compiler).effect},
		"Redact":     {setup: true, compile: (*compiler).effect},
	}
	for name := range keyNames {
		cmds[name] = command{at: true, compile: (*compiler).key}
	}
	return cmds
}

func (c *compiler) output(cmd syntax.Command) error {
	raw, err := c.stringArg(cmd.Name)
	if err != nil || c.nested {
		// VHS drops a Source tape's outputs.
		return err
	}
	f, err := outputFile(raw)
	if err != nil {
		return err
	}
	c.tape.outputs = append(c.tape.outputs, f)
	return nil
}

func (c *compiler) require(cmd syntax.Command) error {
	name, err := c.stringArg(cmd.Name)
	if err == nil {
		c.tape.requires = append(c.tape.requires, syntax.Token{Text: name, Pos: cmd.Name.Pos})
		c.hostCommand(cmd.Name)
	}
	return err
}

func (c *compiler) env(cmd syntax.Command) error {
	key, err := c.stringArg(cmd.Name)
	if err != nil {
		return err
	}
	value, err := c.stringArg(cmd.Name)
	if err != nil {
		return err
	}
	c.tape.env = append(c.tape.env, key+"="+value)
	c.hostCommand(cmd.Name)
	return nil
}

func (c *compiler) typeText(cmd syntax.Command) error {
	delay, err := c.speed()
	if err != nil {
		return err
	}
	text, err := c.strings(cmd.Name)
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, typeStep{text: text, delay: delay})
	return nil
}

func (c *compiler) sleep(cmd syntax.Command) error {
	d, err := c.duration(cmd.Name)
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, sleepStep{d: d})
	return nil
}

func (c *compiler) scroll(cmd syntax.Command) error {
	delay, n, err := c.speedRepeat()
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, scrollStep{up: cmd.Name.Text == "ScrollUp", delay: delay, repeat: n})
	return nil
}

func (c *compiler) key(cmd syntax.Command) error {
	delay, n, err := c.speedRepeat()
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, keyStep{key: keyNames[cmd.Name.Text], delay: delay, repeat: n})
	return nil
}

func (c *compiler) copyText(cmd syntax.Command) error {
	text, err := c.strings(cmd.Name)
	if err != nil {
		return err
	}
	c.add(cmd.Name.Pos, copyStep{text: text})
	return nil
}

func (c *compiler) paste(cmd syntax.Command) error {
	c.add(cmd.Name.Pos, pasteStep{})
	return nil
}

func (c *compiler) mark(cmd syntax.Command) error {
	label, err := c.stringArg(cmd.Name)
	if err != nil {
		return err
	}
	if label == "" || strings.HasPrefix(label, "tape:") {
		return fmt.Errorf("mark label %q is empty or starts with \"tape:\"", label)
	}
	c.marks = append(c.marks, syntax.Token{Text: label, Pos: cmd.Name.Pos})
	c.add(cmd.Name.Pos, markStep{label: label})
	return nil
}

// effect compiles Highlight, Zoom, or Redact "<spec>" (see package spec),
// which apply to the whole recording, or the window of the spec's
// between=, wherever they are in the tape.
func (c *compiler) effect(cmd syntax.Command) error {
	e, _, err := arg(c, cmd.Name, "a spec", func(t syntax.Token) (reel.Effect, error) {
		if !t.IsString() {
			return nil, fmt.Errorf("%q is not a string", t.Text)
		}
		return spec.Effect(strings.ToLower(cmd.Name.Text), t.Text)
	})
	if err != nil {
		return err
	}
	c.tape.effects = append(c.tape.effects, e)
	return nil
}

// hideShow marks a Hide or Show that changes whether the tape is hidden.
// A Hide before any step marks nothing, as the recording starts hidden.
func (c *compiler) hideShow(cmd syntax.Command) error {
	pos := cmd.Name.Pos
	switch {
	case cmd.Name.Text == "Hide" && !c.hidden:
		c.hidden = true
		if c.tape.hide == nil {
			c.tape.hide = &pos
		}
		c.open = -1
		if len(c.tape.steps) > 0 {
			c.open = len(c.tape.steps)
			c.add(pos, markStep{label: hideMark})
		}
	case cmd.Name.Text == "Show" && c.hidden:
		c.hidden = false
		if c.open < 0 {
			c.tape.leadingHide = true
		} else {
			c.tape.spans = true
		}
		c.add(pos, markStep{label: showMark})
	}
	return nil
}

// finish checks what the whole tape decides once it is parsed.
func (c *compiler) finish() {
	t := c.tape
	if c.hidden {
		if c.open < 0 {
			c.errs = append(c.errs, c.errorf(*t.hide, "Hide is never followed by Show, so nothing would be recorded"))
		} else {
			t.steps[c.open].step = markStep{label: hideRestMark}
			t.hideRest = true
		}
	}
	for _, m := range c.marks {
		if _, ok := c.shotNames[m.Text]; ok {
			c.errs = append(c.errs, c.errorf(m.Pos, "Mark %q is the label of a Screenshot of the same name", m.Text))
		}
	}
}

// setCommand compiles a Set. After the first command, only TypingSpeed
// changes anything, for the commands after it, as in VHS. As the other
// settings are then final, commands take theirs as they are compiled.
func (c *compiler) setCommand(syntax.Command) error {
	name, ok := c.take()
	if !ok || name.Kind != syntax.Word {
		return c.errorf(name.Pos, "Set expects a setting")
	}
	if !c.started {
		warning, err := c.set(&c.tape.settings, name)
		if warning != "" {
			c.warnf(name.Pos, "%s", warning)
		}
		if err == nil && name.Text == "Shell" {
			c.hostCommand(name)
		}
		return at(name.Pos, err)
	}
	if name.Text == "TypingSpeed" {
		_, err := c.set(&c.tape.settings, name)
		return at(name.Pos, err)
	}
	scratch := c.tape.settings
	if _, err := c.set(&scratch, name); err != nil {
		return at(name.Pos, err)
	}
	c.warnf(name.Pos, "Set %s after the first command is ignored, as in VHS; move it to the top", name.Text)
	return nil
}

// source compiles the tape a Source command names in place. As in VHS,
// its Output commands are dropped, and it cannot Source another.
func (c *compiler) source(cmd syntax.Command) error {
	name, err := c.stringArg(cmd.Name)
	switch {
	case err != nil:
		return err
	case c.nested:
		return errors.New("a sourced tape cannot Source another")
	case filepath.Ext(name) != ".tape":
		return fmt.Errorf("can only Source a .tape file, not %q", name)
	}
	// The sourced tape's commands replace c's argument cursor, so the
	// arguments left after Source's must be found now.
	if t, extra := c.take(); extra {
		return c.errorf(t.Pos, "unexpected %q after Source", t.Text)
	}
	var src []byte
	if c.root != nil {
		src, err = fs.ReadFile(c.root, path.Clean(filepath.ToSlash(name)))
	} else {
		src, err = os.ReadFile(name)
	}
	if err != nil {
		return fmt.Errorf("reading the Source tape: %w", err)
	}
	if len(src) == 0 {
		return fmt.Errorf("the Source tape %q is empty", name)
	}
	cmds, err := syntax.Parse(name, bytes.NewReader(src), c.parseOptions()...)
	if err != nil {
		return err
	}
	c.nested = true
	c.compileAll(cmds)
	c.nested = false
	return nil
}

// ctrl compiles Ctrl[+Alt][+Shift]+<key>.
func (c *compiler) ctrl(cmd syntax.Command) error {
	name := []string{"ctrl"}
	key := ""
	for _, t := range cmd.Mods {
		if key != "" {
			return c.errorf(t.Pos, "Ctrl takes one key, after its modifiers")
		}
		switch {
		case t.Kind == syntax.Word && (t.Text == "Alt" || t.Text == "Shift"):
			name = append(name, strings.ToLower(t.Text))
		case t.Kind == syntax.Word && keyNames[t.Text] != "":
			key = keyNames[t.Text]
		case t.Kind == syntax.Punct || t.IsString() && len(t.Text) == 1:
			key = strings.ToLower(t.Text)
		default:
			return c.errorf(t.Pos, "Ctrl: %q is not a key", t.Text)
		}
	}
	if key == "" {
		return errors.New("expected Ctrl+<key>")
	}
	return c.addKey(cmd.Name, strings.Join(append(name, key), "+"))
}

// modified compiles Alt+<key> or Shift+<key>. Alt types each character of
// a longer string with alt held, as VHS does.
func (c *compiler) modified(cmd syntax.Command) error {
	if len(cmd.Mods) != 1 {
		return fmt.Errorf("%s expects +<key>", cmd.Name.Text)
	}
	t := cmd.Mods[0]
	mod := strings.ToLower(cmd.Name.Text)
	switch {
	case t.Kind == syntax.Word && (t.Text == "Enter" || t.Text == "Tab"):
		return c.addKey(cmd.Name, mod+"+"+keyNames[t.Text])
	case t.Text == "[" || t.Text == "]":
	case !t.IsString() || t.Text == "":
		return c.errorf(t.Pos, "%s: %q is not a key", cmd.Name.Text, t.Text)
	case mod == "shift":
		if len([]rune(t.Text)) != 1 {
			return c.errorf(t.Pos, "Shift takes one key, not %q", t.Text)
		}
		return c.addKey(cmd.Name, "shift+"+strings.ToLower(t.Text))
	}
	for _, r := range t.Text {
		if err := c.addKey(cmd.Name, mod+"+"+string(r)); err != nil {
			return err
		}
	}
	return nil
}

// addKey adds a press of the key [vt.ParseKey] names, as VHS presses
// modified keys: with no pause.
func (c *compiler) addKey(tok syntax.Token, name string) error {
	if _, err := vt.ParseKey(name); err != nil {
		return err
	}
	c.add(tok.Pos, keyStep{key: name, repeat: 1})
	return nil
}

// wait compiles Wait[+Line|+Screen][@timeout] [/regexp/|"text"].
func (c *compiler) wait(cmd syntax.Command) error {
	w := waitStep{timeout: c.tape.settings.waitTimeout, re: c.tape.settings.waitPattern}
	for i, scope := range cmd.Mods {
		switch {
		case i > 0:
			return c.errorf(scope.Pos, "Wait takes one of +Line or +Screen")
		case scope.Text == "Screen":
			w.screen = true
		case scope.Text != "Line":
			return c.errorf(scope.Pos, "Wait+ expects Line or Screen")
		}
	}
	if cmd.At != nil {
		d, err := cmd.At.Duration()
		if err != nil {
			return c.errorf(cmd.At.Pos, "Wait@: %w", err)
		}
		if d <= 0 {
			return c.errorf(cmd.At.Pos, "Wait expects a positive timeout")
		}
		w.timeout = d
	}
	switch t := c.peek(); {
	case t.Kind == syntax.Regex:
		c.take()
		re, err := regexp.Compile(t.Text)
		if err != nil {
			return c.errorf(t.Pos, "Wait: %w", err)
		}
		w.re = re
	case t.IsString():
		// reel's: a string, matched as is.
		c.take()
		w.re = regexp.MustCompile(regexp.QuoteMeta(t.Text))
	}
	c.add(cmd.Name.Pos, w)
	return nil
}

// screenshot compiles Screenshot <path>.png. Its mark is labeled with the
// file's name without ".png", which {mark} writes as is, so the name must
// hold none of the characters reel replaces in it.
func (c *compiler) screenshot(cmd syntax.Command) error {
	name, err := c.stringArg(cmd.Name)
	if err != nil {
		return err
	}
	dir, file := filepath.Split(name)
	label, ok := strings.CutSuffix(file, ".png")
	switch {
	case !ok || label == "":
		return fmt.Errorf("screenshot %q is not a .png file", name)
	case strings.Contains(name, "{"):
		return fmt.Errorf("screenshot path %q contains \"{\"", name)
	case strings.ContainsAny(label, `<>:"|?*`):
		return fmt.Errorf(`screenshot file name %q contains one of <>:"|?*`, name)
	}
	if prev, taken := c.shotNames[label]; taken {
		return fmt.Errorf("screenshot file name %q is already taken at %s", file, prev)
	}
	c.shotNames[label] = cmd.Name.Pos
	pattern := filepath.Join(dir, "{mark}.png")
	i := slices.IndexFunc(c.tape.shots, func(f reel.File) bool { return f.Path == pattern })
	if i < 0 {
		c.tape.shots = append(c.tape.shots, reel.File{Path: pattern})
		i = len(c.tape.shots) - 1
	}
	c.tape.shots[i].Marks = append(c.tape.shots[i].Marks, label)
	c.add(cmd.Name.Pos, markStep{label: label})
	return nil
}

// outputFile parses an Output path with [reel.ParseFile], rejecting the
// outputs VHS writes that reel does not.
func outputFile(raw string) (reel.File, error) {
	name, _, _ := strings.Cut(raw, "?")
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case ext == "" || strings.HasSuffix(name, "/"):
		return reel.File{}, fmt.Errorf("unsupported output %q: a directory of frames", raw)
	case ext == ".txt" || ext == ".ascii" || ext == ".test":
		return reel.File{}, fmt.Errorf("unsupported output %q: a .snap or .json output holds the screen's text", raw)
	}
	f, err := reel.ParseFile(raw)
	if err != nil {
		return f, err
	}
	switch {
	case len(f.MarkMatch) > 0:
		// [reel.Record] writes the outputs as the tape runs, before the marks
		// are known.
		return f, fmt.Errorf("unsupported output %q: outputs are written as the tape runs, before the marks match= matches; use marks=", raw)
	case ext == ".png" && len(f.Marks) == 0:
		return f, fmt.Errorf("unsupported output %q: VHS writes a .png output as a directory of frames; use Screenshot", raw)
	}
	return f, nil
}

// arg reads the next argument of the command named cmd with read, which
// reads what.
func arg[T any](c *compiler, cmd syntax.Token, what string, read func(syntax.Token) (T, error)) (T, syntax.Token, error) {
	t, ok := c.take()
	if !ok {
		var zero T
		return zero, t, c.errorf(cmd.Pos, "%s expects %s", cmd.Text, what)
	}
	v, err := read(t)
	if err != nil {
		return v, t, c.errorf(t.Pos, "%s: %w", cmd.Text, err)
	}
	return v, t, nil
}

func (c *compiler) stringArg(cmd syntax.Token) (string, error) {
	v, _, err := arg(c, cmd, "a string", func(t syntax.Token) (string, error) {
		if !t.IsString() {
			return "", fmt.Errorf("%q is not a string", t.Text)
		}
		return t.Text, nil
	})
	return v, err
}

// strings reads one or more strings, joined with spaces as VHS joins them.
func (c *compiler) strings(cmd syntax.Token) (string, error) {
	first, err := c.stringArg(cmd)
	if err != nil {
		return "", err
	}
	parts := []string{first}
	for c.peek().IsString() {
		t, _ := c.take()
		parts = append(parts, t.Text)
	}
	return strings.Join(parts, " "), nil
}

func (c *compiler) intArg(cmd syntax.Token, lowest int, v *int) error {
	n, t, err := arg(c, cmd, "a whole number", syntax.Token.Int)
	if err == nil && n < lowest {
		err = c.errorf(t.Pos, "%s must be at least %d, got %d", cmd.Text, lowest, n)
	}
	*v = n
	return err
}

// floatArg reads a number; zero is an error unless zero is allowed.
func (c *compiler) floatArg(cmd syntax.Token, zero bool, v *float64) error {
	f, t, err := arg(c, cmd, "a number", syntax.Token.Float)
	if err == nil && f == 0 && !zero {
		err = c.errorf(t.Pos, "%s must not be 0", cmd.Text)
	}
	*v = f
	return err
}

func (c *compiler) boolArg(cmd syntax.Token) (bool, error) {
	v, _, err := arg(c, cmd, "true or false", syntax.Token.Bool)
	return v, err
}

func (c *compiler) duration(cmd syntax.Token) (time.Duration, error) {
	v, _, err := arg(c, cmd, "a duration", syntax.Token.Duration)
	return v, err
}

// speed reads the command's optional @<duration>, or returns Set
// TypingSpeed's.
func (c *compiler) speed() (time.Duration, error) {
	if c.cmd.At == nil {
		return c.tape.settings.typingSpeed, nil
	}
	d, err := c.cmd.At.Duration()
	if err != nil {
		return 0, c.errorf(c.cmd.At.Pos, "%s@: %w", c.cmd.Name.Text, err)
	}
	return d, nil
}

// speedRepeat reads a key command's optional @<duration> and count.
func (c *compiler) speedRepeat() (time.Duration, int, error) {
	speed, err := c.speed()
	if err != nil {
		return 0, 0, err
	}
	n := 1
	if c.peek().Kind == syntax.Number {
		err = c.intArg(syntax.Token{Text: "a repeat count"}, 1, &n)
	}
	return speed, n, err
}
