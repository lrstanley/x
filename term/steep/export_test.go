// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/still/export"
)

func TestExportOnFailureWritesArtifacts(t *testing.T) {
	dir := t.ArtifactDir()
	h := NewHarness(t, rootTestModel{},
		WithExport(ExportOnFailure, ExportTypePNG|ExportTypeSnapshot),
		WithExportDir(dir),
		WithIgnoreEnv(true),
	)
	h.WaitSettle().Type("hi").WaitSettle()
	if h.export == nil {
		t.Fatal("expected export manager")
	}
	if h.export.lastPNG == nil {
		t.Fatal("expected buffered png before finalize")
	}
	if err := h.export.Finalize(true); err != nil {
		t.Fatal(err)
	}

	var foundPNG, foundSnap bool
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		base := filepath.Base(path)
		if strings.Contains(base, "_failed.png") {
			foundPNG = true
		}
		if strings.Contains(base, "_failed.snap") {
			foundSnap = true
		}
		return nil
	})
	if !foundPNG || !foundSnap {
		entries, _ := os.ReadDir(dir)
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("artifacts: png=%v snap=%v in %s (files: %v)", foundPNG, foundSnap, dir, names)
	}
}

func TestExportGIFCapturesKeysWithoutSlow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var got []string
	var h *Harness
	h = NewHarness(t, rootTestModel{},
		WithWindowSize(80, 2),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(dir),
		WithIgnoreEnv(true),
		WithHooks(func(event HookEvent, stage HookStage, _ any) error {
			if event == HookInput && stage == HookStageAfter {
				got = append(got, viewField(h.View(), "text="))
			}
			return nil
		}),
	)
	h.Type("a").Type("b").Type("c")
	want := []string{"a", "ab", "abc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("views after each Type = %q, want %q", got, want)
	}
	h.QuitProgram()
	h.WaitFinished()
	if err := h.export.Finalize(false); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(h.export.gifPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	distinct := make(map[string]struct{}, len(g.Image))
	for _, img := range g.Image {
		distinct[string(img.Pix)] = struct{}{}
	}
	if len(distinct) < len(want) {
		t.Fatalf("distinct gif frames = %d, want at least %d", len(distinct), len(want))
	}
}

func TestExportGIFDelaysFollowRealTimeOnFPSGrid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewHarness(t, rootTestModel{},
		WithWindowSize(80, 2),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(dir),
		WithExportFPS(10),
		WithIgnoreEnv(true),
	)
	h.Type("a")
	time.Sleep(400 * time.Millisecond)
	h.Type("b")
	h.QuitProgram()
	h.WaitFinished()
	if err := h.export.Finalize(false); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(h.export.gifPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	var longest int
	for i, d := range g.Delay {
		if d%10 != 0 {
			t.Fatalf("frame %d delay = %d cs, want a multiple of the 10 FPS grid (10 cs)", i, d)
		}
		longest = max(longest, d)
	}
	if longest < 40 {
		t.Fatalf("longest frame delay = %d cs, want >= 40 cs for the 400ms the screen was held", longest)
	}
}

func TestExportEmitHeldDelay(t *testing.T) {
	t.Parallel()
	start := time.Now()
	cases := []struct {
		name    string
		speed   float64
		elapsed time.Duration
		want    time.Duration
	}{
		{"real time", 1, 300 * time.Millisecond, 300 * time.Millisecond},
		{"double speed", 2, 400 * time.Millisecond, 200 * time.Millisecond},
		{"half speed", 0.5, 300 * time.Millisecond, 600 * time.Millisecond},
		{"floored to one step", 1, time.Millisecond, 50 * time.Millisecond},
		{"out of order floored", 1, -time.Second, 50 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &exportManager{
				gifCh:  make(chan export.Frame, 1),
				step:   50 * time.Millisecond,
				speed:  tc.speed,
				held:   &export.Frame{},
				heldAt: start,
			}
			m.emitHeldLocked(start.Add(tc.elapsed))
			if m.held != nil {
				t.Fatal("held frame should be cleared after emit")
			}
			if got := (<-m.gifCh).Delay; got != tc.want {
				t.Fatalf("delay = %v, want %v", got, tc.want)
			}
		})
	}
}

// altScreenModel is rootTestModel on the alternate screen, which a program
// leaves (revealing a blank primary screen) as it exits.
type altScreenModel struct{ rootTestModel }

func (m altScreenModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	next, cmd := m.rootTestModel.Update(msg)
	m.rootTestModel = next.(rootTestModel)
	return m, cmd
}

func (m altScreenModel) View() tea.View {
	v := m.rootTestModel.View()
	v.AltScreen = true
	return v
}

func TestExportGIFOmitsProgramExitRepaint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewHarness(t, altScreenModel{},
		WithWindowSize(20, 2),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(dir),
		WithIgnoreEnv(true),
	)
	h.Type("abc")
	h.QuitProgram()
	h.WaitFinished()
	if err := h.export.Finalize(false); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(h.export.gifPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for _, frame := range g.Image {
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Src)
	}
	var lit int
	for i := 0; i < len(canvas.Pix); i += 4 {
		if int(canvas.Pix[i])+int(canvas.Pix[i+1])+int(canvas.Pix[i+2]) > 0 {
			lit++
		}
	}
	if lit < 50 {
		t.Fatalf("last gif frame has %d lit pixels, want the final screen and not the blank exit repaint", lit)
	}
}

func viewField(view, prefix string) string {
	for _, line := range strings.Split(view, "\n") {
		rest, ok := strings.CutPrefix(line, prefix)
		if ok {
			return strings.TrimRight(rest, " ")
		}
	}
	return ""
}

func TestExportFilenameOverwrite(t *testing.T) {
	m := &exportManager{
		tb:   t,
		dir:  t.TempDir(),
		mode: ExportOn,
	}
	name1 := m.artifactName(HookInput.String(), ".png", false)
	time.Sleep(2 * time.Millisecond)
	name2 := m.artifactName(HookInput.String(), ".png", false)
	if name1 == name2 {
		t.Fatalf("expected distinct timestamps, got duplicate %q", name1)
	}
	if !strings.HasSuffix(name1, ".png") {
		t.Fatalf("bad suffix: %q", name1)
	}
}
