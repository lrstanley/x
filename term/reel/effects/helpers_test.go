// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
)

const ms = time.Millisecond

// update rewrites the golden images in testdata instead of comparing with
// them, as with "go test -update".
var update = flag.Bool("update", false, "rewrite the golden images in testdata")

// event is one event of a timeline built with [buildTimeline]: an
// asciicast event code and its data, at an offset.
type event struct {
	at   time.Duration
	code string
	data string
}

func output(atMS int, s string) event { return event{time.Duration(atMS) * ms, "o", s} }
func input(atMS int, s string) event  { return event{time.Duration(atMS) * ms, "i", s} }
func mark(atMS int, s string) event   { return event{time.Duration(atMS) * ms, "m", s} }

// buildTimeline returns a cols by rows timeline titled title, with events,
// read from the asciicast v2 file they make.
func buildTimeline(t *testing.T, cols, rows int, title string, events ...event) *reel.Timeline {
	t.Helper()
	var b strings.Builder
	header, _ := json.Marshal(map[string]any{"version": 2, "width": cols, "height": rows, "title": title})
	b.Write(header)
	for _, ev := range events {
		data, _ := json.Marshal(ev.data)
		fmt.Fprintf(&b, "\n[%.3f, %q, %s]", ev.at.Seconds(), ev.code, data)
	}
	tl, err := reel.ReadTimeline(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// recordFrames is an [reel.Effect] that appends every frame it sees to
// frames and passes it on.
type recordFrames struct{ frames *[]reel.Frame }

func (e recordFrames) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		*e.frames = append(*e.frames, f)
		emit(f)
	}, nil
}

// exportFrames exports tl to a GIF with the profile's rasterizer, or the
// fake one without one, and returns the frames the GIF was given.
func exportFrames(t *testing.T, tl *reel.Timeline, p reel.Profile) []reel.Frame {
	t.Helper()
	var frames []reel.Frame
	p.Outputs = reel.Files(filepath.Join(t.TempDir(), "out.gif"))
	if p.Rasterizer == nil {
		p.Rasterizer = testutil.Rasterizer{}
	}
	p.Effects = append(slices.Clip(p.Effects), recordFrames{&frames})
	if err := reel.Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	return frames
}

// totalDelay is how long frames play.
func totalDelay(frames []reel.Frame) time.Duration {
	var total time.Duration
	for _, f := range frames {
		total += f.Delay
	}
	return total
}

// assertGolden compares the images of frames, stacked top to bottom with a
// transparent row between them, with testdata/name.png, or writes it with
// -update.
func assertGolden(t *testing.T, name string, frames []reel.Frame) {
	t.Helper()
	var w, h int
	for _, f := range frames {
		w, h = max(w, f.Image.Rect.Dx()), h+f.Image.Rect.Dy()+1
	}
	sheet := image.NewNRGBA(image.Rect(0, 0, w, max(0, h-1)))
	y := 0
	for _, f := range frames {
		r := f.Image.Rect
		draw.Draw(sheet, image.Rect(0, y, r.Dx(), y+r.Dy()), f.Image, r.Min, draw.Src)
		y += r.Dy() + 1
	}
	path := filepath.Join("testdata", name+".png")
	if *update {
		var buf bytes.Buffer
		if err := png.Encode(&buf, sheet); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run the test with -update to write it)", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want := image.NewNRGBA(img.Bounds())
	draw.Draw(want, want.Rect, img, img.Bounds().Min, draw.Src)
	if want.Rect != sheet.Rect || !bytes.Equal(want.Pix, sheet.Pix) {
		got := filepath.Join(t.ArtifactDir(), name+".png")
		var buf bytes.Buffer
		_ = png.Encode(&buf, sheet)
		_ = os.WriteFile(got, buf.Bytes(), 0o600)
		t.Fatalf("frames differ from %s; got %s, kept with go test -artifacts (run with -update to accept)", path, got)
	}
}

// readPNG decodes the PNG at path.
func readPNG(t *testing.T, path string) *image.NRGBA {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Rect, img, img.Bounds().Min, draw.Src)
	return out
}
