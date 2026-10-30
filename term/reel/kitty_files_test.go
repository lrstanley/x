// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// kittyPNG returns a 2x1 red PNG.
func kittyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, kittyRed)
	img.SetNRGBA(1, 0, kittyRed)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fileCmd returns a Kitty transmit-and-place command with keys ctrl whose
// payload is path, as the file media send it.
func fileCmd(ctrl, path string) string {
	return "\x1b_Ga=T,i=1,q=2," + ctrl + ";" + base64.StdEncoding.EncodeToString([]byte(path)) + "\x1b\\"
}

// writeKittySession writes each of chunks to a session opened with opts,
// closes it, and returns it.
func writeKittySession(t *testing.T, chunks []string, opts ...SessionOption) *Session {
	t.Helper()
	s, err := Open(t.Context(), 10, 2, opts...)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if _, err = s.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// timelineOutput is everything the program wrote, as recorded.
func timelineOutput(tl *Timeline) string {
	var b strings.Builder
	for _, ev := range tl.events {
		if o, ok := ev.(Output); ok {
			b.Write(o.Data)
		}
	}
	return b.String()
}

// TestKittyFiles checks what WithKittyFiles reads, and that the image it
// reads reaches the terminal and the timeline in the output.
func TestKittyFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	pngData := kittyPNG(t)
	pngPath := write("img.png", pngData)
	// A pixel after four bytes that are not.
	rawPath := write("img.rgba", []byte{9, 9, 9, 9, kittyRed.R, kittyRed.G, kittyRed.B, kittyRed.A})
	textPath := write("notes.txt", []byte("not an image"))

	tests := []struct {
		name  string
		cmd   string
		shown bool
	}{
		{"png file", fileCmd("t=f,f=100", pngPath), true},
		{"raw pixels at an offset", fileCmd("t=f,f=32,s=1,v=1,O=4,S=4", rawPath), true},
		{"a file that is not an image", fileCmd("t=f,f=100", textPath), false},
		{"pixels of the wrong size", fileCmd("t=f,f=32,s=2,v=2", rawPath), false},
		{"a file under /proc", fileCmd("t=f,f=100", "/proc/self/environ"), false},
		{"a missing file", fileCmd("t=f,f=100", filepath.Join(dir, "missing")), false},
		{"a temporary file outside the temporary directory", fileCmd("t=t,f=100", pngPath), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := writeKittySession(t, []string{tc.cmd}, WithKittyFiles())
			defer s.Close()
			shown := len(s.Terminal().Images()) == 1
			if shown != tc.shown {
				t.Fatalf("image shown = %v, want %v", shown, tc.shown)
			}
			out := timelineOutput(s.Timeline())
			if tc.shown && strings.Contains(out, "t=f") {
				t.Errorf("the timeline still sends the image by file: %q", out)
			}
			if !tc.shown && out != tc.cmd {
				t.Errorf("a refused command was changed to %q", out)
			}
		})
	}

	// Without the option, files are refused.
	s := writeKittySession(t, []string{fileCmd("t=f,f=100", pngPath)})
	t.Cleanup(func() { _ = s.Close() })
	if imgs := s.Terminal().Images(); len(imgs) != 0 {
		t.Errorf("a file was read without WithKittyFiles: %+v", imgs)
	}
	if _, err := os.Stat(pngPath); err != nil {
		t.Errorf("a file transmission removed the file: %v", err)
	}
}

// TestKittyFilesRemoved checks that temporary files and shared memory
// objects are deleted once read.
func TestKittyFilesRemoved(t *testing.T) {
	t.Parallel()
	pngData := kittyPNG(t)
	tmp, err := os.CreateTemp(t.TempDir(), "reel-tty-graphics-protocol-*.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tmp.Write(pngData)
	_ = tmp.Close()
	paths := map[string]string{"t=t,f=100": tmp.Name()}
	if runtime.GOOS == "linux" {
		shm := "/dev/shm/reel-test-" + filepath.Base(t.TempDir())
		if err = os.WriteFile(shm, pngData, 0o600); err == nil {
			paths["t=s,f=100"] = shm
		}
	}
	for ctrl, path := range paths {
		name := path
		if strings.HasPrefix(ctrl, "t=s") {
			name = strings.TrimPrefix(path, "/dev/shm")
		}
		s := writeKittySession(t, []string{fileCmd(ctrl, name)}, WithKittyFiles())
		if imgs := s.Terminal().Images(); len(imgs) != 1 {
			t.Errorf("%s: got %d placements, want 1", ctrl, len(imgs))
		}
		_ = s.Close()
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			_ = os.Remove(path)
			t.Errorf("%s: %s was not removed: %v", ctrl, path, err)
		}
	}
}

// TestKittyFilesSplit checks that a file transmission split across writes,
// down to one byte each, is still rewritten, that the output around it is
// passed on, and that the timeline replays to the same image.
func TestKittyFilesSplit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "img.png")
	if err := os.WriteFile(path, kittyPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	placed := "a\x1b[1mb" + fileCmd("t=f,f=100", path)
	stream := placed + "c\x1b_Ga=d\x1b\\d"
	var chunks []string
	for i := range len(stream) {
		chunks = append(chunks, stream[i:i+1])
	}
	s := writeKittySession(t, chunks, WithKittyFiles())
	if imgs := s.Terminal().Images(); len(imgs) != 0 {
		t.Fatalf("placements after a=d: %+v", imgs)
	}
	if got := strings.TrimSpace(s.Terminal().Text()); !strings.HasPrefix(got, "ab") || !strings.Contains(got, "cd") {
		t.Errorf("text = %q, want the output around the command", got)
	}

	// Without the delete, a .cast of the timeline replays to the image.
	s = writeKittySession(t, []string{placed}, WithKittyFiles())
	_ = s.Close()
	back := readCast(t, writeCast(t, s.Timeline(), CastV3))
	r, err := newReplay(t.Context(), replayRuntime, back.header, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	for _, ev := range back.events {
		if err = r.apply(ev); err != nil {
			t.Fatal(err)
		}
	}
	if imgs := r.term.Images(); len(imgs) != 1 || imgs[0].Image.Pixels.NRGBAAt(1, 0) != kittyRed {
		t.Errorf("replayed placements %+v, want the red image", imgs)
	}
}

// TestKittyFilesHeld checks that an unfinished command is held until it
// ends, and passed on as sent when the session closes first.
func TestKittyFilesHeld(t *testing.T) {
	t.Parallel()
	s := writeKittySession(t, []string{"x\x1b", "_Ga=T,t=f;abc"}, WithKittyFiles())
	if out := timelineOutput(s.Timeline()); out != "x" {
		t.Errorf("output before the command ends = %q, want x", out)
	}
	_ = s.Close()
	if out := timelineOutput(s.Timeline()); out != "x\x1b_Ga=T,t=f;abc" {
		t.Errorf("output after close = %q, want the unfinished command passed on", out)
	}
}
