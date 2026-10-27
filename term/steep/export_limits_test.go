// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel/sink"
)

// counterModel changes a status line and a progress bar on every key press: a
// small change that is never a duplicate frame.
type counterModel struct{ w, h, n int }

func (m counterModel) Init() tea.Cmd { return nil }

func (m counterModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		m.n++
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m counterModel) View() tea.View {
	lines := make([]string, max(m.h, 6))
	lines[1] = fmt.Sprintf("  step %d", m.n)
	lines[3] = "  [" + strings.Repeat("#", m.n%max(m.w-6, 1)) + "]"
	return tea.NewView(strings.Join(lines, "\n"))
}

// recordedFrames paints n distinct frames and returns how many the GIF kept.
func recordedFrames(t *testing.T, n int, opts ...Option) int {
	t.Helper()
	h := NewHarness(t, counterModel{}, append([]Option{
		WithWindowSize(80, 24),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
	}, opts...)...)
	for range n {
		h.Type("x")
	}
	return len(finishGIF(t, h, false).Image)
}

func TestExportKeepsEveryFrameByDefault(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MAX_FRAMES", "")
	t.Setenv("STEEP_EXPORT_MAX_BYTES", "")
	const n = 150
	if got := recordedFrames(t, n); got < n {
		t.Fatalf("got %d frames, want at least %d with no limit", got, n)
	}
}

func TestExportMaxFramesOption(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MAX_FRAMES", "")
	got := recordedFrames(t, 30, WithExport(ExportOn, ExportTypeGIF, sink.WithMaxFrames(10)))
	if got != 10 {
		t.Fatalf("got %d frames, want 10 from sink.WithMaxFrames", got)
	}
}

func TestExportMaxFramesEnv(t *testing.T) {
	opt := WithExport(ExportOn, ExportTypeGIF, sink.WithMaxFrames(5))
	tests := []struct {
		name, env string
		want      func(int) bool
	}{
		{"overrides option", "12", func(n int) bool { return n == 12 }},
		{"zero removes the limit", "0", func(n int) bool { return n > 20 }},
		{"negative is ignored", "-5", func(n int) bool { return n == 5 }},
		{"garbage is ignored", "lots", func(n int) bool { return n == 5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("STEEP_EXPORT_MAX_FRAMES", tt.env)
			t.Setenv("STEEP_EXPORT_MAX_BYTES", "")
			if got := recordedFrames(t, 30, opt); !tt.want(got) {
				t.Fatalf("STEEP_EXPORT_MAX_FRAMES=%q kept %d frames", tt.env, got)
			}
		})
	}
}

func TestExportMaxBytes(t *testing.T) {
	// An 80x24 canvas is 345600 bytes, so this holds the first frame and a
	// handful of changes, not all 40.
	const limit = 360 << 10
	t.Run("option", func(t *testing.T) {
		t.Setenv("STEEP_EXPORT_MAX_FRAMES", "")
		t.Setenv("STEEP_EXPORT_MAX_BYTES", "")
		got := recordedFrames(t, 40, WithExport(ExportOn, ExportTypeGIF, sink.WithMaxBytes(limit)))
		if got < 2 || got > 20 {
			t.Fatalf("got %d frames, want a handful within %d bytes", got, limit)
		}
	})
	t.Run("env", func(t *testing.T) {
		t.Setenv("STEEP_EXPORT_MAX_FRAMES", "")
		t.Setenv("STEEP_EXPORT_MAX_BYTES", "368640")
		got := recordedFrames(t, 40)
		if got < 2 || got > 20 {
			t.Fatalf("got %d frames, want a handful within %d bytes", got, limit)
		}
	})
	for _, env := range []string{"-1", "64m", "lots"} {
		t.Run("ignored "+env, func(t *testing.T) {
			t.Setenv("STEEP_EXPORT_MAX_FRAMES", "")
			t.Setenv("STEEP_EXPORT_MAX_BYTES", env)
			if got := recordedFrames(t, 40); got < 40 {
				t.Fatalf("STEEP_EXPORT_MAX_BYTES=%q kept %d frames, want all 40", env, got)
			}
		})
	}
}
