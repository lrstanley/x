// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
)

// withRender draws [Harness.Image] with opts.
func withRender(opts ...render.Option) Option {
	return WithRecording(reel.Profile{Render: opts})
}

// fakeTB is a [testing.TB] whose errors are recorded instead of failing the
// test, so a harness can run as a failing test inside a passing one.
type fakeTB struct {
	testing.TB

	mu     sync.Mutex
	failed bool
	errs   []string
}

func (f *fakeTB) Error(args ...any) { f.record(fmt.Sprintln(args...)) }

func (f *fakeTB) Errorf(format string, args ...any) { f.record(fmt.Sprintf(format, args...)) }

func (f *fakeTB) Fail() { f.record("") }

// FailNow marks the test failed, and stops the goroutine, so it must be
// called on a goroutine of the test's own.
func (f *fakeTB) FailNow() {
	f.record("")
	runtime.Goexit()
}

// Fatalf records the error, and stops the goroutine as FailNow does, so it
// must be called on a goroutine of the test's own.
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.record(fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (f *fakeTB) record(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = true
	if msg != "" {
		f.errs = append(f.errs, msg)
	}
}

func (f *fakeTB) Failed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed
}

func (f *fakeTB) errors() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.errs)
}

// exportOptions export a small terminal into dir, ignoring the environment,
// followed by opts.
func exportOptions(dir string, opts ...Option) []Option {
	return append([]Option{WithWindowSize(20, 4), WithArtifacts(dir), WithIgnoreEnv(true)}, opts...)
}

func readGIF(t *testing.T, path string) *gif.GIF {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// TestExportOnFailure checks the on failure policy: a passing test writes
// nothing, and a failing one writes the default outputs, whose .cast
// exported again with the same profile and result writes the same GIF.
func TestExportOnFailure(t *testing.T) {
	t.Parallel()
	profile := reel.Profile{Effects: []reel.Effect{
		effects.Keys(time.Second), effects.Pause(), Outro(500 * time.Millisecond),
	}}

	t.Run("passed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		h := NewHarness(t, rootTestModel{}, exportOptions(dir, WithRecording(profile), WithExportOnFailure())...)
		h.Type("ab").Wait("text=ab")
		h.Close()
		if names := dirNames(t, dir); len(names) != 0 {
			t.Fatalf("a passing test wrote %q", names)
		}
	})

	t.Run("failed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		ftb := &fakeTB{TB: t}
		h := NewHarness(ftb, rootTestModel{}, exportOptions(dir, WithRecording(profile), WithExportOnFailure())...)
		h.Type("ab").Wait("text=ab").Assert("text=missing")
		h.Close()

		name := "TestExportOnFailure_failed"
		want := []string{name + "_failed.cast", name + "_failed.gif"}
		if got := dirNames(t, dir); !slices.Equal(got, want) {
			t.Fatalf("files = %q, want %q (errors: %q)", got, want, ftb.errors())
		}
		if errs := ftb.errors(); len(errs) != 1 {
			t.Fatalf("errors = %q, want only the failed assertion", errs)
		}

		f, err := os.Open(filepath.Join(dir, want[0]))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		tl, err := reel.ReadTimeline(f)
		if err != nil {
			t.Fatal(err)
		}
		again := filepath.Join(t.TempDir(), "again.gif")
		p := profile
		p.Outputs = reel.Files(again)
		p.Meta = &Result{Name: t.Name(), Failed: true, Failures: h.failures.messages()}
		if err = reel.Export(t.Context(), tl, p); err != nil {
			t.Fatal(err)
		}
		first, err := os.ReadFile(filepath.Join(dir, want[1]))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(again)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("exporting the .cast again wrote a different GIF (%d bytes, want %d)", len(second), len(first))
		}
	})
}

// TestExportAlways checks the always policy, and that [Outro] makes a failed
// test's end pause at least 3s, before its card.
func TestExportAlways(t *testing.T) {
	t.Parallel()
	pauseDelay := func(t *testing.T, path string) time.Duration {
		t.Helper()
		g := readGIF(t, path)
		return time.Duration(g.Delay[len(g.Delay)-2]) * 10 * time.Millisecond
	}
	for _, tc := range []struct {
		name   string
		fail   bool
		pause  time.Duration
		status string
		want   time.Duration
	}{
		{"passed", false, time.Second, "passed", time.Second},
		{"failed", true, time.Second, "failed", failureHold},
		{"failed with a long pause", true, 5 * time.Second, "failed", 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			ftb := &fakeTB{TB: t}
			h := NewHarness(ftb, rootTestModel{}, exportOptions(dir,
				WithRecording(reel.Profile{
					Outputs: reel.Files("{name}_{status}.gif"),
					Effects: []reel.Effect{effects.Pause(effects.PauseAtEnd(tc.pause)), Outro(100 * time.Millisecond)},
				}),
				WithExportAlways(),
			)...)
			h.Wait("size=20x4")
			if tc.fail {
				h.Assert("missing")
			}
			h.Close()
			path := filepath.Join(dir, normalizeTestName(t.Name())+"_"+tc.status+".gif")
			if got := pauseDelay(t, path); got != tc.want {
				t.Fatalf("end pause plays %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExportMarks checks the hook marks: each occurrence of an event has a
// label of its own, and the timeline ends on the mark Close places as it
// starts.
func TestExportMarks(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(20, 4), WithIgnoreEnv(true))
	h.Wait("size=20x4").Type("a").Mark("mine").Type("b").Wait("text=ab")
	h.Close()
	var labels []string
	for m := range h.Timeline().Marks() {
		labels = append(labels, m.Label)
	}
	want := []string{"ready-1", "settle-1", "input-1", "mine", "input-2", "settle-2", "close-1"}
	if !slices.Equal(labels, want) {
		t.Fatalf("marks = %q, want %q", labels, want)
	}
}

// TestExportStills checks per-occurrence stills, written under their mark
// labels, at the events asked for.
func TestExportStills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewHarness(t, rootTestModel{}, exportOptions(dir,
		WithRecording(reel.Profile{Outputs: reel.Files("rec/{name}.cast")}),
		WithStills(StillPNG, HookInput),
		WithStills(StillSnapshot, HookInput, HookSettle),
		WithExportAlways(),
	)...)
	h.Type("a").Type("b").Wait("text=ab")
	h.Close()

	stem := "TestExportStills_passed_"
	want := []string{
		stem + "input-1.png", stem + "input-1.snap",
		stem + "input-2.png", stem + "input-2.snap",
		stem + "settle-1.snap",
		"rec",
	}
	slices.Sort(want)
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files = %q, want %q", got, want)
	}
	if got := dirNames(t, filepath.Join(dir, "rec")); !slices.Equal(got, []string{"TestExportStills.cast"}) {
		t.Fatalf("rec = %q", got)
	}
	// The mark of an input is placed once the program has painted it.
	for label, text := range map[string]string{"input-1": "text=a", "input-2": "text=ab"} {
		snap, err := os.ReadFile(filepath.Join(dir, stem+label+".snap"))
		if err != nil {
			t.Fatal(err)
		}
		if got := viewField(string(snap), "text="); "text="+got != text {
			t.Errorf("%s snapshot shows %q, want %q", label, got, text)
		}
	}
	a, errA := os.ReadFile(filepath.Join(dir, stem+"input-1.png"))
	b, errB := os.ReadFile(filepath.Join(dir, stem+"input-2.png"))
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if bytes.Equal(a, b) {
		t.Fatal("the stills of two inputs are the same image")
	}
}

// TestExportPaths checks where outputs go: relative paths under the
// artifacts directory, absolute ones as they are, and {name} of a subtest
// without its slashes.
func TestExportPaths(t *testing.T) {
	t.Parallel()
	t.Run("sub/test", func(t *testing.T) {
		t.Parallel()
		dir, abs := t.TempDir(), filepath.Join(t.TempDir(), "abs.cast")
		h := NewHarness(t, rootTestModel{}, exportOptions(dir,
			WithRecording(reel.Profile{Outputs: reel.Files("{name}.cast", abs)}),
			WithExportAlways(),
		)...)
		h.Wait("size=20x4")
		h.Close()
		if got := dirNames(t, dir); !slices.Equal(got, []string{"TestExportPaths_sub_test.cast"}) {
			t.Fatalf("files = %q", got)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("artifact dir", func(t *testing.T) {
		t.Parallel()
		h := NewHarness(t, rootTestModel{},
			WithWindowSize(20, 4), WithIgnoreEnv(true),
			WithRecording(reel.Profile{
				Outputs: reel.Files("{name}_{status}.cast"),
				Vars:    map[string]string{"name": "fixed", "status": "ok"},
			}),
			WithStills(StillPNG|StillSnapshot, HookReady),
			WithExportAlways(),
		)
		h.Wait("size=20x4")
		h.Close()
		want := []string{"fixed_ok.cast", "fixed_ok_ready-1.png", "fixed_ok_ready-1.snap"}
		if got := dirNames(t, t.ArtifactDir()); !slices.Equal(got, want) {
			t.Fatalf("files = %q", got)
		}
	})
}

// TestExportEnv checks the environment's policy and outputs: the default
// outputs when it names none.
func TestExportEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STEEP_EXPORT_MODE", "on")
	t.Setenv("STEEP_EXPORT_DIR", dir)
	t.Setenv("STEEP_EXPORT_TYPE", "")
	h := NewHarness(t, rootTestModel{}, WithWindowSize(20, 4))
	h.Wait("size=20x4")
	h.Close()
	want := []string{"TestExportEnv_passed.cast", "TestExportEnv_passed.gif"}
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files = %q, want %q", got, want)
	}

	dir = t.TempDir()
	t.Setenv("STEEP_EXPORT_DIR", dir)
	t.Setenv("STEEP_EXPORT_TYPE", "snapshot")
	h = NewHarness(t, rootTestModel{}, WithWindowSize(20, 4),
		WithRecording(reel.Profile{Outputs: reel.Files("ignored.gif")}))
	h.Type("a")
	h.Close()
	want = []string{"TestExportEnv_passed_close-1.snap", "TestExportEnv_passed_input-1.snap", "TestExportEnv_passed_ready-1.snap"}
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files = %q, want %q", got, want)
	}
}

// TestExportSkipped checks that a skipped test exports nothing.
func TestExportSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Parallel subtests finish before the parent's cleanups run.
	t.Cleanup(func() {
		if names := dirNames(t, dir); len(names) != 0 {
			t.Errorf("a skipped test wrote %q", names)
		}
	})
	t.Run("skipped", func(t *testing.T) {
		t.Parallel()
		h := NewHarness(t, rootTestModel{}, exportOptions(dir, WithExportAlways())...)
		h.Wait("size=20x4")
		t.Skip("skipped after the harness ran")
	})
}

// slowModel blocks in Update when it is sent a "z", so it cannot quit.
type slowModel struct{ rootTestModel }

func (m slowModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.Key().Text == "z" {
		time.Sleep(time.Second)
	}
	next, cmd := m.rootTestModel.Update(msg)
	m.rootTestModel, _ = next.(rootTestModel)
	return m, cmd
}

// TestExportWhenStopFails checks that the recording is exported when the
// program does not stop in time, which fails the test from Close.
func TestExportWhenStopFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, slowModel{}, exportOptions(dir,
		WithTimeout(200*time.Millisecond),
		WithRecording(reel.Profile{Outputs: reel.Files("rec.cast")}),
		WithExportOnFailure(),
	)...)
	h.Wait("size=20x4").Type("z")
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Close()
	}()
	<-done
	if errs := ftb.errors(); len(errs) == 0 || !strings.Contains(errs[0], "timeout") {
		t.Fatalf("errors = %q, want a timeout stopping the program", errs)
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{"rec.cast"}) {
		t.Fatalf("files = %q, want the recording", got)
	}
}

// TestExportError checks that a failed export fails the test.
func TestExportError(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, exportOptions(t.TempDir(),
		WithRecording(reel.Profile{
			Outputs: reel.Files("rec.mp4"),
			Encode:  []sink.Option{sink.WithFFmpeg(filepath.Join(t.TempDir(), "no-ffmpeg"))},
		}),
		WithExportAlways(),
	)...)
	h.Close()
	if errs := ftb.errors(); len(errs) != 1 || !strings.Contains(errs[0], "steep: export:") {
		t.Fatalf("errors = %q, want the export's", errs)
	}
}

// altScreenModel is rootTestModel on the alternate screen, which a program
// leaves (revealing a blank primary screen) as it exits.
type altScreenModel struct{ rootTestModel }

func (m altScreenModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	next, cmd := m.rootTestModel.Update(msg)
	m.rootTestModel, _ = next.(rootTestModel)
	return m, cmd
}

func (m altScreenModel) View() tea.View {
	v := m.rootTestModel.View()
	v.AltScreen = true
	return v
}

// TestExportOmitsProgramExitRepaint checks that the recording ends on the
// last screen the test saw, not the blank one the program leaves as it
// exits.
func TestExportOmitsProgramExitRepaint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewHarness(t, altScreenModel{}, exportOptions(dir,
		WithWindowSize(20, 2),
		WithRecording(reel.Profile{Outputs: reel.Files("rec.gif")}),
		WithExportAlways(),
	)...)
	h.Type("abc").Wait("text=abc")
	h.Close()
	g := readGIF(t, filepath.Join(dir, "rec.gif"))
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for _, frame := range g.Image {
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
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

// TestExportVideo records a GIF and a video of the same frames.
func TestExportVideo(t *testing.T) {
	t.Parallel()
	if err := sink.CheckVideo(sink.FormatMP4); err != nil {
		t.Skipf("cannot encode mp4: %v", err)
	}
	dir := t.TempDir()
	h := NewHarness(t, rootTestModel{}, exportOptions(dir,
		WithRecording(reel.Profile{Outputs: reel.Files("rec.gif", "rec.mp4")}),
		WithExportAlways(),
	)...)
	h.Type("ab")
	h.Close()
	for _, name := range []string{"rec.gif", "rec.mp4"} {
		if fi, err := os.Stat(filepath.Join(dir, name)); err != nil || fi.Size() == 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// TestExportOnPanic checks that a test that panics still exports, from the
// cleanup the harness registers. The panic runs in a child process.
func TestExportOnPanic(t *testing.T) {
	if dir := os.Getenv("STEEP_TEST_PANIC_DIR"); dir != "" {
		h := NewHarness(t, rootTestModel{}, exportOptions(dir, WithExportOnFailure())...)
		h.Type("a")
		panic("boom")
	}
	t.Parallel()
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExportOnPanic$", "-test.count=1")
	cmd.Env = append(os.Environ(), "STEEP_TEST_PANIC_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "boom") {
		t.Fatalf("child did not panic: %v\n%s", err, out)
	}
	want := []string{"TestExportOnPanic_failed.cast", "TestExportOnPanic_failed.gif"}
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files = %q, want %q\n%s", got, want, out)
	}

	// The recording ends on the result, with the stack of the panic.
	f, err := os.Open(filepath.Join(dir, want[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(f)
	if err != nil {
		t.Fatal(err)
	}
	got := notes(t, tl)
	n := findNote(t, got[len(got)-1:], kindResult, map[string]any{"failed": true, "panicked": true})
	if stack, _ := n.fields["stack"].(string); !strings.HasPrefix(stack, "panic(") || !strings.Contains(stack, "TestExportOnPanic") {
		t.Fatalf("stack = %q", stack)
	}
}

func viewField(view, prefix string) string {
	for line := range strings.SplitSeq(view, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimRight(rest, " ")
		}
	}
	return ""
}
