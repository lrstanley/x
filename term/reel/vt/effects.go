// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"slices"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// Effects are the Ghostty terminal callbacks from
// include/ghostty/vt/terminal.h ("Effects"). installCallbacks puts each
// Go func in the Wasm table with [wasmvt.Module.Bind] and passes that
// index to ghostty_terminal_set. The index is the function pointer.
// It is not a pointer to the index.
//
// Callbacks run inside VT writes, with the terminal lock already held.
// They must not call ghostty_terminal_vt_write. Arguments are Wasm
// addresses. Copy bytes out before the next allocating call, because
// growth invalidates [wasmvt.Module.Slice]. Sized out-structs need
// their byte size in the first field, the same role as
// GHOSTTY_INIT_SIZED. Callbacks that fill an out pointer return 1.

// installCallbacks registers every effect once per Wasm instance.
// A second call is a no-op. The handles live for the life of the module,
// including after the terminal returns to the pool.
func (t *Terminal) installCallbacks() error {
	if t.callbacksOn {
		return nil
	}
	pairs := []struct {
		opt int32
		fn  any
	}{
		{wasmvt.TerminalOptionWritePty, t.onWritePty},
		{wasmvt.TerminalOptionBell, t.onBell},
		{wasmvt.TerminalOptionTitleChanged, t.onTitle},
		{wasmvt.TerminalOptionPwdChanged, t.onPwd},
		{wasmvt.TerminalOptionEnquiry, t.onEnquiry},
		{wasmvt.TerminalOptionXtversion, t.onXtversion},
		{wasmvt.TerminalOptionSize, t.onSize},
		{wasmvt.TerminalOptionColorScheme, t.onColorScheme},
		{wasmvt.TerminalOptionDeviceAttributes, t.onDeviceAttributes},
		{wasmvt.TerminalOptionClipboardWrite, t.onClipboardWrite},
		{wasmvt.TerminalOptionClipboardRead, t.onClipboardRead},
		{wasmvt.TerminalOptionDesktopNotification, t.onNotification},
		{wasmvt.TerminalOptionProgressReport, t.onProgress},
		{wasmvt.TerminalOptionProgramStatus, t.onProgramStatus},
		{wasmvt.TerminalOptionSemanticPrompt, t.onPrompt},
		{wasmvt.TerminalOptionRenderHold, t.onHold},
		{wasmvt.TerminalOptionReset, t.onReset},
		{wasmvt.TerminalOptionResizeWindow, t.onResizeWindow},
	}
	for _, pair := range pairs {
		idx := t.mod.Bind(pair.fn)
		if err := t.mod.SetOption(t.term, pair.opt, idx); err != nil {
			return err
		}
	}
	t.callbacksOn = true
	return nil
}

// onWritePty is GhosttyTerminalWritePtyFn. data/n is the reply the
// emulator wants written to the pty. The bytes are copied onto
// [Terminal.Read] before the callback returns.
func (t *Terminal) onWritePty(_, _ int32, data, n int32) {
	if n <= 0 || data == 0 || t.replies == nil {
		return
	}
	t.replies.Write(append([]byte(nil), t.mod.Slice(data, int(n))...))
}

func (t *Terminal) onBell(_, _ int32) {
	t.bell = time.Now()
}

// onTitle reads TerminalDataTitle. The GhosttyString is only valid
// until the next mutating terminal call, so readDataString copies it.
func (t *Terminal) onTitle(_, _ int32) {
	t.title = t.readDataString(wasmvt.TerminalDataTitle)
}

// onPwd reads TerminalDataPwd the same way as onTitle.
func (t *Terminal) onPwd(_, _ int32) {
	t.pwd = t.readDataString(wasmvt.TerminalDataPwd)
}

// onEnquiry answers ENQ with a single ACK byte. sret is a GhosttyString
// out-parameter. The ACK storage is allocated in prepare and must
// outlive this call.
func (t *Terminal) onEnquiry(sret, _, _ int32) {
	t.mod.StoreString(sret, 0, t.answer, 1)
}

// onXtversion answers CSI > q with [xtversion].
// sret is a GhosttyString out-parameter backed by storage from prepare.
func (t *Terminal) onXtversion(sret, _, _ int32) {
	t.mod.StoreString(sret, 0, t.version, len(xtversion))
}

// onSize fills a GhosttyTerminalSizeReport: rows, cols, then cell
// width and height in pixels. The field order is the manifest layout
// for this build (u16, u16, u32, u32). Returns 1 so Ghostty keeps
// the report.
func (t *Terminal) onSize(_, _, out int32) int32 {
	t.mod.SetU16(out, 0, uint16(t.rows))
	t.mod.SetU16(out, 2, uint16(t.cols))
	t.mod.SetU32(out, 4, t.cellW)
	t.mod.SetU32(out, 8, t.cellH)
	return 1
}

// onColorScheme reports a dark ColorScheme. This host has no light
// palette switch. Returns 1 so Ghostty keeps the value.
func (t *Terminal) onColorScheme(_, _, out int32) int32 {
	t.mod.SetI32(out, 0, wasmvt.ColorSchemeDark)
	return 1
}

// onDeviceAttributes fills GhosttyDeviceAttributes. primary is a
// nested struct, so conformance_level and num_features are reached
// by adding the generated offsets. Level 64 and no feature IDs is
// the VT identity this host advertises. Returns 1 on success.
func (t *Terminal) onDeviceAttributes(_, _, out int32) int32 {
	n := wasmvt.SizeDeviceAttributes
	t.mod.Zero(out, n)
	primary := wasmvt.DeviceAttributesPrimaryOff
	level := wasmvt.DeviceAttributesPrimaryConformanceLevelOff
	count := wasmvt.DeviceAttributesPrimaryNumFeaturesOff
	t.mod.SetU16(out, primary+level, 64)
	t.mod.SetU32(out, primary+count, 0)
	return 1
}

// onClipboardWrite handles OSC 52 / 1337 / 5522 writes. req is a
// GhosttyClipboardWrite. The reply function pointer is a table index
// invoked with [wasmvt.Module.Call2] before this returns.
func (t *Terminal) onClipboardWrite(_, _, req int32) {
	t.clipWrote = t.clipboardText(req)
	t.replyClipboardWrite(req)
}

// onClipboardRead handles a clipboard read request. The reply is
// built from [Terminal.SetClipboard] and passed back through the
// table index stored on the GhosttyClipboardRead.
func (t *Terminal) onClipboardRead(_, _, req int32) {
	t.replyClipboardRead(req)
}

// onNotification copies the title and body GhosttyStrings off req.
// req is a GhosttyTerminalDesktopNotification.
func (t *Terminal) onNotification(_, _, req int32) {
	title := wasmvt.TerminalDesktopNotificationTitleOff
	body := wasmvt.TerminalDesktopNotificationBodyOff
	t.notifications = append(t.notifications, Notification{
		Title: t.mod.String(req, title),
		Body:  t.mod.String(req, body),
	})
}

// onProgress reads a GhosttyTerminalProgressReport. progress is a
// signed byte, so the u8 load is converted through int8.
func (t *Terminal) onProgress(_, _, req int32) {
	state := t.mod.I32(req, wasmvt.TerminalProgressReportStateOff)
	pct := int(int8(t.mod.U8(req, wasmvt.TerminalProgressReportProgressOff)))
	t.progress = screen.Progress{Reported: true, State: progressState(state), Percent: pct}
}

// onProgramStatus reads a GhosttyTerminalProgramStatus (OSC 7501).
// String fields are copied immediately. A clear state deletes the id.
// Ghostty only calls it for valid reports, so any call counts as one.
func (t *Terminal) onProgramStatus(_, _, req int32) {
	state := t.mod.I32(req, wasmvt.TerminalProgramStatusStateOff)
	kind := t.mod.I32(req, wasmvt.TerminalProgramStatusKindOff)
	progress := int(int8(t.mod.U8(req, wasmvt.TerminalProgramStatusProgressOff)))
	id := t.mod.String(req, wasmvt.TerminalProgramStatusIdOff)
	if state == programClear {
		t.setProgramStatusesLocked(deleteProgramStatus(t.statuses, id), true)
		return
	}
	rec := screen.ProgramStatus{
		ID:       id,
		State:    programState(state),
		Kind:     programKind(kind),
		App:      t.mod.String(req, wasmvt.TerminalProgramStatusAppOff),
		Title:    t.mod.String(req, wasmvt.TerminalProgramStatusTitleOff),
		Message:  t.mod.String(req, wasmvt.TerminalProgramStatusMessageOff),
		Progress: -1,
	}
	if (rec.State == screen.ProgramWorking || rec.State == screen.ProgramBlocked) && progress >= 0 && progress <= 100 {
		rec.Progress = progress
	}
	t.setProgramStatusesLocked(putProgramStatus(t.statuses, rec), true)
}

// setProgramStatusesLocked replaces the records with next, which the caller
// must not change afterwards, and sets whether any OSC 7501 report has
// arrived since the last full reset. When next is not the current records,
// it rebuilds the sorted view, and when that differs, replaces it and bumps
// the generation. The old view is left alone, since states may share it.
func (t *Terminal) setProgramStatusesLocked(next []screen.ProgramStatus, reported bool) {
	t.statusReported = reported
	if len(next) == len(t.statuses) && (len(next) == 0 || &next[0] == &t.statuses[0]) {
		return
	}
	t.statuses = next
	view := programStatusView(next)
	if slices.Equal(view, t.statusView) {
		return
	}
	t.statusView = view
	t.statusGen++
}

// onPrompt reads a GhosttyTerminalSemanticPrompt (OSC 133).
// has_exit_code gates whether exit_code is meaningful.
func (t *Terminal) onPrompt(_, _, req int32) {
	kind := t.mod.I32(req, wasmvt.TerminalSemanticPromptKindOff)
	t.prompt.Kind = promptName(kind)
	t.prompt.Command = t.mod.String(req, wasmvt.TerminalSemanticPromptCommandOff)
	has := t.mod.U8(req, wasmvt.TerminalSemanticPromptHasExitCodeOff) != 0
	t.prompt.HasExitCode = has
	if has {
		t.prompt.ExitCode = int(t.mod.I32(req, wasmvt.TerminalSemanticPromptExitCodeOff))
	} else {
		t.prompt.ExitCode = 0
	}
	if t.prompt.Kind == PromptStart {
		t.setProgramStatusesLocked(deleteWorking(t.statuses), t.statusReported)
	}
}

// onHold is the synchronized-output hold (mode 2026). held is a
// C bool. The first hold freezes the last captured frame so reads
// do not show a partial update.
func (t *Terminal) onHold(_, _, held int32) {
	if held != 0 {
		if !t.held {
			t.frozen, t.frozenCur = t.captureFrame()
		}
		t.held = true
		return
	}
	t.held = false
	t.frozen = nil
	t.gridFresh = false
}

func (t *Terminal) onReset(_, _ int32) {
	t.resetStateLocked()
	_ = t.reapplyLocked()
}

// onResizeWindow reads a GhosttyTerminalResizeWindow tagged union.
// Only the grid arm is recorded. value.grid.rows and columns are
// nested, so the offsets are added. The request is exposed through
// [Terminal.ResizeRequest] and is not applied here.
func (t *Terminal) onResizeWindow(_, _, req int32) {
	tag := t.mod.I32(req, wasmvt.TerminalResizeWindowTagOff)
	if tag != wasmvt.TerminalResizeWindowTagGrid {
		return
	}
	value := wasmvt.TerminalResizeWindowValueOff
	grid := wasmvt.TerminalResizeWindowValueGridOff
	base := value + grid
	rows := wasmvt.TerminalResizeWindowGridRowsOff
	cols := wasmvt.TerminalResizeWindowGridColumnsOff
	t.resizeRows = int(t.mod.U16(req, base+rows))
	t.resizeCols = int(t.mod.U16(req, base+cols))
	t.resizeOK = true
}

// clipboardText walks the GhosttyClipboardContent array on a write
// request and returns the text/plain payload. An empty mime is
// treated as plain text. Strings are copied by [wasmvt.Module.String].
func (t *Terminal) clipboardText(req int32) string {
	contents := int32(t.mod.U32(req, wasmvt.ClipboardWriteContentsOff))
	n := int(t.mod.U32(req, wasmvt.ClipboardWriteContentsLenOff))
	if contents == 0 || n <= 0 {
		return ""
	}
	stride := wasmvt.SizeClipboardContent
	mimeOff := wasmvt.ClipboardContentMimeOff
	dataOff := wasmvt.ClipboardContentDataOff
	var plain string
	for i := range n {
		base := int(contents) + i*stride
		mime := t.mod.String(int32(base), mimeOff)
		data := t.mod.String(int32(base), dataOff)
		if mime == "" || strings.HasPrefix(mime, "text/plain") {
			plain = data
			if mime != "" {
				return plain
			}
		}
	}
	return plain
}

// replyClipboardWrite calls the Wasm reply stored on the request.
// The reply struct is sized (first field is the byte size) and
// reports ClipboardWriteResultSuccess. Allocation failure drops
// the reply. Ghostty then sees an unanswered request.
func (t *Terminal) replyClipboardWrite(req int32) {
	replyIdx := int32(t.mod.U32(req, wasmvt.ClipboardWriteReplyOff))
	n := wasmvt.SizeClipboardWriteReply
	ptr, err := t.mod.Alloc(n)
	if err != nil {
		return
	}
	defer t.mod.Free(ptr, n)
	t.mod.Zero(ptr, n)
	t.mod.SetU32(ptr, 0, uint32(n))
	t.mod.SetI32(ptr, wasmvt.ClipboardWriteReplyResultOff, wasmvt.ClipboardWriteResultSuccess)
	t.mod.Call2(replyIdx, req, ptr)
}

// replyClipboardRead builds one GhosttyClipboardContent when the
// host clipboard is non-empty, then calls the Wasm reply. The mime
// and data bytes must stay allocated until Call2 returns. Ghostty
// reads them during that call.
func (t *Terminal) replyClipboardRead(req int32) {
	replyIdx := int32(t.mod.U32(req, wasmvt.ClipboardReadReplyOff))
	text := t.clipRead
	var content int32
	if text != "" {
		raw := append([]byte(nil), text...)
		data, err := t.mod.Alloc(len(raw))
		if err != nil {
			return
		}
		defer t.mod.Free(data, len(raw))
		t.mod.Put(data, raw)
		mimeBytes := []byte("text/plain")
		mime, err := t.mod.Alloc(len(mimeBytes))
		if err != nil {
			return
		}
		defer t.mod.Free(mime, len(mimeBytes))
		t.mod.Put(mime, mimeBytes)
		content, err = t.mod.Alloc(wasmvt.SizeClipboardContent)
		if err != nil {
			return
		}
		defer t.mod.Free(content, wasmvt.SizeClipboardContent)
		t.mod.Zero(content, wasmvt.SizeClipboardContent)
		t.mod.StoreString(content, wasmvt.ClipboardContentMimeOff, mime, len(mimeBytes))
		t.mod.StoreString(content, wasmvt.ClipboardContentDataOff, data, len(raw))
	}
	n := wasmvt.SizeClipboardReadReply
	ptr, err := t.mod.Alloc(n)
	if err != nil {
		return
	}
	defer t.mod.Free(ptr, n)
	t.mod.Zero(ptr, n)
	t.mod.SetU32(ptr, 0, uint32(n))
	t.mod.SetI32(ptr, wasmvt.ClipboardReadReplyResultOff, wasmvt.ClipboardReadResultSuccess)
	if content != 0 {
		t.mod.SetU32(ptr, wasmvt.ClipboardReadReplyContentsOff, uint32(content))
		t.mod.SetU32(ptr, wasmvt.ClipboardReadReplyContentsLenOff, 1)
	}
	t.mod.Call2(replyIdx, req, ptr)
}

// readDataString calls ghostty_terminal_get into scratch for a
// TerminalData value whose output is a GhosttyString, and copies
// the bytes out. A failed get returns an empty string.
func (t *Terminal) readDataString(data int32) string {
	n := wasmvt.SizeString
	t.mod.Zero(t.scratch, n)
	if err := t.mod.Get(t.term, data, t.scratch); err != nil {
		return ""
	}
	return t.mod.String(t.scratch, 0)
}

// progressState maps a GhosttyTerminalProgressState, whose values
// [screen.ProgressState] shares. A state this build does not know clears
// the progress.
func progressState(v int32) screen.ProgressState {
	if v < 0 || v > int32(screen.ProgressPause) {
		return screen.ProgressRemove
	}
	return screen.ProgressState(v)
}

// programClear is the GhosttyProgramStatusState of a clear report, which
// removes records instead of setting a state.
const programClear = 5

// programState maps a GhosttyProgramStatusState other than [programClear].
// [screen.ProgramState] is ordered by urgency instead of by value. A state
// this build does not know is idle.
func programState(v int32) screen.ProgramState {
	switch v {
	case 1:
		return screen.ProgramWorking
	case 2:
		return screen.ProgramDone
	case 3:
		return screen.ProgramBlocked
	case 4:
		return screen.ProgramError
	default:
		return screen.ProgramIdle
	}
}

// programKind maps a GhosttyProgramStatusKind, whose values
// [screen.ProgramKind] shares. A kind this build does not know is none.
func programKind(v int32) screen.ProgramKind {
	if v < 0 || v > int32(screen.ProgramKindAuth) {
		return screen.ProgramKindNone
	}
	return screen.ProgramKind(v)
}

func promptName(v int32) PromptKind {
	switch v {
	case 1:
		return PromptStart
	case 2:
		return PromptInput
	case 3:
		return PromptOutput
	case 4:
		return PromptCommand
	default:
		return PromptNone
	}
}

const programStatusMax = 256

// putProgramStatus returns in with rec replacing the record of its id, as
// the most recently updated, evicting the least recently updated record when
// that would make more than programStatusMax. It returns in itself when rec
// is already its most recent record.
func putProgramStatus(in []screen.ProgramStatus, rec screen.ProgramStatus) []screen.ProgramStatus {
	if len(in) > 0 && in[len(in)-1] == rec {
		return in
	}
	out := make([]screen.ProgramStatus, 0, len(in)+1)
	for _, old := range in {
		if old.ID != rec.ID {
			out = append(out, old)
		}
	}
	if len(out) == programStatusMax {
		out = out[1:]
	}
	return append(out, rec)
}

// deleteProgramStatus returns in without the record id and every record
// beneath it, or without any record for the root id. It returns in itself
// when that removes nothing.
func deleteProgramStatus(in []screen.ProgramStatus, id string) []screen.ProgramStatus {
	return deleteProgramStatusFunc(in, func(rec screen.ProgramStatus) bool {
		return id == "" || rec.ID == id || strings.HasPrefix(rec.ID, id+"/")
	})
}

// deleteWorking returns in without its working and blocked records, which a
// new shell prompt ends. It returns in itself when there are none.
func deleteWorking(in []screen.ProgramStatus) []screen.ProgramStatus {
	return deleteProgramStatusFunc(in, func(rec screen.ProgramStatus) bool {
		return rec.State == screen.ProgramWorking || rec.State == screen.ProgramBlocked
	})
}

// deleteProgramStatusFunc returns in without the records del reports, as a
// new slice, or in itself when del reports none.
func deleteProgramStatusFunc(in []screen.ProgramStatus, del func(screen.ProgramStatus) bool) []screen.ProgramStatus {
	first := slices.IndexFunc(in, del)
	if first < 0 {
		return in
	}
	out := make([]screen.ProgramStatus, first, len(in))
	copy(out, in[:first])
	for _, rec := range in[first+1:] {
		if !del(rec) {
			out = append(out, rec)
		}
	}
	return out
}

// programStatusView returns in sorted by id, each record with App filled from
// its nearest ancestor that set one. It is nil when in is empty. Ids are
// unique, and an ancestor sorts before its descendants, so each record takes
// App from its nearest ancestor in the view, which already has its own.
func programStatusView(in []screen.ProgramStatus) []screen.ProgramStatus {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b screen.ProgramStatus) int {
		return strings.Compare(a.ID, b.ID)
	})
	for i := range out {
		id := out[i].ID
		for out[i].App == "" && id != "" {
			prefix, _, found := strings.CutLast(id, "/")
			if !found {
				prefix = ""
			}
			id = prefix
			if j, ok := slices.BinarySearchFunc(out[:i], id, func(rec screen.ProgramStatus, id string) int {
				return strings.Compare(rec.ID, id)
			}); ok {
				out[i].App = out[j].App
				break
			}
		}
	}
	return out
}
