// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// replayGIF decodes r and rebuilds each frame as a full-canvas NRGBA image,
// undoing disposal methods in order. The decoder gives the transparent index
// a zero-alpha color, so drawing a frame over the canvas skips those pixels.
func replayGIF(r io.Reader) ([]image.Image, error) {
	g, err := gif.DecodeAll(r)
	if err != nil {
		return nil, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	frames := make([]image.Image, 0, len(g.Image))
	for i, frame := range g.Image {
		var before *image.NRGBA
		if g.Disposal[i] == gif.DisposalPrevious {
			before = cloneNRGBA(canvas)
		}
		draw.Draw(canvas, frame.Rect, frame, frame.Rect.Min, draw.Over)
		frames = append(frames, cloneNRGBA(canvas))
		switch g.Disposal[i] {
		case gif.DisposalPrevious:
			canvas = before
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Rect, image.Transparent, image.Point{}, draw.Src)
		}
	}
	return frames, nil
}

func TestReplayGIFRoundTrip(t *testing.T) {
	t.Parallel()

	originals := []image.Image{
		solidFrame(1, 16, 16),
		solidFrame(2, 16, 16),
		solidFrame(3, 16, 16),
	}

	path := t.TempDir() + "/roundtrip.gif"
	if err := writeGIF(path, originals, 100*time.Millisecond, WithOptimize(OptimizeNone)); err != nil {
		t.Fatal(err)
	}

	replayed, err := replayGIFFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(originals) {
		t.Fatalf("got %d replayed frames, want %d", len(replayed), len(originals))
	}
	for i := range originals {
		assertImagesSimilar(t, originals[i], replayed[i])
	}
}

func TestReplayGIFDisposals(t *testing.T) {
	t.Parallel()

	w, h := 32, 32
	base := solidFrame(0, w, h)
	dotOn := cloneNRGBA(base)
	dotOff := cloneNRGBA(base)
	testutil.PaintRect(dotOn, image.Rect(10, 10, 12, 12), color.NRGBA{R: 0xff, A: 0xff})
	testutil.PaintRect(dotOff, image.Rect(10, 10, 12, 12), color.NRGBA{B: 0x80, A: 0xff})

	originals := []image.Image{base, dotOn, dotOff, dotOn}

	var buf bytes.Buffer
	if err := writeGIFTo(&buf, originals, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	replayed, err := replayGIF(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(originals) {
		t.Fatalf("got %d replayed frames, want %d", len(replayed), len(originals))
	}
	for i := range originals {
		assertImagesSimilar(t, originals[i], replayed[i])
	}
}

func TestOptimizePaletteRoundTrip(t *testing.T) {
	t.Parallel()

	src := solidFrame(1, 8, 8)
	frames := []image.Image{src, src, solidFrame(2, 8, 8)}
	path := t.TempDir() + "/quant.gif"
	if err := writeGIF(path, frames, 100*time.Millisecond, WithOptimize(OptimizeFrames|OptimizeColorQuantization)); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) == 0 {
		t.Fatal("expected frames")
	}
	if palLen := len(g.Image[0].Palette); palLen > 4 {
		t.Fatalf("compact palette len = %d, want <= 4 used colors", palLen)
	}

	replayed, err := replayGIFFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertImagesSimilar(t, src, replayed[0])
}

func TestOptimizePaletteReducesFileSize(t *testing.T) {
	t.Parallel()

	frames := []image.Image{solidFrame(1, 16, 16), solidFrame(2, 16, 16), solidFrame(3, 16, 16)}

	var baseline bytes.Buffer
	if err := writeGIFTo(&baseline, frames, 100*time.Millisecond, WithOptimize(OptimizeNone)); err != nil {
		t.Fatal(err)
	}

	var compact bytes.Buffer
	if err := writeGIFTo(&compact, frames, 100*time.Millisecond, WithOptimize(OptimizeColorQuantization)); err != nil {
		t.Fatal(err)
	}

	if compact.Len() >= baseline.Len() {
		t.Fatalf("quantized GIF size %d >= baseline %d", compact.Len(), baseline.Len())
	}
}
