// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package screen

import (
	"image/color"
	"slices"
)

// Cursor is the caret of a screen.
type Cursor struct {
	// X is the column, starting at 0.
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
	// ScrollbackLen is the number of scrollback rows. It is zero on the
	// alternate screen.
	ScrollbackLen int
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
// ==, and ProgramStatuses element by element.
func (s State) Equal(o State) bool {
	return s.Title == o.Title &&
		s.Focused == o.Focused &&
		s.AltScreen == o.AltScreen &&
		s.Cursor == o.Cursor &&
		s.CursorColor == o.CursorColor &&
		s.CursorColorSet == o.CursorColorSet &&
		s.Foreground == o.Foreground &&
		s.Background == o.Background &&
		s.ScrollbackLen == o.ScrollbackLen &&
		s.Progress == o.Progress &&
		s.ProgramStatusGeneration == o.ProgramStatusGeneration &&
		s.ProgramStatusReported == o.ProgramStatusReported &&
		s.Generation == o.Generation &&
		slices.Equal(s.ProgramStatuses, o.ProgramStatuses)
}

// SameCommit reports whether s and o, two states of one terminal, show the
// same program output: the same grid generation, title, screen, cursor,
// shown progress, and program status generation. Unlike [State.Equal] it
// ignores focus, colors, and scrollback, and compares the program status
// records by generation, without reading them, so it is cheap enough to
// run on every write to tell a write that changed nothing from one that did.
func (s State) SameCommit(o State) bool {
	return s.Generation == o.Generation &&
		s.Title == o.Title &&
		s.AltScreen == o.AltScreen &&
		s.Cursor == o.Cursor &&
		s.ShownProgress() == o.ShownProgress() &&
		s.ProgramStatusGeneration == o.ProgramStatusGeneration
}

// SameLook reports whether s and o, two states of one terminal, are drawn
// the same: they are the same commit (see [State.SameCommit]), with the same
// focus and colors. Like SameCommit, it ignores scrollback.
func (s State) SameLook(o State) bool {
	return s.SameCommit(o) &&
		s.Focused == o.Focused &&
		s.Foreground == o.Foreground &&
		s.Background == o.Background &&
		s.CursorColor == o.CursorColor &&
		s.CursorColorSet == o.CursorColorSet
}
