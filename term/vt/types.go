// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"image/color"

	"github.com/lrstanley/x/term/vt/screen"
)

// Cursor is the caret at the last grid read.
type Cursor struct {
	// X is the column, starting at 0.
	X int
	// Y is the viewport row, starting at 0.
	Y int
	// Visible is false when the caret is hidden by mode 25.
	Visible bool
	// Blink is true when the caret is set to blink. The host draws the frames.
	Blink bool
	// Shape is the visual style from the render state.
	Shape screen.CursorShape
	// Color is the cursor color. Nil means the terminal default.
	Color color.Color
}

// Snapshot is the host metadata around the cell grid, read together.
// Generation is the content generation [Terminal.Screen] would copy.
// Zero means there is no grid. The grid itself is not copied.
type Snapshot struct {
	// Title is the last OSC 0 / OSC 2 title.
	Title string
	// AltScreen reports whether the alternate screen is active.
	AltScreen bool
	// Cursor is the caret from the latest grid read.
	Cursor Cursor
	// Foreground is the override color, or the default when the override is clear.
	Foreground color.NRGBA
	// Background is the override color, or the default when the override is clear.
	Background color.NRGBA
	// CursorColor is the cursor color [Terminal.CursorColor] would return.
	// CursorColorSet is false when neither the host nor the program has set one.
	CursorColor    color.NRGBA
	CursorColorSet bool
	// ScrollbackLen is the number of scrollback rows. It is zero on the
	// alternate screen.
	ScrollbackLen int
	// Generation is the viewport content generation. It does not copy cells.
	Generation uint64
}

// Progress is the latest OSC 9;4 report. The zero value means the program
// has not reported one.
type Progress struct {
	// Reported is false until the program sends OSC 9;4.
	Reported bool
	// State is the named OSC 9;4 state.
	State ProgressState
	// Percent is 0 through 100. Negative means the report had no percent.
	Percent int
}

// ProgressState is the OSC 9;4 state name.
type ProgressState string

// Progress states reported by OSC 9;4. Names match
// GhosttyTerminalProgressState in include/ghostty/vt/terminal.h.
const (
	// ProgressRemove clears any visible progress indication.
	ProgressRemove ProgressState = "remove"
	// ProgressSet shows determinate progress. Percent is 0 through 100.
	ProgressSet ProgressState = "set"
	// ProgressError shows a failed progress state.
	ProgressError ProgressState = "error"
	// ProgressIndeterminate shows progress with no known percent.
	ProgressIndeterminate ProgressState = "indeterminate"
	// ProgressPause shows paused progress.
	ProgressPause ProgressState = "pause"
)

// Notification is one desktop notification from OSC 9 or OSC 777.
type Notification struct {
	// Title is the notification title. It may be empty.
	Title string
	// Body is the notification body. It may be empty.
	Body string
}

// Prompt is the latest OSC 133 step.
type Prompt struct {
	// Kind is the latest OSC 133 step. PromptNone means none has arrived.
	Kind PromptKind
	// HasExitCode is true when the end step included an exit status.
	HasExitCode bool
	// ExitCode is the process status from that step. It is meaningful
	// only when HasExitCode is true.
	ExitCode int
	// Command is the command line from the prompt, when the shell sent one.
	Command string
}

// PromptKind is one step of a semantic prompt.
type PromptKind string

// Semantic prompt steps from OSC 133. Names match
// GhosttySemanticPromptKind in include/ghostty/vt/terminal.h.
// PromptNone is the zero value and is never reported.
const (
	// PromptNone means no prompt event has arrived. A zero value is not
	// a real step.
	PromptNone PromptKind = ""
	// PromptStart means the shell started drawing a prompt.
	PromptStart PromptKind = "start"
	// PromptInput means the prompt is drawn and the user can type.
	PromptInput PromptKind = "input"
	// PromptOutput means the command started. Bytes after this are its output.
	PromptOutput PromptKind = "output"
	// PromptCommand means the command finished. ExitCode is set when the
	// shell included one.
	PromptCommand PromptKind = "end"
)

// ProgramState is an OSC 7501 state.
type ProgramState string

// Program status states from OSC 7501. Names match
// GhosttyProgramStatusState in include/ghostty/vt/terminal.h.
// The protocol describes state only. How to show it is up to the host.
const (
	// ProgramIdle means the program is at rest, waiting for the next
	// instruction. An interactive tool sitting at its own prompt is idle.
	ProgramIdle ProgramState = "idle"
	// ProgramWorking means the program is running on its own. The report
	// may include a progress percentage.
	ProgramWorking ProgramState = "working"
	// ProgramDone means a piece of work finished and the result is ready
	// for the user to look at.
	ProgramDone ProgramState = "done"
	// ProgramBlocked means the program cannot continue until the user
	// does something. Kind says what it needs and Message says why.
	ProgramBlocked ProgramState = "blocked"
	// ProgramError means the program failed and stopped.
	ProgramError ProgramState = "error"
	// ProgramClear removes the record with this id and every record
	// beneath it. An empty id removes every record. It is not a state
	// the program stays in.
	ProgramClear ProgramState = "clear"
)

// ProgramKind is what a blocked program is waiting for.
type ProgramKind string

// Program status kinds from OSC 7501. Names match
// GhosttyProgramStatusKind in include/ghostty/vt/terminal.h.
// Kind is only set while the state is [ProgramBlocked].
const (
	// ProgramKindNone means the program did not say, or the state is
	// not [ProgramBlocked].
	ProgramKindNone ProgramKind = ""
	// ProgramKindPermission means the program wants approval to do
	// something, such as applying a change.
	ProgramKindPermission ProgramKind = "permission"
	// ProgramKindQuestion means the program is waiting for an answer
	// the user has to type.
	ProgramKindQuestion ProgramKind = "question"
	// ProgramKindAuth means the program is waiting for a login, password,
	// token, or other credential.
	ProgramKindAuth ProgramKind = "auth"
)

// ProgramStatus is one OSC 7501 record.
type ProgramStatus struct {
	// ID is the OSC 7501 record id. Nested ids use slashes.
	ID string
	// State is the record's state. ProgramClear removes it.
	State ProgramState
	// Kind is why a blocked program is waiting. Empty for other states.
	Kind ProgramKind
	// App is the application name. An empty App is filled from the
	// nearest ancestor that set one when ProgramStatuses is read.
	App string
	// Title is the report title.
	Title string
	// Message is the report body.
	Message string
	// Progress is 0 through 100. HasProgress is false when the report
	// did not include one.
	Progress    int
	HasProgress bool
}

// Key is a key press encoded for the program's stdin.
type Key struct {
	// Code is a Ghostty key name such as "ENTER" or "A". Empty means the
	// encoder should use Text alone.
	Code string
	// Text is the text the key produces, such as the typed character.
	// It is encoded as UTF-8 for the key event.
	Text string
	// Mods is the modifier bitmask held with the key.
	Mods Mods
}

// Mods is a modifier bitmask matching the terminal's key encoder.
type Mods uint16

// Modifier bits.
const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// MouseAction is a mouse event the encoder can emit.
type MouseAction int32

// Mouse actions.
const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMotion
)

// MouseButton is a button index the encoder understands.
type MouseButton int32

// Mouse buttons.
const (
	MouseUnknown MouseButton = iota
	MouseLeft
	MouseRight
	MouseMiddle
)

// Mode identifies a terminal mode. ANSI is true for modes such as linefeed
// (20) and false for DEC private modes such as the alternate screen (1047).
type Mode struct {
	// Number is the mode number, such as 20 or 1047.
	Number int
	// ANSI is true for standard modes and false for DEC private modes.
	// The high bit of GhosttyTerminalModeConfig stores this distinction.
	ANSI bool
}
