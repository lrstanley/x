// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"image/color"
	"slices"
)

// Cursor is the caret of a screen.
type Cursor struct {
	// X is the column, starting at 0. It is -1 when the viewport is scrolled
	// off the cursor (see [State.ScrollbackOffset]), as is Y.
	X int
	// Y is the viewport row, starting at 0.
	Y int
	// Visible is false when the caret is hidden by mode 25.
	Visible bool
	// Blink is true when the caret is set to blink. The host draws the frames.
	Blink bool
	// Shape is the caret's visual style.
	Shape CursorShape
}

// State is the terminal state around the cell grid that changes how a
// screen is drawn, such as the title, the cursor, and the default colors.
// The emulator fills it and a renderer reads it, so neither depends on the
// other. The zero value is a focus-less terminal with nothing reported and
// the renderer's default colors.
//
// State holds a slice, so it is not comparable with ==. Use [State.Equal],
// or [State.SameCommit] to compare what a program wrote.
type State struct {
	// Title is the last OSC 0 / OSC 2 title.
	Title string
	// Focused reports whether the terminal window has keyboard focus. The
	// emulator does not track it: whoever drives the terminal sets it.
	Focused bool
	// AltScreen reports whether the alternate screen is active.
	AltScreen bool
	// Cursor is the caret from the same grid read as Generation.
	Cursor Cursor
	// CursorColor is the cursor color. CursorColorSet is false when
	// neither the host nor the program has set one, and the renderer
	// picks its own.
	CursorColor    color.NRGBA
	CursorColorSet bool
	// Foreground and Background are the colors of cells that do not set
	// their own. The zero color means the renderer's default.
	Foreground color.NRGBA
	Background color.NRGBA
	// ReverseColors reports whether reverse video (DECSCNM, mode 5) is set,
	// which the renderer draws by swapping the default foreground and
	// background. The emulator leaves the cells as they are.
	ReverseColors bool
	// Palette is the indexed colors the program set with OSC 4 and has not
	// reset with OSC 104, which the renderer draws over its own, or nil
	// when there are none. A color set to the emulator's default for its
	// index does not count. It may be shared with the emulator and other
	// states and must not be modified: the emulator replaces it rather
	// than changing it.
	Palette *Palette
	// ScrollbackLen is the number of scrollback rows. It is zero on the
	// alternate screen.
	ScrollbackLen int
	// ScrollbackOffset is how many rows the viewport is scrolled up from the
	// bottom, between zero and ScrollbackLen. Zero follows live output, and
	// is the zero value, so a state built by hand shows the live screen.
	ScrollbackOffset int
	// Progress is the latest OSC 9;4 report. See [State.ShownProgress] for
	// the progress to draw.
	Progress Progress
	// ProgramStatuses are the OSC 7501 records, sorted by id, so the root
	// record (empty id) comes first. App is filled from the nearest ancestor
	// that set one. The slice may be shared with the emulator and other
	// states and must not be modified: the emulator replaces it rather than
	// changing it.
	ProgramStatuses []ProgramStatus
	// ProgramStatusGeneration changes whenever ProgramStatuses do, and never
	// repeats for a terminal, so two states of one terminal with the same
	// generation hold the same records.
	ProgramStatusGeneration uint64
	// ProgramStatusReported is true once any OSC 7501 report, including a
	// clear, has arrived since the last full reset (RIS).
	ProgramStatusReported bool
	// Generation is the content generation of the cell grid read with this
	// state, see [Buffer.Generation]. Zero means there is no grid.
	Generation uint64

	// WorkingDirectory is the last OSC 7 working directory.
	WorkingDirectory string
	// Bell is the latest BEL.
	Bell Bell
	// Notifications are the OSC 9 and OSC 777 desktop notifications since
	// the last full reset, oldest first. The slice may be shared with the
	// emulator and other states and must not be modified.
	Notifications []Notification
	// Clipboard is the program's latest clipboard write.
	Clipboard ClipboardWrite
	// Prompt is the latest OSC 133 semantic-prompt step.
	Prompt Prompt
}

// ShownProgress is the OSC 9;4 progress to show for s: none when it is
// [ProgressRemove], and none once any OSC 7501 report has arrived, until the
// next full reset, since the OSC 7501 spec asks a terminal that shows OSC 9;4
// progress as program status to stop doing so.
func (s State) ShownProgress() Progress {
	if s.ProgramStatusReported || s.Progress.State == ProgressRemove {
		return Progress{}
	}
	return s.Progress
}

// Equal reports whether s and o are the same state. Fields compare as with
// ==, Palette by its colors, and ProgramStatuses and Notifications element
// by element.
func (s State) Equal(o State) bool {
	return s.Title == o.Title &&
		s.Focused == o.Focused &&
		s.AltScreen == o.AltScreen &&
		s.Cursor == o.Cursor &&
		s.CursorColor == o.CursorColor &&
		s.CursorColorSet == o.CursorColorSet &&
		s.Foreground == o.Foreground &&
		s.Background == o.Background &&
		s.ReverseColors == o.ReverseColors &&
		samePalette(s.Palette, o.Palette) &&
		s.ScrollbackLen == o.ScrollbackLen &&
		s.ScrollbackOffset == o.ScrollbackOffset &&
		s.Progress == o.Progress &&
		s.ProgramStatusGeneration == o.ProgramStatusGeneration &&
		s.ProgramStatusReported == o.ProgramStatusReported &&
		s.Generation == o.Generation &&
		slices.Equal(s.ProgramStatuses, o.ProgramStatuses) &&
		s.sameEvents(o) &&
		slices.Equal(s.Notifications, o.Notifications)
}

// SameCommit reports whether s and o, two states of one terminal, show the
// same program output: the same grid generation, title, screen, cursor,
// shown progress, program status generation, and program events (working
// directory, bell, notifications, clipboard write, and prompt step). Unlike
// [State.Equal] it ignores focus, colors, and scrollback, compares the
// program status records by generation, and the notifications by count and
// slice they share, without reading them, so it is cheap enough to run on
// every write to tell a write that changed nothing from one that did.
func (s State) SameCommit(o State) bool {
	return s.Generation == o.Generation &&
		s.Title == o.Title &&
		s.AltScreen == o.AltScreen &&
		s.Cursor == o.Cursor &&
		s.ShownProgress() == o.ShownProgress() &&
		s.ProgramStatusGeneration == o.ProgramStatusGeneration &&
		s.sameEvents(o) &&
		s.sameNotifications(o)
}

// sameEvents reports whether s and o have the same program events, but for
// the notifications.
func (s State) sameEvents(o State) bool {
	return s.WorkingDirectory == o.WorkingDirectory &&
		s.Bell == o.Bell &&
		s.Clipboard == o.Clipboard &&
		s.Prompt == o.Prompt
}

// sameNotifications reports whether s and o, two states of one terminal,
// have the same notifications. A terminal only appends to them, and starts
// a new slice on a full reset, so two of its states hold the same ones when
// they hold as many from the same slice.
func (s State) sameNotifications(o State) bool {
	n := len(s.Notifications)
	return n == len(o.Notifications) && (n == 0 || &s.Notifications[0] == &o.Notifications[0])
}

// SameLook reports whether s and o, two states of one terminal, are drawn
// the same: they are the same commit (see [State.SameCommit]), with the same
// focus, colors, and scrollback offset. Like SameCommit, it ignores the
// scrollback length, which only changes with output.
func (s State) SameLook(o State) bool {
	return s.SameCommit(o) &&
		s.Focused == o.Focused &&
		s.ScrollbackOffset == o.ScrollbackOffset &&
		s.Foreground == o.Foreground &&
		s.Background == o.Background &&
		s.ReverseColors == o.ReverseColors &&
		samePalette(s.Palette, o.Palette) &&
		s.CursorColor == o.CursorColor &&
		s.CursorColorSet == o.CursorColorSet
}
