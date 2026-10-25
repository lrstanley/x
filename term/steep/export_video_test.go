// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lrstanley/x/term/still/export"
)

// requireVideo skips the test unless ffmpeg can encode format.
func requireVideo(t *testing.T, format export.VideoFormat) {
	t.Helper()
	if err := export.CheckVideo(format); err != nil {
		t.Skipf("cannot encode %s: %v", format, err)
	}
}

// assertVideoFile checks that path is a playable video. ffmpeg cannot decode
// animated WebP, so for that only the RIFF/WEBP container header is checked.
func assertVideoFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatalf("%s is empty", path)
	}
	if filepath.Ext(path) == ".webp" {
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			t.Fatalf("%s is not a RIFF/WEBP file", path)
		}
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", path, "-f", "null", "-")
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("ffmpeg could not decode %s: %v\n%s", path, err, stderr.String())
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	slices.Sort(names)
	return names
}

// finishExport quits the program and finalizes export.
func finishExport(t *testing.T, h *Harness, failed bool) {
	t.Helper()
	h.QuitProgram()
	h.WaitFinished()
	if err := h.export.Finalize(failed); err != nil {
		t.Fatal(err)
	}
}

func videoHarness(t *testing.T, dir string, mode ExportMode, types ExportType, opts ...Option) *Harness {
	t.Helper()
	return NewHarness(t, rootTestModel{}, append([]Option{
		WithWindowSize(40, 4),
		WithExport(mode, types),
		WithExportDir(dir),
		WithExportName("rec"),
		WithIgnoreEnv(true),
	}, opts...)...)
}

func TestParseExportTypesVideo(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]ExportType{
		"mp4":           ExportTypeMP4,
		"WebM":          ExportTypeWebM,
		" WEBP ":        ExportTypeWebP,
		"gif,mp4, webm": ExportTypeGIF | ExportTypeMP4 | ExportTypeWebM,
	} {
		if got := parseExportTypes(in); got != want {
			t.Errorf("parseExportTypes(%q) = %v, want %v", in, got, want)
		}
	}
	if ExportTypeAll.has(ExportTypeMP4) || ExportTypeAll.has(ExportTypeWebM) || ExportTypeAll.has(ExportTypeWebP) {
		t.Fatal("ExportTypeAll includes a video type, which need ffmpeg")
	}
	if got := parseExportTypes("all"); got != ExportTypeAll {
		t.Fatalf("parseExportTypes(all) = %v, want ExportTypeAll", got)
	}
	if got := parseExportTypes("mp4,all"); got != ExportTypeAll {
		t.Fatalf("parseExportTypes(mp4,all) = %v, want ExportTypeAll", got)
	}
}

func TestApplyEnvExportQuality(t *testing.T) {
	for env, want := range map[string]int{"": 0, "50": 1, "0": 1, "100": 1, "101": 0, "-1": 0, "high": 0} {
		t.Run("quality="+env, func(t *testing.T) {
			t.Setenv("STEEP_EXPORT_QUALITY", env)
			if got := len(collectOptions(t).exportOpts); got != want {
				t.Fatalf("STEEP_EXPORT_QUALITY=%q added %d options, want %d", env, got, want)
			}
		})
	}
}

func TestDoneAfter(t *testing.T) {
	t.Parallel()
	t.Run("single output", func(t *testing.T) {
		t.Parallel()
		var n int
		doneAfter(func() { n++ }, 1)()
		if n != 1 {
			t.Fatalf("done ran %d times, want 1", n)
		}
	})
	t.Run("after the last output", func(t *testing.T) {
		t.Parallel()
		var n atomic.Int32
		done := doneAfter(func() { n.Add(1) }, 3)
		for i := range 2 {
			done()
			if n.Load() != 0 {
				t.Fatalf("done ran after %d of 3 outputs", i+1)
			}
		}
		done()
		if n.Load() != 1 {
			t.Fatalf("done ran %d times, want 1", n.Load())
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		t.Parallel()
		const outputs = 4
		var n atomic.Int32
		done := doneAfter(func() { n.Add(1) }, outputs)
		var wg sync.WaitGroup
		for range outputs {
			wg.Go(done)
		}
		wg.Wait()
		if n.Load() != 1 {
			t.Fatalf("done ran %d times, want 1", n.Load())
		}
	})
}

// TestExportPipelineFansOut checks that every output gets every frame, each
// with a Done to recycle its image ([doneAfter] makes it fire once).
func TestExportPipelineFansOut(t *testing.T) {
	t.Parallel()
	var chs []chan export.Frame
	var outs []chan<- export.Frame
	for range 3 {
		ch := make(chan export.Frame, 4)
		chs = append(chs, ch)
		outs = append(outs, ch)
	}
	p := exportPipeline{base: ExportContext{Step: 50 * time.Millisecond}, speed: 1, out: outs}
	start := time.Now()
	for i := range 2 {
		p.record(labelImage(uint8(i+1)), start.Add(time.Duration(i)*time.Second), frameSource{})
	}
	p.finish(start.Add(2*time.Second), false)

	for _, ch := range chs {
		if len(ch) != 2 {
			t.Fatalf("output got %d frames, want 2", len(ch))
		}
	}
	for want := uint8(1); want <= 2; want++ {
		var got []export.Frame
		for _, ch := range chs {
			f := <-ch
			if imageLabel(f.Image) != want {
				t.Fatalf("output got frame %d, want %d", imageLabel(f.Image), want)
			}
			if f.Done == nil {
				t.Fatalf("frame %d has no Done, want one to recycle its image", want)
			}
			got = append(got, f)
		}
		for _, f := range got {
			f.Done()
		}
	}
}

func TestExportVideoMissingFFmpeg(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tb := &captureFatalTB{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil && r != fatalSentinel {
				panic(r)
			}
		}()
		NewHarness(tb, rootTestModel{},
			WithExport(ExportOn, ExportTypeGIF|ExportTypeMP4, export.WithFFmpeg(filepath.Join(t.TempDir(), "no-ffmpeg"))),
			WithExportDir(dir),
			WithIgnoreEnv(true),
		)
	}()
	if !errors.Is(tb.err, export.ErrFFmpegNotFound) {
		t.Fatalf("harness error = %v, want one wrapping export.ErrFFmpegNotFound", tb.err)
	}
	for _, want := range []string{"mp4", "WithExport", "STEEP_EXPORT_TYPE"} {
		if !strings.Contains(tb.err.Error(), want) {
			t.Errorf("error %q does not mention %q", tb.err, want)
		}
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("a failed harness left files behind: %v", names)
	}
}

func TestExportTypeAllWithoutFFmpeg(t *testing.T) {
	t.Setenv("STEEP_EXPORT_TYPE", "all")
	t.Setenv("STEEP_EXPORT_MODE", "")
	dir := t.TempDir()
	h := NewHarness(t, rootTestModel{},
		WithWindowSize(40, 4),
		WithExport(ExportOn, ExportTypeGIF, export.WithFFmpeg(filepath.Join(t.TempDir(), "no-ffmpeg"))),
		WithExportDir(dir),
		WithExportName("rec"),
	)
	h.Type("a")
	finishExport(t, h, false)
	if _, err := os.Stat(filepath.Join(dir, "rec.gif")); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{"mp4", "webm", "webp"} {
		if _, err := os.Stat(filepath.Join(dir, "rec."+ext)); err == nil {
			t.Errorf("rec.%s was written, want video types left out of all", ext)
		}
	}
}

func TestExportGIFAndVideo(t *testing.T) {
	requireVideo(t, export.VideoMP4)
	dir := t.TempDir()
	h := videoHarness(t, dir, ExportOn, ExportTypeGIF|ExportTypeMP4)
	h.Type("a").Type("b").Type("c")
	finishExport(t, h, false)

	g := filepath.Join(dir, "rec.gif")
	if _, err := os.Stat(g); err != nil {
		t.Fatal(err)
	}
	assertVideoFile(t, filepath.Join(dir, "rec.mp4"))
	if got := dirNames(t, dir); !slices.Equal(got, []string{"rec.gif", "rec.mp4"}) {
		t.Fatalf("files = %v, want the gif and the mp4 only", got)
	}
}

func TestExportEveryVideoFormat(t *testing.T) {
	for _, v := range []struct {
		typ    ExportType
		format export.VideoFormat
	}{
		{ExportTypeMP4, export.VideoMP4},
		{ExportTypeWebM, export.VideoWebM},
		{ExportTypeWebP, export.VideoWebP},
	} {
		t.Run(string(v.format), func(t *testing.T) {
			requireVideo(t, v.format)
			dir := t.TempDir()
			h := videoHarness(t, dir, ExportOn, v.typ)
			h.Type("a").Type("b")
			finishExport(t, h, false)
			assertVideoFile(t, filepath.Join(dir, "rec."+string(v.format)))
			if got := dirNames(t, dir); len(got) != 1 {
				t.Fatalf("files = %v, want only the video", got)
			}
		})
	}
}

// TestExportVideoOnly checks that a recording without a GIF still gets
// commit-driven frames and the key overlay.
func TestExportVideoOnly(t *testing.T) {
	requireVideo(t, export.VideoMP4)
	dir := t.TempDir()
	h := videoHarness(t, dir, ExportOn, ExportTypeMP4, WithExportKeys(time.Second))
	if h.export.keys == nil {
		t.Fatal("a video-only recording has no key overlay")
	}
	h.Type("a").Type("b").Type("c")
	rec := h.export.outputs[0].rec
	finishExport(t, h, false)
	if n := rec.Frames(); n < 3 {
		t.Fatalf("video has %d frames, want at least one per key press", n)
	}
	assertVideoFile(t, filepath.Join(dir, "rec.mp4"))
	if got := dirNames(t, dir); !slices.Equal(got, []string{"rec.mp4"}) {
		t.Fatalf("files = %v, want only the mp4", got)
	}
}

func TestExportVideoOnFailure(t *testing.T) {
	requireVideo(t, export.VideoMP4)
	t.Run("passing", func(t *testing.T) {
		dir := t.TempDir()
		h := videoHarness(t, dir, ExportOnFailure, ExportTypeGIF|ExportTypeMP4)
		h.Type("a").Type("b")
		finishExport(t, h, false)
		if names := dirNames(t, dir); len(names) != 0 {
			t.Fatalf("a passing test left %v, want no files, temporary ones included", names)
		}
	})
	t.Run("failed", func(t *testing.T) {
		dir := t.TempDir()
		h := videoHarness(t, dir, ExportOnFailure, ExportTypeGIF|ExportTypeMP4)
		h.Type("a").Type("b")
		finishExport(t, h, true)
		assertVideoFile(t, filepath.Join(dir, "rec_failed.mp4"))
		if got := dirNames(t, dir); !slices.Equal(got, []string{"rec_failed.gif", "rec_failed.mp4"}) {
			t.Fatalf("files = %v, want the failed gif and mp4", got)
		}
	})
}

func TestExportVideoOnCopiesFailed(t *testing.T) {
	requireVideo(t, export.VideoMP4)
	dir := t.TempDir()
	h := videoHarness(t, dir, ExportOn, ExportTypeMP4)
	h.Type("a").Type("b")
	finishExport(t, h, true)
	assertVideoFile(t, filepath.Join(dir, "rec.mp4"))
	assertVideoFile(t, filepath.Join(dir, "rec_failed.mp4"))
	a, _ := os.ReadFile(filepath.Join(dir, "rec.mp4"))
	b, _ := os.ReadFile(filepath.Join(dir, "rec_failed.mp4"))
	if !bytes.Equal(a, b) {
		t.Fatal("rec_failed.mp4 is not a copy of rec.mp4")
	}
}
