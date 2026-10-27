// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestFormatOf(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]Format{
		"a.gif": FormatGIF, "dir/b.PNG": FormatPNG, "c.mp4": FormatMP4,
		"d.WebM": FormatWebM, "e.webp": FormatWebP,
	} {
		got, err := FormatOf(path)
		if err != nil || got != want {
			t.Errorf("FormatOf(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
	for _, path := range []string{"a.cast", "b", "c.jpg", "gif"} {
		if _, err := FormatOf(path); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("FormatOf(%q) error = %v, want ErrUnknownFormat", path, err)
		}
	}
}

func TestFilePicksSink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		ext   string
		video bool
		check func(Sink) bool
	}{
		{".gif", false, func(s Sink) bool { _, ok := s.(*GIFEncoder); return ok }},
		{".png", false, func(s Sink) bool { _, ok := s.(*pngSink); return ok }},
		{".mp4", true, func(s Sink) bool { v, ok := s.(*VideoEncoder); return ok && v.format == FormatMP4 }},
		{".webm", true, func(s Sink) bool { v, ok := s.(*VideoEncoder); return ok && v.format == FormatWebM }},
		{".webp", true, func(s Sink) bool { v, ok := s.(*VideoEncoder); return ok && v.format == FormatWebP }},
	} {
		path := filepath.Join(dir, "out"+tc.ext)
		if tc.video {
			if CheckVideo(Format(tc.ext[1:])) != nil {
				t.Logf("skipping %s: no ffmpeg encoder", tc.ext)
				continue
			}
		}
		s, err := File(path)
		if err != nil {
			t.Fatalf("File(%q): %v", path, err)
		}
		if !tc.check(s) {
			t.Errorf("File(%q) = %T, want the %s sink", path, s, tc.ext)
		}
		if err = s.Discard(); err != nil {
			t.Fatal(err)
		}
	}
	for _, ext := range []string{".cast", ".jpg", ""} {
		if _, err := File(filepath.Join(dir, "out"+ext)); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("File(%q) error = %v, want ErrUnknownFormat", ext, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("discarded sinks left %d files", len(entries))
	}
}

func TestPNGSinkWritesLastFrame(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "last.png")
	s, err := File(path, WithOptimize(OptimizeNone))
	if err != nil {
		t.Fatal(err)
	}
	last := solidNRGBA(3, 2, color.NRGBA{R: 9, G: 8, B: 7, A: 0xff})
	for _, img := range []image.Image{solidNRGBA(3, 2, color.NRGBA{A: 0xff}), last} {
		if err = s.WriteFrame(Frame{Image: img, Delay: time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	clear(last.Pix) // The sink keeps its own copy.
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(1, 1)); got != (color.NRGBA{R: 9, G: 8, B: 7, A: 0xff}) {
		t.Errorf("pixel = %v, want the last frame's color", got)
	}
	assertOnlyFile(t, path)

	// Without frames nothing is written.
	empty := filepath.Join(t.TempDir(), "empty.png")
	s, err = File(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(empty)); len(entries) != 0 {
		t.Errorf("an empty png sink left %d files", len(entries))
	}
}

func TestWriter(t *testing.T) {
	t.Parallel()
	frames := []Frame{
		{Image: solidFrame(1, 4, 4), Delay: 100 * time.Millisecond},
		{Image: solidFrame(2, 4, 4), Delay: 200 * time.Millisecond},
	}
	write := func(t *testing.T, format Format) []byte {
		t.Helper()
		var buf bytes.Buffer
		s, err := Writer(&buf, format)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range frames {
			if err = s.WriteFrame(f); err != nil {
				t.Fatal(err)
			}
			if buf.Len() != 0 {
				t.Fatalf("%s: wrote to w before Close", format)
			}
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	g, err := gif.DecodeAll(bytes.NewReader(write(t, FormatGIF)))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.Delay, []int{10, 20}) {
		t.Errorf("gif delays = %v, want [10 20]", g.Delay)
	}
	if _, err = png.Decode(bytes.NewReader(write(t, FormatPNG))); err != nil {
		t.Errorf("png: %v", err)
	}
	for _, format := range videoFormats {
		if CheckVideo(format) != nil {
			continue
		}
		data := write(t, format)
		if len(data) == 0 {
			t.Errorf("%s: wrote nothing", format)
			continue
		}
		if format == FormatWebP && (string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP") {
			t.Errorf("webp: not a WebP file")
		}
	}
	if _, err = Writer(&bytes.Buffer{}, "cast"); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("Writer(cast) error = %v, want ErrUnknownFormat", err)
	}
}

// TestWriterMP4Plays checks the MP4 Writer produces the same timeline a file
// does, since it is encoded to a seekable temporary file first.
func TestWriterMP4Plays(t *testing.T) {
	t.Parallel()
	info := requireVideo(t, FormatMP4)
	var buf bytes.Buffer
	s, err := Writer(&buf, FormatMP4)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range videoTestColors {
		if err = s.WriteFrame(Frame{Image: solidNRGBA(64, 32, c), Delay: videoTestDelays[i]}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "out.mp4")
	if err = os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	verifyVideo(t, info, path)
}

func TestChan(t *testing.T) {
	t.Parallel()
	ch := make(chan Frame, 2)
	s := Chan(ch)
	src := solidFrame(1, 2, 2)
	if err := s.WriteFrame(Frame{Image: src, Delay: time.Second, Isolated: true}); err != nil {
		t.Fatal(err)
	}
	clear(src.Pix)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFrame(Frame{Image: src, Delay: time.Second}); err == nil {
		t.Fatal("WriteFrame after Close succeeded")
	}
	var got []Frame
	for f := range ch {
		got = append(got, f)
	}
	if len(got) != 1 || got[0].Delay != time.Second || !got[0].Isolated {
		t.Fatalf("frames = %+v, want the one frame sent", got)
	}
	if img, ok := got[0].Image.(*image.NRGBA); !ok || slices.Equal(img.Pix, src.Pix) {
		t.Error("the frame sent does not own a copy of the image")
	}
}

// failSink fails every write.
type failSink struct{ collectEncoder }

func (s *failSink) WriteFrame(Frame) error { return errEncoderFull }

func TestTee(t *testing.T) {
	t.Parallel()
	a, b, bad := &collectEncoder{}, &collectEncoder{}, &failSink{}
	s := Tee(a, bad, b)
	if err := s.WriteFrame(Frame{Image: solidFrame(1, 2, 2), Delay: time.Second}); !errors.Is(err, errEncoderFull) {
		t.Fatalf("WriteFrame = %v, want the failing sink's error", err)
	}
	if len(a.frames) != 1 || len(b.frames) != 1 {
		t.Fatalf("frames = %d, %d; want every sink written", len(a.frames), len(b.frames))
	}
	if err := s.Discard(); err != nil || !a.discarded || !b.discarded || !bad.discarded {
		t.Fatal("Discard did not discard every sink")
	}
	if err := Tee(a, b).Close(); err != nil || !a.closed || !b.closed {
		t.Fatal("Close did not close every sink")
	}
}
