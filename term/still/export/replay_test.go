// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"
)

func TestReplayGIFRoundTrip(t *testing.T) {
	t.Parallel()

	originals := []image.Image{
		solidFrame(1, 16, 16),
		solidFrame(2, 16, 16),
		solidFrame(3, 16, 16),
	}

	path := t.TempDir() + "/roundtrip.gif"
	if err := recordViaChannel(path, originals, 100*time.Millisecond, WithOptimize(OptimizeNone)); err != nil {
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
		assertPalettizedSimilar(t, originals[i], replayed[i])
	}
}

func TestReplayGIFDisposals(t *testing.T) {
	t.Parallel()

	w, h := 32, 32
	base := solidFrame(0, w, h)
	dotOn := cloneNRGBA(base)
	dotOff := cloneNRGBA(base)
	paintRect(dotOn, image.Rect(10, 10, 12, 12), color.NRGBA{R: 0xff, A: 0xff})
	paintRect(dotOff, image.Rect(10, 10, 12, 12), color.NRGBA{B: 0x80, A: 0xff})

	originals := []image.Image{base, dotOn, dotOff, dotOn}

	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, originals, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	replayed, err := ReplayGIF(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(originals) {
		t.Fatalf("got %d replayed frames, want %d", len(replayed), len(originals))
	}
	for i := range originals {
		assertPalettizedSimilar(t, originals[i], replayed[i])
	}
}

func TestOptimizePaletteRoundTrip(t *testing.T) {
	t.Parallel()

	src := solidFrame(1, 8, 8)
	frames := []image.Image{src, src, solidFrame(2, 8, 8)}
	path := t.TempDir() + "/quant.gif"
	if err := recordViaChannel(path, frames, 100*time.Millisecond, WithOptimize(OptimizeFrames|OptimizeColorQuantization)); err != nil {
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
	assertPalettizedSimilar(t, src, replayed[0])
}

func TestOptimizePaletteReducesFileSize(t *testing.T) {
	t.Parallel()

	frames := []image.Image{solidFrame(1, 16, 16), solidFrame(2, 16, 16), solidFrame(3, 16, 16)}

	var baseline bytes.Buffer
	if err := recordViaChannelWriter(&baseline, frames, 100*time.Millisecond, WithOptimize(OptimizeNone)); err != nil {
		t.Fatal(err)
	}

	var compact bytes.Buffer
	if err := recordViaChannelWriter(&compact, frames, 100*time.Millisecond, WithOptimize(OptimizeColorQuantization)); err != nil {
		t.Fatal(err)
	}

	if compact.Len() >= baseline.Len() {
		t.Fatalf("quantized GIF size %d >= baseline %d", compact.Len(), baseline.Len())
	}
}
