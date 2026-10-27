// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

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
