// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tape

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Command is a command of a tape, as written: Wait+Screen@5s /ready/ is
// the command Wait, with the modifier Screen, the value 5s, and the
// argument ready.
type Command struct {
	Name Token
	// Mods are the tokens after "+" signs, as in Ctrl+Alt+c or Wait+Screen.
	Mods []Token
	// At is the token after "@", as in Type@50ms, or nil.
	At *Token
	// Args are the tokens up to the next command.
	Args []Token
}

// Option configures [Parse] and [ParseFile].
type Option func(*config)

type config struct {
	commands Commands
	report   func(Command, Action)
}

// WithCommands parses with cmds, rather than [VHSCommands].
func WithCommands(cmds Commands) Option {
	return func(cfg *config) { cfg.commands = cmds }
}

// WithReport calls fn with each command that is skipped or fails, in the
// order of the tape, such as to log them.
func WithReport(fn func(cmd Command, action Action)) Option {
	return func(cfg *config) { cfg.report = fn }
}

// ParseFile parses the tape in the file name. See [Parse].
func ParseFile(name string, opts ...Option) ([]Command, error) {
	src, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return parse(name, string(src), opts)
}

// Parse parses a tape into its commands, giving their positions in the
// file name, which may be empty. Every error is an [*Error], joined with
// [errors.Join] when there are several.
//
// A command starts at a word that names one of [VHSCommands], or of
// [WithCommands], and its arguments run to the next. Parse checks only
// that, and drops or fails the commands whose [Action] says to: it is for
// the caller to give the commands their meaning.
func Parse(name string, r io.Reader, opts ...Option) ([]Command, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parse(name, string(src), opts)
}

func parse(name, src string, opts []Option) ([]Command, error) {
	p := &parser{}
	for _, opt := range opts {
		if opt != nil {
			opt(&p.cfg)
		}
	}
	if p.cfg.commands == nil {
		p.cfg.commands = VHSCommands()
	}
	p.toks, p.errs = lex(name, src)

	var cmds []Command
	for p.peek().Kind != eof {
		tok := p.next()
		if !p.isCommand(tok) {
			if tok.Kind == Word {
				p.errorf(tok.Pos, "unknown command %q", tok.Text)
			} else {
				p.errorf(tok.Pos, "expected a command, got %q", tok.Text)
			}
			// Resume at the next command.
			for p.peek().Kind != eof && !p.isCommand(p.peek()) {
				p.next()
			}
			continue
		}
		cmd := p.command(tok)
		action := p.cfg.commands[tok.Text]
		if action == Keep {
			cmds = append(cmds, cmd)
			continue
		}
		if action == Fail {
			p.errs = append(p.errs, &Error{Pos: tok.Pos, Err: fmt.Errorf("%s: %w", tok.Text, ErrDisabled)})
		}
		if p.cfg.report != nil {
			p.cfg.report(cmd, action)
		}
	}
	if len(p.errs) > 0 {
		return nil, errors.Join(p.errs...)
	}
	return cmds, nil
}

type parser struct {
	cfg  config
	toks []Token
	errs []error
}

func (p *parser) peek() Token { return p.toks[0] }

func (p *parser) next() Token {
	t := p.toks[0]
	if t.Kind != eof {
		p.toks = p.toks[1:]
	}
	return t
}

func (p *parser) peekPunct(s string) bool { return p.peek().Kind == Punct && p.peek().Text == s }

func (p *parser) isCommand(t Token) bool {
	_, ok := p.cfg.commands[t.Text]
	return t.Kind == Word && ok
}

func (p *parser) errorf(pos Pos, format string, args ...any) {
	p.errs = append(p.errs, &Error{Pos: pos, Err: fmt.Errorf(format, args...)})
}

// command reads the rest of the command named by name.
func (p *parser) command(name Token) Command {
	cmd := Command{Name: name}
	for p.peekPunct("+") {
		if t, ok := p.after(name); ok {
			cmd.Mods = append(cmd.Mods, t)
		}
	}
	if p.peekPunct("@") {
		if t, ok := p.after(name); ok {
			cmd.At = &t
		}
	}
	for p.peek().Kind != eof && !p.isCommand(p.peek()) {
		cmd.Args = append(cmd.Args, p.next())
	}
	return cmd
}

// after reads the token after a "+" or "@" of the command named by name,
// whatever it is.
func (p *parser) after(name Token) (Token, bool) {
	punct := p.next()
	if p.peek().Kind == eof {
		p.errorf(punct.Pos, "%s%s expects a value", name.Text, punct.Text)
		return Token{}, false
	}
	return p.next(), true
}

// IsString reports whether t is a string: quoted, or a bare word.
func (t Token) IsString() bool { return t.Kind == String || t.Kind == Word }

// Int returns t as a whole number.
func (t Token) Int() (int, error) {
	n, err := strconv.Atoi(t.Text)
	if t.Kind != Number || err != nil {
		return 0, fmt.Errorf("%q is not a whole number", t.Text)
	}
	return n, nil
}

// Float returns t as a number.
func (t Token) Float() (float64, error) {
	f, err := strconv.ParseFloat(t.Text, 64)
	if t.Kind != Number || err != nil {
		return 0, fmt.Errorf("%q is not a number", t.Text)
	}
	return f, nil
}

// Bool returns t as true or false.
func (t Token) Bool() (bool, error) {
	if t.Kind != Word || t.Text != "true" && t.Text != "false" {
		return false, fmt.Errorf("%q is not true or false", t.Text)
	}
	return t.Text == "true", nil
}

// Duration returns t as a number with a unit of ms, s, or m; a bare
// number is seconds, as in VHS.
func (t Token) Duration() (time.Duration, error) {
	n := strings.TrimRight(t.Text, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ%")
	unit := t.Text[len(n):]
	if unit == "" {
		unit = "s"
	}
	d, err := time.ParseDuration(n + unit)
	if t.Kind != Number || err != nil || unit != "ms" && unit != "s" && unit != "m" {
		return 0, fmt.Errorf("%q is not a duration", t.Text)
	}
	return d, nil
}
