// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

// Progress is the latest OSC 9;4 report. The zero value means the program
// has not reported one.
type Progress struct {
	// Reported is false until the program sends OSC 9;4.
	Reported bool
	// State is the reported state.
	State ProgressState
	// Percent is 0 through 100. Negative means the report had no percent.
	Percent int
}

// unknown is what String returns for a value this package does not know.
const unknown = "unknown"

// ProgressState is an OSC 9;4 progress state. Values match
// GhosttyTerminalProgressState in include/ghostty/vt/terminal.h.
type ProgressState uint8

// Progress states reported by OSC 9;4.
const (
	// ProgressRemove clears any progress indication. It is the zero value.
	ProgressRemove ProgressState = iota
	// ProgressSet shows determinate progress at [Progress.Percent].
	ProgressSet
	// ProgressError shows a failed state, at [Progress.Percent] when set.
	ProgressError
	// ProgressIndeterminate shows progress with no known percent.
	ProgressIndeterminate
	// ProgressPause shows paused progress, at [Progress.Percent] when set.
	ProgressPause
)

// String returns the Ghostty name of s, such as "set", or "unknown".
func (s ProgressState) String() string {
	switch s {
	case ProgressRemove:
		return "remove"
	case ProgressSet:
		return "set"
	case ProgressError:
		return "error"
	case ProgressIndeterminate:
		return "indeterminate"
	case ProgressPause:
		return "pause"
	default:
		return unknown
	}
}

// ProgramState is an OSC 7501 program state. States are ordered by urgency,
// how much they need the user, least first, so the larger of two states is
// the more urgent. The protocol describes state only. How to show it is up
// to the host.
type ProgramState uint8

// Program states reported by OSC 7501, least urgent first. A clear report
// removes records instead of setting a state, so it has no value here.
const (
	// ProgramIdle means the program is at rest, waiting for the next
	// instruction. An interactive tool sitting at its own prompt is idle.
	// It is the zero value.
	ProgramIdle ProgramState = iota
	// ProgramDone means a piece of work finished and the result is ready
	// for the user to look at.
	ProgramDone
	// ProgramWorking means the program is running on its own, at
	// [ProgramStatus.Progress] when set.
	ProgramWorking
	// ProgramBlocked means the program cannot continue until the user
	// does something. [ProgramStatus.Kind] says what it needs and
	// [ProgramStatus.Message] says why.
	ProgramBlocked
	// ProgramError means the program failed and stopped.
	ProgramError
)

// String returns the Ghostty name of s, such as "working", or "unknown".
func (s ProgramState) String() string {
	switch s {
	case ProgramIdle:
		return "idle"
	case ProgramDone:
		return "done"
	case ProgramWorking:
		return "working"
	case ProgramBlocked:
		return "blocked"
	case ProgramError:
		return "error"
	default:
		return unknown
	}
}

// ProgramKind is what a blocked program waits for.
type ProgramKind uint8

// Program kinds reported by OSC 7501. They only apply to [ProgramBlocked].
const (
	// ProgramKindNone means the program did not say, or the state is not
	// [ProgramBlocked]. It is the zero value.
	ProgramKindNone ProgramKind = iota
	// ProgramKindPermission means the program wants approval to do
	// something, such as applying a change.
	ProgramKindPermission
	// ProgramKindQuestion means the program is waiting for an answer the
	// user has to type.
	ProgramKindQuestion
	// ProgramKindAuth means the program is waiting for a login, password,
	// token, or other credential.
	ProgramKindAuth
)

// String returns the Ghostty name of k, such as "permission". It is empty
// for [ProgramKindNone], since a report without a kind leaves it out, and
// "unknown" for a kind this package does not know.
func (k ProgramKind) String() string {
	switch k {
	case ProgramKindNone:
		return ""
	case ProgramKindPermission:
		return "permission"
	case ProgramKindQuestion:
		return "question"
	case ProgramKindAuth:
		return "auth"
	default:
		return unknown
	}
}

// ProgramStatus is one OSC 7501 program status record.
type ProgramStatus struct {
	// ID is the slash-separated record id. Empty is the root record.
	ID string
	// State is the record's state.
	State ProgramState
	// Kind is what a blocked record waits for. It is [ProgramKindNone] for
	// other states.
	Kind ProgramKind
	// App, Title, and Message are untrusted text from the program. App is
	// the application name, Title the report title, and Message its body.
	App     string
	Title   string
	Message string
	// Progress is 0 through 100. Negative means the report had none. The
	// zero value is 0%, so a record without progress sets -1.
	Progress int
}
