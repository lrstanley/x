// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package types holds shared terminal rendering data types.
package types //nolint:revive // shared terminal rendering data types

import (
	"image"
	"image/color"
	"slices"

	"github.com/lrstanley/x/term/still/units"
	"github.com/lrstanley/x/term/vt/screen"
)

// Metrics describes the terminal grid geometry used for a draw.
type Metrics struct {
	// Scale is the font scale the metrics were derived at, where 1 is the
	// default font size.
	Scale float64
	// CellWidth is the horizontal size of one character cell in pixels.
	CellWidth units.Px
	// CellHeight is the vertical size of one character cell in pixels.
	CellHeight units.Px
	// FontBaseline is the distance from the bottom of the cell to the font
	// baseline in pixels.
	FontBaseline units.Px
	// UnderlinePosition is the distance from the top of the cell to the
	// underline placement in pixels.
	UnderlinePosition units.Px
	// UnderlineThickness is the underline stroke width in pixels.
	UnderlineThickness units.Px
	// StrikethroughPosition is the distance from the top of the cell to the
	// strikethrough placement in pixels.
	StrikethroughPosition units.Px
	// StrikethroughThickness is the strikethrough stroke width in pixels.
	StrikethroughThickness units.Px
	// CursorThickness is the width of the cursor bar or block edge in pixels.
	CursorThickness units.Px
	// CursorHeight is the vertical extent of the cursor in pixels (often the
	// cell height).
	CursorHeight units.Px
	// BoxThickness is the stroke width for drawn boxes and borders in pixels.
	BoxThickness units.Px
	// IconHeight is the target vertical band in pixels for scaling Nerd
	// Font–style icons.
	IconHeight units.Px
	// IconHeightSingle is the target vertical band in pixels for single-cell
	// nerd icons.
	IconHeightSingle units.Px
	// FaceWidth is the measured advance width of the grid font face in
	// fractional pixels (for centering).
	FaceWidth units.FractionalPx
	// FaceHeight is the line box height of the grid font (ascent + descent
	// + line gap) in fractional pixels.
	FaceHeight units.FractionalPx
	// FaceY is the vertical offset of the face box within the cell in
	// fractional pixels.
	FaceY units.FractionalPx
}

// CellSize returns the pixel size of one terminal cell.
func (m Metrics) CellSize() image.Point {
	return image.Pt(m.CellWidth.Int(), m.CellHeight.Int())
}

// Palette holds baseline colors for terminal rendering when a cell or control
// sequence does not specify a color. Indexed entries map ANSI palette indices
// (e.g. 0–255) to RGB values; other fields apply to UI chrome and defaults.
type Palette struct {
	// Indexed maps 8- or 256-color palette indices to colors.
	Indexed map[int]color.Color
	// DefaultForeground is the text color when no explicit foreground is set.
	DefaultForeground color.Color
	// DefaultBackground is the cell background when no explicit background is
	// set.
	DefaultBackground color.Color
	// Cursor is the fill or outline color of the text cursor.
	Cursor color.Color
	// CursorText is the foreground color of text drawn over the cursor region.
	CursorText color.Color
	// Margin is the color used for padding or gutter areas outside the terminal
	// grid.
	Margin color.Color
	// Progress is the OSC 9;4 progress bar color for set and indeterminate
	// progress.
	Progress color.Color
	// ProgressError is the progress bar color for the error state.
	ProgressError color.Color
	// ProgressPause is the progress bar color for the pause state.
	ProgressPause color.Color
	// ProgramDone is the OSC 7501 program status color for done records.
	// Working, blocked, and error records use Progress, ProgressPause, and
	// ProgressError, so both reports read as one, and idle records a blend
	// of the default foreground into the default background.
	ProgramDone color.Color
}

// EmulatorState snapshots host-terminal properties that influence how a session
// is drawn but are not represented on [screen.Screen], such as title, focus, cursor
// mode, and scrollback metadata.
//
// EmulatorState holds a slice, so it is not comparable with ==; use
// [EmulatorState.Equal].
type EmulatorState struct {
	// Title is the window or tab title reported by the terminal.
	Title string
	// Focused is true when the terminal emulator window has keyboard focus.
	Focused bool
	// AltScreen is true when the alternate screen buffer is active.
	AltScreen bool
	// CursorVisible is true when the caret should be shown (subject to blink).
	CursorVisible bool
	// CursorX is the cursor column in the active screen buffer (zero-based).
	CursorX int
	// CursorY is the cursor row in the active screen buffer (zero-based).
	CursorY int
	// CursorColor is the cursor color chosen by DECSCUSR or similar, if known.
	CursorColor color.Color
	// CursorStyle is the cursor shape (block, bar, underline, etc.).
	CursorStyle screen.CursorShape
	// CursorBlink is true when cursor blinking is enabled.
	CursorBlink bool
	// FgColor is the default foreground when the emulator resets attributes.
	FgColor color.Color
	// BgColor is the default background when the emulator resets attributes.
	BgColor color.Color
	// ScrollbackCount is the number of lines retained above the viewport, if
	// tracked.
	ScrollbackCount int
	// Progress is the latest OSC 9;4 progress report. The zero value draws
	// nothing. The OSC 7501 spec has a terminal stop showing OSC 9;4 as
	// program status once any OSC 7501 report arrives, until the next full
	// reset.
	Progress Progress
	// ProgramStatuses are the OSC 7501 program status records, sorted by id,
	// so the root record (empty id) comes first. Nil draws nothing. They are
	// drawn only with window chrome: the most urgent record (see
	// [ProgramState]; the earlier record on ties) replaces the title and
	// colors an outline around the window, and each other record adds a dot
	// after the title. The renderer does not modify the slice.
	ProgramStatuses []ProgramStatus
}

// Equal reports whether s and o are the same state. Fields compare as with
// ==, colors as interface values, and ProgramStatuses element by element.
func (s EmulatorState) Equal(o EmulatorState) bool {
	return s.Title == o.Title &&
		s.Focused == o.Focused &&
		s.AltScreen == o.AltScreen &&
		s.CursorVisible == o.CursorVisible &&
		s.CursorX == o.CursorX &&
		s.CursorY == o.CursorY &&
		s.CursorColor == o.CursorColor &&
		s.CursorStyle == o.CursorStyle &&
		s.CursorBlink == o.CursorBlink &&
		s.FgColor == o.FgColor &&
		s.BgColor == o.BgColor &&
		s.ScrollbackCount == o.ScrollbackCount &&
		s.Progress == o.Progress &&
		slices.Equal(s.ProgramStatuses, o.ProgramStatuses)
}

// ProgramState is an OSC 7501 program state. States are ordered by urgency,
// how much they need the user, least first, so the larger of two states is
// the more urgent.
type ProgramState uint8

// Program states reported by OSC 7501, least urgent first. A clear report
// removes records instead of setting a state, so it has no value here.
const (
	// ProgramIdle means the program is at rest. It is the zero value.
	ProgramIdle ProgramState = iota
	// ProgramDone means the program finished and the result is ready.
	ProgramDone
	// ProgramWorking means the program is running, at
	// [ProgramStatus.Progress] when set.
	ProgramWorking
	// ProgramBlocked means the program waits on the user, for
	// [ProgramStatus.Kind].
	ProgramBlocked
	// ProgramError means the program failed and stopped.
	ProgramError
)

// ProgramKind is what a blocked program waits for.
type ProgramKind uint8

// Program kinds reported by OSC 7501. They only apply to [ProgramBlocked].
const (
	// ProgramKindNone means the program did not say. It is the zero value.
	ProgramKindNone ProgramKind = iota
	// ProgramKindPermission means the program wants approval to do something.
	ProgramKindPermission
	// ProgramKindQuestion means the program waits for an answer.
	ProgramKindQuestion
	// ProgramKindAuth means the program waits for a credential.
	ProgramKindAuth
)

// ProgramStatus is one OSC 7501 program status record.
type ProgramStatus struct {
	// ID is the slash-separated record id. Empty is the root record.
	ID string
	// State is the record's state.
	State ProgramState
	// Kind is what a blocked record waits for.
	Kind ProgramKind
	// App, Title, and Message are untrusted text from the program. They are
	// drawn as plain text, with controls, bidi overrides, and invisible
	// formatting characters removed. App is shown when Title is empty.
	App     string
	Title   string
	Message string
	// Progress is 0 through 100. Negative means the report had none.
	Progress int
}

// ProgressState is an OSC 9;4 progress state.
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

// Progress is an OSC 9;4 progress report.
type Progress struct {
	// State is the reported state.
	State ProgressState
	// Percent is 0 through 100. Negative means the report had no percent.
	Percent int
}
