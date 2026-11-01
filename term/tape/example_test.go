// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tape_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lrstanley/x/term/tape"
)

func ExampleParse() {
	cmds, err := tape.Parse("demo.tape", strings.NewReader(`
Output demo.gif
Set FontSize 22
Type@50ms "echo hello" Enter
Ctrl+C
Wait+Screen@5s /hello/
`))
	if err != nil {
		panic(err)
	}
	for _, cmd := range cmds {
		fmt.Printf("%s: %s mods=%d at=%v args=%d\n", cmd.Name.Pos, cmd.Name.Text, len(cmd.Mods), cmd.At != nil, len(cmd.Args))
	}
	// Output:
	// demo.tape:2:1: Output mods=0 at=false args=1
	// demo.tape:3:1: Set mods=0 at=false args=2
	// demo.tape:4:1: Type mods=0 at=true args=1
	// demo.tape:4:24: Enter mods=0 at=false args=0
	// demo.tape:5:1: Ctrl mods=1 at=false args=0
	// demo.tape:6:1: Wait mods=1 at=true args=1
}

// Commands of a caller's own are added to VHS's, and VHS's skipped or
// failed, with each that is reported to the caller.
func ExampleWithCommands() {
	cmds := tape.VHSCommands()
	cmds["Mark"] = tape.Keep       // a command of the caller's own
	cmds["Screenshot"] = tape.Skip // ignored
	cmds["Env"] = tape.Fail        // an error
	report := func(cmd tape.Command, action tape.Action) {
		fmt.Printf("%s: %s %s\n", cmd.Name.Pos, action, cmd.Name.Text)
	}

	parsed, err := tape.Parse("", strings.NewReader(`
Type "make"
Mark built
Screenshot built.png
Env SECRET 1
`), tape.WithCommands(cmds), tape.WithReport(report))
	fmt.Println(len(parsed), errors.Is(err, tape.ErrDisabled))
	fmt.Println(err)
	// Output:
	// 4:1: skip Screenshot
	// 5:1: fail Env
	// 0 true
	// tape: 5:1: Env: command is disabled
}

// player gives commands their meaning: here it prints what it would do,
// where a runner that drives a terminal would type and press keys in it.
type player struct {
	out         io.Writer
	typingSpeed time.Duration
}

// play runs a command, with a handler per command name.
func (p *player) play(cmd tape.Command) error {
	handlers := map[string]func(tape.Command) error{
		"Set":   p.set,
		"Type":  p.typeText,
		"Enter": p.press("enter"),
		"Sleep": p.sleep,
	}
	h, ok := handlers[cmd.Name.Text]
	if !ok {
		return &tape.Error{Pos: cmd.Name.Pos, Err: fmt.Errorf("%s is not supported", cmd.Name.Text)}
	}
	return h(cmd)
}

func (p *player) set(cmd tape.Command) error {
	if len(cmd.Args) != 2 || cmd.Args[0].Text != "TypingSpeed" {
		return &tape.Error{Pos: cmd.Name.Pos, Err: errors.New("only Set TypingSpeed is supported")}
	}
	d, err := cmd.Args[1].Duration()
	if err != nil {
		return &tape.Error{Pos: cmd.Args[1].Pos, Err: err}
	}
	p.typingSpeed = d
	return nil
}

func (p *player) typeText(cmd tape.Command) error {
	speed := p.typingSpeed
	if cmd.At != nil {
		d, err := cmd.At.Duration()
		if err != nil {
			return &tape.Error{Pos: cmd.At.Pos, Err: err}
		}
		speed = d
	}
	var words []string
	for _, arg := range cmd.Args {
		if !arg.IsString() {
			return &tape.Error{Pos: arg.Pos, Err: fmt.Errorf("%q is not a string", arg.Text)}
		}
		words = append(words, arg.Text)
	}
	fmt.Fprintf(p.out, "type %q, %v a key\n", strings.Join(words, " "), speed)
	return nil
}

func (p *player) press(key string) func(tape.Command) error {
	return func(cmd tape.Command) error {
		n := 1
		if len(cmd.Args) == 1 {
			var err error
			if n, err = cmd.Args[0].Int(); err != nil {
				return &tape.Error{Pos: cmd.Args[0].Pos, Err: err}
			}
		}
		fmt.Fprintf(p.out, "press %s %d times\n", key, n)
		return nil
	}
}

func (p *player) sleep(cmd tape.Command) error {
	if len(cmd.Args) != 1 {
		return &tape.Error{Pos: cmd.Name.Pos, Err: errors.New("Sleep expects a duration")}
	}
	d, err := cmd.Args[0].Duration()
	if err != nil {
		return &tape.Error{Pos: cmd.Args[0].Pos, Err: err}
	}
	fmt.Fprintf(p.out, "sleep %v\n", d)
	return nil
}

// A program that runs tapes, such as one that records a terminal, parses
// a tape and gives each command its meaning.
func Example_player() {
	cmds, err := tape.Parse("", strings.NewReader(`
Set TypingSpeed 20ms
Type "echo" 'hi'
Enter 2
Type@100ms "exit"
Sleep .5
`))
	if err != nil {
		panic(err)
	}
	p := &player{out: os.Stdout, typingSpeed: 50 * time.Millisecond}
	for _, cmd := range cmds {
		if err = p.play(cmd); err != nil {
			fmt.Println(err)
		}
	}
	// Output:
	// type "echo hi", 20ms a key
	// press enter 2 times
	// type "exit", 100ms a key
	// sleep 500ms
}
