// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"net/url"
	"slices"
	"strconv"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// maxPromptMarks is how many prompts a terminal keeps marks for. Each is a
// tracked grid reference, which libghostty-vt moves as rows scroll and
// reflow, so the oldest is dropped past this even while its row is still in
// the scrollback.
const maxPromptMarks = 1024

// PromptMark is a primary prompt the shell started with OSC 133;A, and how
// the command run from it ended.
type PromptMark struct {
	// Row is the prompt's first row, counting from the top of the
	// scrollback, so the live screen starts at row [State.ScrollbackLen].
	Row int
	// Ended is true once the shell reported the command finished (OSC
	// 133;D).
	Ended bool
	// HasExitCode is true when the shell included the command's exit status
	// when it ended.
	HasExitCode bool
	// ExitCode is the command's exit status. It is meaningful only when
	// HasExitCode is true.
	ExitCode int
}

// Failed reports whether the command run from the prompt ended with a
// non-zero exit status.
func (m PromptMark) Failed() bool {
	return m.HasExitCode && m.ExitCode != 0
}

// promptMark is a [PromptMark] whose row libghostty-vt tracks with ref, a
// GhosttyTrackedGridRef. Row is where ref was when it was last resolved.
type promptMark struct {
	PromptMark

	ref int32
}

// promptMarks are a terminal's prompt marks, oldest first.
type promptMarks struct {
	marks []promptMark
	// view is the marks as [Terminal.promptMarksLocked] last returned them.
	// It is replaced, never changed in place, so a [State] can share it.
	view []PromptMark
	// written is true when a write may have moved or erased rows since the
	// marks were resolved, and moved when a resize or a new scrollback cap
	// may have moved any of them.
	written, moved bool
	// top is the scrollback length when the marks were last resolved, so
	// marks from that row down were on the screen then.
	top int
}

// trackPromptLocked marks the cursor's row, where a primary prompt just
// started. A prompt redrawn on the row of the last mark keeps that mark, and
// one on the alternate screen, such as a shell in a full-screen program,
// gets none.
func (t *Terminal) trackPromptLocked() {
	if t.activeAlt() {
		return
	}
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, wasmvt.TerminalDataCursorY, t.scratch) != nil {
		return
	}
	pt, out := t.scratch, t.scratch+int32(wasmvt.SizePoint)
	t.writePoint(pt, Point{Tag: PointActive, Y: int(t.mod.U16(t.scratch, 0))})
	t.mod.Zero(out, wasmvt.SizeTrackedGridRef)
	if t.mod.Xghostty_terminal_grid_ref_track(t.term, pt, out) != 0 {
		return
	}
	m := promptMark{ref: t.mod.I32(out, 0)}
	pm := &t.prompts
	if !t.resolvePromptLocked(&m) ||
		(len(pm.marks) > 0 && t.resolvePromptLocked(&pm.marks[len(pm.marks)-1]) && pm.marks[len(pm.marks)-1].Row == m.Row) {
		t.mod.Xghostty_tracked_grid_ref_free(m.ref)
		return
	}
	if len(pm.marks) == maxPromptMarks {
		t.mod.Xghostty_tracked_grid_ref_free(pm.marks[0].ref)
		pm.marks = slices.Delete(pm.marks, 0, 1)
	}
	pm.marks = append(pm.marks, m)
}

// endPromptLocked records how the command from the last prompt ended, from
// the latest step. A second end, which some shells send with each prompt,
// does not change it, and neither does one on the alternate screen.
func (t *Terminal) endPromptLocked() {
	marks := t.prompts.marks
	if len(marks) == 0 || t.activeAlt() {
		return
	}
	if m := &marks[len(marks)-1]; !m.Ended {
		m.Ended, m.HasExitCode, m.ExitCode = true, t.prompt.HasExitCode, t.prompt.ExitCode
	}
}

// resolvePromptLocked sets m.Row to the row of its reference, counted from
// the top of the scrollback, and reports whether that is still a prompt row.
// When the scrollback is erased or pruned, libghostty-vt can move the
// reference of a row that goes to the top row rather than drop it, so a
// moved reference is told by its row not being flagged as a prompt, or by
// its mark not being above the next one (see compactPromptsLocked).
func (t *Terminal) resolvePromptLocked(m *promptMark) bool {
	// Past trackPromptLocked's point and reference.
	coord := t.scratch + 32
	ref := coord + int32(wasmvt.SizePointCoordinate)
	row := ref + 16
	t.mod.Zero(coord, wasmvt.SizePointCoordinate)
	if t.mod.Xghostty_tracked_grid_ref_point(m.ref, int32(PointScreen), coord) != 0 {
		return false
	}
	t.mod.Zero(ref, wasmvt.SizeGridRef)
	t.mod.SetU32(ref, wasmvt.GridRefSizeOff, uint32(wasmvt.SizeGridRef))
	if t.mod.Xghostty_tracked_grid_ref_snapshot(m.ref, ref) != 0 ||
		t.mod.Xghostty_grid_ref_row(ref, row) != 0 {
		return false
	}
	raw := int64(t.mod.U64(row, 0)) //nolint:gosec // A bit field, passed back as it is.
	if t.mod.Xghostty_row_get(raw, wasmvt.RowDataSemanticPrompt, row+8) != 0 ||
		t.mod.I32(row, 8) != wasmvt.RowSemanticPromptPrompt {
		return false
	}
	m.Row = int(t.mod.U32(coord, wasmvt.PointCoordinateYOff))
	return true
}

// promptMarksLocked returns the marks of the prompts still on the screen or
// in the scrollback, oldest first, and frees the rest. It is nil on the
// alternate screen, which a shell does not draw prompts on.
//
// After a write, only the oldest mark and those that were on the screen are
// resolved again: rows in the scrollback do not change, and all move
// together when the scrollback is erased or pruned, which moves or drops the
// oldest.
func (t *Terminal) promptMarksLocked() []PromptMark {
	pm := &t.prompts
	if len(pm.marks) == 0 {
		// The next marks are all on the screen.
		*pm = promptMarks{}
		return nil
	}
	if t.activeAlt() {
		return nil
	}
	if !pm.written && !pm.moved {
		return pm.view
	}
	for i := 0; i < len(pm.marks); i++ {
		m := &pm.marks[i]
		was := m.Row
		if !t.resolvePromptLocked(m) {
			m.Row = -1
		}
		if i > 0 || pm.moved || m.Row != was {
			continue
		}
		// The scrollback did not move: skip to the marks on the screen.
		on := slices.IndexFunc(pm.marks, func(m promptMark) bool { return m.Row >= pm.top })
		if on < 0 {
			break
		}
		i = max(i, on-1)
	}
	t.compactPromptsLocked()
	pm.written, pm.moved = false, false
	pm.top = t.scrollbackLenLocked()
	if !slices.EqualFunc(pm.marks, pm.view, func(m promptMark, v PromptMark) bool { return m.PromptMark == v }) {
		pm.view = make([]PromptMark, 0, len(pm.marks))
		for _, m := range pm.marks {
			pm.view = append(pm.view, m.PromptMark)
		}
	}
	if len(pm.view) == 0 {
		pm.view = nil
	}
	return pm.view
}

// compactPromptsLocked frees the marks that are gone, with row -1, and those
// not above the mark after them, whose rows were erased.
func (t *Terminal) compactPromptsLocked() {
	pm := &t.prompts
	kept := pm.marks[:0]
	for _, m := range pm.marks {
		if m.Row < 0 {
			t.mod.Xghostty_tracked_grid_ref_free(m.ref)
			continue
		}
		for len(kept) > 0 && kept[len(kept)-1].Row >= m.Row {
			t.mod.Xghostty_tracked_grid_ref_free(kept[len(kept)-1].ref)
			kept = kept[:len(kept)-1]
		}
		kept = append(kept, m)
	}
	clear(pm.marks[len(kept):])
	pm.marks = kept
}

// freePromptMarksLocked frees every mark, for a full reset, which can come
// part way through a write that starts new ones.
func (t *Terminal) freePromptMarksLocked() {
	for _, m := range t.prompts.marks {
		t.mod.Xghostty_tracked_grid_ref_free(m.ref)
	}
	t.prompts = promptMarks{moved: true}
}

// appendPromptVT appends body, the screen and its scrollback as rows ending
// in CRLF, to dst, with the OSC 133 sequences that start marks' prompts at
// the start of their rows, and that end their commands before the next
// prompt. When the last command has not ended, the sequence of last, the
// latest step, follows the body. Which cells were typed input and which were
// output is not kept.
func appendPromptVT(dst, body []byte, marks []PromptMark, last Prompt) []byte {
	row := 0
	var prev *PromptMark
	for i := range marks {
		m := &marks[i]
		for ; row < m.Row; row++ {
			line, rest, ok := bytes.Cut(body, []byte("\r\n"))
			if !ok {
				break
			}
			dst = append(append(dst, line...), "\r\n"...)
			body = rest
		}
		if row < m.Row {
			break
		}
		if prev != nil {
			dst = appendEndPromptVT(dst, *prev)
		}
		dst = append(dst, ansi.FinalTermPrompt()...)
		prev = m
	}
	dst = append(dst, body...)
	switch {
	case prev == nil:
	case prev.Ended:
		dst = appendEndPromptVT(dst, *prev)
	case last.Kind == PromptInput:
		dst = append(dst, ansi.FinalTermCmdStart()...)
	case last.Kind == PromptOutput && last.Command != "":
		dst = append(dst, ansi.FinalTermCmdExecuted("cmdline_url="+url.PathEscape(last.Command))...)
	case last.Kind == PromptOutput:
		dst = append(dst, ansi.FinalTermCmdExecuted()...)
	}
	return dst
}

// appendEndPromptVT appends the OSC 133;D that ends m's command, or nothing
// when it has not ended.
func appendEndPromptVT(dst []byte, m PromptMark) []byte {
	switch {
	case !m.Ended:
		return dst
	case m.HasExitCode:
		return append(dst, ansi.FinalTermCmdFinished(strconv.Itoa(m.ExitCode))...)
	default:
		return append(dst, ansi.FinalTermCmdFinished()...)
	}
}
