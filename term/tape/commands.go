// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package tape

import (
	"errors"
	"strconv"
	"strings"
)

// ErrDisabled is the error of a command whose [Action] is [Fail].
var ErrDisabled = errors.New("command is disabled")

// Action is what [Parse] does with a command.
type Action int

const (
	// Keep returns the command.
	Keep Action = iota
	// Skip drops the command and its arguments, as if they were not in the
	// tape.
	Skip
	// Fail makes the command an error, wrapping [ErrDisabled].
	Fail
)

func (a Action) String() string {
	switch a {
	case Keep:
		return "keep"
	case Skip:
		return "skip"
	case Fail:
		return "fail"
	}
	return "Action(" + strconv.Itoa(int(a)) + ")"
}

// Commands are the commands a tape may use, by name, and what [Parse] does
// with each. A word that is not one of them is never a command, so it is
// an argument of the command before it.
type Commands map[string]Action

// VHSCommands returns VHS's commands, each kept, as a new map the caller
// may change: to add commands of its own, or to skip or fail VHS's.
func VHSCommands() Commands {
	cmds := Commands{}
	for name := range strings.FieldsSeq(`Output Require Source Set Env Type
		Sleep Enter Space Backspace Delete Insert Tab Escape Up Down Left Right
		PageUp PageDown End Ctrl Alt Shift ScrollUp ScrollDown Hide Show Wait
		Screenshot Copy Paste`) {
		cmds[name] = Keep
	}
	return cmds
}
