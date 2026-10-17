// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"encoding/base64"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/charm/steep/snapshot"
)

// Program Status Protocol limits. A report that breaks one is discarded whole.
// See https://www.superlogical.com/rex/docs/build/program-status
const (
	programStatusMaxRecords = 256
	programStatusMaxSeq     = 4096
	programStatusMaxKey     = 16
	programStatusMaxMsgEnc  = 2732
	programStatusMaxMsg     = 2048
	programStatusMaxTitleEn = 256
	programStatusMaxTitle   = 192
	programStatusMaxApp     = 32
	programStatusMaxID      = 128
	programStatusMaxSegment = 32
	programStatusMaxDepth   = 8
)

// programStatusSupportReply answers OSC 7501 feature detection. states and
// kinds list every value this terminal accepts, per protocol revision 0.4.
const programStatusSupportReply = "\x1b]7501;?:states=idle,working,done,blocked,error:kinds=permission,question,auth\x07"

// ProgramStatuses returns the current OSC 7501 records, sorted by id. App is
// copied from the nearest ancestor that set one. Each report replaces its
// record; at most 256 records are kept, and the least recently updated record
// is dropped when a new id would exceed that. The returned slice is a copy.
//
// Working and blocked records are removed when a shell prompt starts (OSC 133
// A). A full reset (RIS) removes every record. Alternate-screen switches and
// soft reset do not. Status is not drawn into the screen.
func (h *Harness) ProgramStatuses() []ansi.ProgramStatus {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.programStatusesLocked()
}

func (e *emulator) handleProgramStatus(data []byte) bool {
	if len(data)+4 > programStatusMaxSeq {
		return true
	}
	body := oscPayload(data, 7501)
	if programStatusQuery(body) {
		_, _ = io.WriteString(e.vt.InputPipe(), programStatusSupportReply)
		return true
	}
	rec, clear, ok := parseProgramStatus(body)
	if !ok {
		return true
	}
	e.trackMu.Lock()
	defer e.trackMu.Unlock()
	if clear {
		e.statuses = slices.DeleteFunc(e.statuses, func(old ansi.ProgramStatus) bool {
			return rec.ID == "" || old.ID == rec.ID || strings.HasPrefix(old.ID, rec.ID+"/")
		})
		return true
	}
	e.statuses = slices.DeleteFunc(e.statuses, func(old ansi.ProgramStatus) bool {
		return old.ID == rec.ID
	})
	if len(e.statuses) == programStatusMaxRecords {
		e.statuses = slices.Delete(e.statuses, 0, 1)
	}
	e.statuses = append(e.statuses, rec)
	return true
}

// handleShellPrompt drops working and blocked records when a shell prompt
// begins (OSC 133 A). Idle, done, and error remain.
func (e *emulator) handleShellPrompt(data []byte) bool {
	body := oscPayload(data, 133)
	if body != "A" && !strings.HasPrefix(body, "A;") {
		return false
	}
	e.trackMu.Lock()
	e.statuses = slices.DeleteFunc(e.statuses, func(s ansi.ProgramStatus) bool {
		return s.State == ansi.ProgramStateWorking || s.State == ansi.ProgramStateBlocked
	})
	e.trackMu.Unlock()
	return true
}

// programStatusesLocked returns a display copy. The caller must hold trackMu.
func (e *emulator) programStatusesLocked() []ansi.ProgramStatus {
	out := slices.Clone(e.statuses)
	for i := range out {
		id := out[i].ID
		for out[i].App == "" && id != "" {
			if at := strings.LastIndexByte(id, '/'); at >= 0 {
				id = id[:at]
			} else {
				id = ""
			}
			for _, parent := range e.statuses {
				if parent.ID == id {
					out[i].App = parent.App
					break
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b ansi.ProgramStatus) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func snapshotProgramStatuses(in []ansi.ProgramStatus) []snapshot.ProgramStatus {
	if len(in) == 0 {
		return nil
	}
	out := make([]snapshot.ProgramStatus, len(in))
	for i, s := range in {
		out[i] = snapshot.ProgramStatus{
			ID:      s.ID,
			State:   string(s.State),
			Kind:    string(s.Kind),
			App:     s.App,
			Title:   s.Title,
			Message: s.Message,
		}
		if s.HasProgress {
			progress := s.Progress
			out[i].Progress = &progress
		}
	}
	return out
}

// oscPayload returns the bytes after "cmd;". The VT parser passes the whole
// OSC data, including the numeric command.
func oscPayload(data []byte, cmd int) string {
	s := strings.TrimPrefix(string(data), strconv.Itoa(cmd)+";")
	return strings.TrimSpace(s)
}

func programStatusQuery(body string) bool {
	return body == "?" || strings.HasPrefix(body, "?:") || strings.HasPrefix(body, "? ")
}

// parseProgramStatus reports whether body is a valid report. clear is set when
// the report removes records instead of replacing one.
func parseProgramStatus(body string) (rec ansi.ProgramStatus, clear bool, ok bool) {
	if body == "" {
		return ansi.ProgramStatus{}, false, false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(body, ":") {
		part = strings.TrimSpace(part)
		key, val, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" || !lowerKey(key) {
			continue
		}
		if len(key) > programStatusMaxKey {
			return ansi.ProgramStatus{}, false, false
		}
		if !statusValue(val) {
			continue
		}
		switch key {
		case "msg":
			if len(val) > programStatusMaxMsgEnc {
				return ansi.ProgramStatus{}, false, false
			}
		case "title":
			if len(val) > programStatusMaxTitleEn {
				return ansi.ProgramStatus{}, false, false
			}
		case "app":
			if len(val) > programStatusMaxApp {
				return ansi.ProgramStatus{}, false, false
			}
		case "id":
			if len(val) > programStatusMaxID {
				return ansi.ProgramStatus{}, false, false
			}
		}
		fields[key] = val
	}

	state, hasState := fields["state"]
	if !hasState {
		return ansi.ProgramStatus{}, false, false
	}
	switch ansi.ProgramState(state) {
	case ansi.ProgramStateIdle, ansi.ProgramStateWorking, ansi.ProgramStateDone,
		ansi.ProgramStateBlocked, ansi.ProgramStateError, ansi.ProgramStateClear:
	default:
		return ansi.ProgramStatus{}, false, false
	}

	rec.State = ansi.ProgramState(state)
	if id, hasID := fields["id"]; hasID {
		if !validProgramStatusID(id) {
			return ansi.ProgramStatus{}, false, false
		}
		rec.ID = id
	}
	// A bad title or message discards the report before it clears or replaces.
	var title, message string
	if raw, hasTitle := fields["title"]; hasTitle {
		title, ok = decodeProgramStatusText(raw, programStatusMaxTitle)
		if !ok {
			return ansi.ProgramStatus{}, false, false
		}
	}
	if raw, hasMsg := fields["msg"]; hasMsg {
		message, ok = decodeProgramStatusText(raw, programStatusMaxMsg)
		if !ok {
			return ansi.ProgramStatus{}, false, false
		}
	}
	if rec.State == ansi.ProgramStateClear {
		return rec, true, true
	}

	if app, hasApp := fields["app"]; hasApp && validProgramStatusSegment(app) {
		rec.App = app
	}
	if rec.State == ansi.ProgramStateBlocked {
		switch ansi.ProgramStatusKind(fields["kind"]) {
		case ansi.ProgramStatusKindPermission, ansi.ProgramStatusKindQuestion, ansi.ProgramStatusKindAuth:
			rec.Kind = ansi.ProgramStatusKind(fields["kind"])
		}
	}
	if rec.State == ansi.ProgramStateWorking || rec.State == ansi.ProgramStateBlocked {
		if raw, hasProgress := fields["progress"]; hasProgress {
			n, err := strconv.Atoi(raw)
			if err == nil && n >= 0 && n <= 100 {
				rec.Progress = n
				rec.HasProgress = true
			}
		}
	}
	rec.Title = title
	rec.Message = message
	return rec, false, true
}

func lowerKey(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return true
}

// statusValue reports whether v is a protocol value. '/' is accepted so record
// ids such as build/test survive; the pair grammar otherwise excludes ':' and ';'.
func statusValue(v string) bool {
	for i := range len(v) {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == ',' || c == '+' || c == '/' || c == '=' || c == '-':
		default:
			return false
		}
	}
	return true
}

func validProgramStatusID(id string) bool {
	if id == "" || len(id) > programStatusMaxID {
		return false
	}
	segs := strings.Split(id, "/")
	if len(segs) > programStatusMaxDepth {
		return false
	}
	for _, seg := range segs {
		if !validProgramStatusSegment(seg) {
			return false
		}
	}
	return true
}

func validProgramStatusSegment(s string) bool {
	if len(s) < 1 || len(s) > programStatusMaxSegment {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return true
}

func decodeProgramStatusText(s string, limit int) (string, bool) {
	if s == "" {
		return "", true
	}
	var (
		b   []byte
		err error
	)
	if len(s)%4 == 0 {
		b, err = base64.StdEncoding.DecodeString(s)
	}
	if err != nil || len(s)%4 != 0 {
		b, err = base64.RawStdEncoding.DecodeString(s)
	}
	if err != nil || !utf8.Valid(b) || len(b) > limit {
		return "", false
	}
	text := string(b)
	for _, r := range text {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return "", false
		}
	}
	return text, true
}
