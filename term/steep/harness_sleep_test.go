// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"strconv"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type paintMsg struct{}

// paintOptions record a painter at the fastest frame rate Bubble Tea allows.
func paintOptions(t *testing.T) []Option {
	t.Helper()
	return gifOptions(t, WithProgramOptions(tea.WithFPS(120)))
}

// painter repaints every millisecond and shows "done" once it has painted n
// frames. Recorded with paintOptions, it fills the export queue between hooks in under a
// second.
type painter struct{ n, frame int }

func (p painter) paint() tea.Cmd {
	return tea.Tick(time.Millisecond, func(time.Time) tea.Msg { return paintMsg{} })
}

func (p painter) Init() tea.Cmd { return p.paint() }

func (p painter) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	if _, ok := msg.(paintMsg); ok {
		p.frame++
		return p, p.paint()
	}
	return p, nil
}

func (p painter) View() tea.View {
	if p.frame >= p.n {
		return tea.NewView("done")
	}
	return tea.NewView(strconv.Itoa(p.frame))
}

func TestHarnessSleepRecordsAsItGoes(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, painter{n: 1 << 30}, paintOptions(t)...)
	start := time.Now()
	h.Sleep(1500 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond {
		t.Fatalf("slept %v, want at least 1.5s", elapsed)
	}
	if h.export.droppedPending {
		t.Fatal("export dropped frames while sleeping")
	}
}

func TestHarnessLongWaitRecordsAsItGoes(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, painter{n: 1500}, paintOptions(t)...)
	h.Wait("done", WithTimeout(5*time.Second))
	if h.export.droppedPending {
		t.Fatal("export dropped frames during a long wait")
	}
}
