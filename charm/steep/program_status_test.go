// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func osc7501(body string) string { return "\x1b]7501;" + body + "\x1b\\" }

func writeVT(t *testing.T, h *Harness, s string) {
	t.Helper()
	h.emulator.mu.Lock()
	defer h.emulator.mu.Unlock()
	if _, err := h.emulator.vt.WriteString(s); err != nil {
		t.Fatalf("write vt: %v", err)
	}
}

func TestProgramStatusProtocol(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 8, 2)
	h.emulator.mu.Lock()
	before := h.emulator.vt.Render()
	h.emulator.mu.Unlock()

	stream := osc7501("?") + osc7501("state=blocked:kind=question:progress=42:app=agent:id=job:title=VGl0bGU=:msg=SGVsbG8=")
	h.emulator.mu.Lock()
	for i := range len(stream) {
		if _, err := h.emulator.vt.WriteString(stream[i : i+1]); err != nil {
			h.emulator.mu.Unlock()
			t.Fatalf("write vt: %v", err)
		}
	}
	after := h.emulator.vt.Render()
	h.emulator.mu.Unlock()

	if before != after {
		t.Fatal("program status changed the screen")
	}
	h.WaitMessage[tea.ProgramStatusSupportMsg]()

	got := h.ProgramStatuses()
	if len(got) != 1 {
		t.Fatalf("records = %+v", got)
	}
	want := ansi.ProgramStatus{
		State: ansi.ProgramStateBlocked, Kind: ansi.ProgramStatusKindQuestion,
		Progress: 42, HasProgress: true, App: "agent", ID: "job",
		Title: "Title", Message: "Hello",
	}
	if got[0] != want {
		t.Fatalf("record = %+v, want %+v", got[0], want)
	}

	cloned := h.ProgramStatuses()
	cloned[0].State = ansi.ProgramStateError
	writeVT(t, h, osc7501("state=done:id=job"))
	got = h.ProgramStatuses()
	want = ansi.ProgramStatus{State: ansi.ProgramStateDone, ID: "job"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("replacement retained fields: %+v", got)
	}

	for _, bad := range []string{
		"state=unknown:id=job",
		"state=error:id=job:msg=A",
		"state=error:id=job:msg=Gwo=",
		"state=error:id=/",
		"state=error:id=job:msg=" + strings.Repeat("A", 2800),
	} {
		writeVT(t, h, osc7501(bad))
		got = h.ProgramStatuses()
		if len(got) != 1 || got[0] != want {
			t.Fatalf("invalid report applied: %q: %+v", bad, got)
		}
	}
}

func TestProgramStatusLifecycle(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 8, 2)
	writeVT(t, h, osc7501("state=idle:app=root")+
		osc7501("state=working:id=build:app=compiler")+
		osc7501("state=blocked:id=build/test")+
		osc7501("state=done:id=build2"))
	got := h.ProgramStatuses()
	if len(got) != 4 || got[2].App != "compiler" || got[3].App != "root" {
		t.Fatalf("inheritance: %+v", got)
	}
	writeVT(t, h, osc7501("state=clear:id=build"))
	got = h.ProgramStatuses()
	if len(got) != 2 || got[1].ID != "build2" {
		t.Fatalf("subtree clear: %+v", got)
	}

	writeVT(t, h, osc7501("state=working:id=w")+osc7501("state=blocked:id=b")+osc7501("state=error:id=e"))
	writeVT(t, h, "\x1b]133;A\x1b\\")
	got = h.ProgramStatuses()
	if len(got) != 3 || got[2].State != ansi.ProgramStateError {
		t.Fatalf("prompt: %+v", got)
	}

	writeVT(t, h, "\x1b[?1049h\x1b[?1049l\x1b[!p")
	if len(h.ProgramStatuses()) != 3 {
		t.Fatal("screen switch or soft reset cleared status")
	}
	writeVT(t, h, "\x1bc")
	if len(h.ProgramStatuses()) != 0 {
		t.Fatal("RIS retained status")
	}
	writeVT(t, h, osc7501("state=done:id=x")+osc7501("state=clear:msg=A"))
	if len(h.ProgramStatuses()) != 1 {
		t.Fatal("invalid clear removed records")
	}
	writeVT(t, h, osc7501("state=clear"))
	if len(h.ProgramStatuses()) != 0 {
		t.Fatal("root clear retained status")
	}
}

func TestProgramStatusBoundedEviction(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 1)
	var b strings.Builder
	for i := range programStatusMaxRecords {
		fmt.Fprintf(&b, osc7501("state=working:id=j%03d"), i)
	}
	b.WriteString(osc7501("state=done:id=j000"))
	b.WriteString(osc7501("state=idle:id=new"))
	writeVT(t, h, b.String())

	got := h.ProgramStatuses()
	if len(got) != programStatusMaxRecords {
		t.Fatalf("records = %d, want %d", len(got), programStatusMaxRecords)
	}
	for _, s := range got {
		if s.ID == "j001" {
			t.Fatal("least recently updated record survived")
		}
	}
	var updated ansi.ProgramStatus
	for _, s := range got {
		if s.ID == "j000" {
			updated = s
		}
	}
	if updated.State != ansi.ProgramStateDone {
		t.Fatalf("updated record = %+v", updated)
	}
}

func TestProgramStatusInSnapshot(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 1)
	writeVT(t, h, osc7501("state=working:id=job:progress=0:app=demo"))
	snap := h.emulator.snapshot(t)
	if len(snap.ProgramStatus) != 1 || snap.ProgramStatus[0].State != "working" {
		t.Fatalf("snapshot status = %+v", snap.ProgramStatus)
	}
	if snap.ProgramStatus[0].Progress == nil || *snap.ProgramStatus[0].Progress != 0 {
		t.Fatalf("progress = %v, want 0", snap.ProgramStatus[0].Progress)
	}
}
