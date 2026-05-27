// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
	"testing/synctest"
	"time"
)

func TestGIFFrameCount(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		frame := 0
		bounds := image.Rect(0, 0, 8, 8)
		path := t.TempDir() + "/out.gif"

		closer, err := GIF(path, WithFrameRate(MaxFrameRate, false, func() image.Image {
			img := image.NewNRGBA(bounds)
			solidFrameInto(img, frame%4)
			frame++
			return img
		}))
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(265 * time.Millisecond)
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(g.Image); n != 26 {
			t.Fatalf("got %d frames, want 26 ticks for 265ms at %dfps", n, MaxFrameRate)
		}
	})
}

func TestGIFDedupesIdle(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := t.TempDir() + "/idle.gif"
		closer, err := GIF(path, WithFrameRate(MaxFrameRate, false, func() image.Image {
			return solidFrame(0, 4, 4)
		}))
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(100 * time.Millisecond)
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Image) != 1 {
			t.Fatalf("got %d frames, want 1 deduped idle frame", len(g.Image))
		}
		if g.Delay[0] < 10 {
			t.Fatalf("delay = %d cs, want accumulated idle time >= 10 cs", g.Delay[0])
		}
	})
}

func TestGIFDedupIncrementalDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := t.TempDir() + "/dedup-delay.gif"
		closer, err := GIF(path, WithFrameRate(MaxFrameRate, false, func() image.Image {
			return solidFrame(0, 4, 4)
		}))
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(500 * time.Millisecond)
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Image) != 1 {
			t.Fatalf("got %d frames, want 1 deduped idle frame", len(g.Image))
		}
		// ~500ms idle at max fps should stay near wall time, not explode via
		// repeated time-since-last-emit accumulation on deduped ticks.
		if g.Delay[0] > 100 {
			t.Fatalf("delay = %d cs, want <= 100 cs (~500ms idle)", g.Delay[0])
		}
		if g.Delay[0] < 40 {
			t.Fatalf("delay = %d cs, want >= 40 cs (~500ms idle)", g.Delay[0])
		}
	})
}

func TestOptimizeDisposalsBackgroundIndex(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 0x0e, G: 0x11, B: 0x16, A: 0xff}
	base := solidNRGBA(32, 32, bg)
	scroll := cloneNRGBA(base)
	paintRect(scroll, image.Rect(0, 8, 32, 24), color.NRGBA{R: 0xff, A: 0xff})

	originals := []image.Image{base, scroll, base}
	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, originals, 50*time.Millisecond,
		WithBackground(bg),
		WithOptimize(OptimizeDirtyRects),
	); err != nil {
		t.Fatal(err)
	}

	raw := bytes.Clone(buf.Bytes())

	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	wantBG := uint8(g.Image[0].Palette.Index(bg)) //nolint:gosec // bounded.
	if g.BackgroundIndex != wantBG {
		t.Fatalf("BackgroundIndex = %d, want %d from WithBackground", g.BackgroundIndex, wantBG)
	}

	replayed, err := ReplayGIF(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for i := range originals {
		assertPalettizedSimilar(t, originals[i], replayed[i])
	}
}

func TestOptimizeDisposalsDominantBackgroundIndex(t *testing.T) {
	t.Parallel()

	margin := color.NRGBA{R: 0x81, G: 0x2c, B: 0xd1, A: 0xff}
	content := color.NRGBA{R: 0x0e, G: 0x11, B: 0x16, A: 0xff}
	// Small margin border; content bg dominates pixel count.
	frame := solidNRGBA(32, 32, content)
	paintRect(frame, image.Rect(0, 0, 32, 2), margin)
	paintRect(frame, image.Rect(0, 0, 2, 32), margin)

	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, []image.Image{frame, frame}, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	wantBG := uint8(g.Image[0].Palette.Index(content)) //nolint:gosec // bounded.
	if g.BackgroundIndex != wantBG {
		t.Fatalf("BackgroundIndex = %d, want dominant content index %d", g.BackgroundIndex, wantBG)
	}
}

func TestGIFStrictDedupMultiple(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tick := 0
		path := t.TempDir() + "/dedup-multiple.gif"
		closer, err := GIF(path, WithFrameRate(10, true, func() image.Image {
			tick++
			switch tick {
			case 1:
				return solidFrame(1, 4, 4)
			case 2:
				return solidFrame(2, 4, 4)
			default:
				return solidFrame(3, 4, 4)
			}
		}))
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(400 * time.Millisecond)
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Image) != 3 {
			t.Fatalf("got %d frames, want 3 (ticks 3-4 deduped into frame 3)", len(g.Image))
		}
		want := []int{10, 10, 20}
		for i, d := range g.Delay {
			if d != want[i] {
				t.Fatalf("delay[%d] = %d cs, want %d cs", i, d, want[i])
			}
		}
	})
}

func TestGIFNonStrictGridSnap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		frame := 0
		path := t.TempDir() + "/grid-snap.gif"
		closer, err := GIF(path, WithFrameRate(10, false, func() image.Image {
			img := solidFrame(frame%3, 4, 4)
			frame++
			return img
		}))
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(300 * time.Millisecond)
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		const nominalCS = 10
		for i, d := range g.Delay {
			if d%nominalCS != 0 {
				t.Fatalf("delay[%d] = %d cs, want multiple of %d cs at 10fps", i, d, nominalCS)
			}
		}
	})
}

func TestGIFChannelRoundOnce(t *testing.T) {
	t.Parallel()

	same := solidFrame(0, 4, 4)
	frames := []image.Image{same, same, same}
	path := t.TempDir() + "/round-once.gif"
	if err := recordViaChannel(path, frames, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 1 {
		t.Fatalf("got %d frames, want 1 deduped frame", len(g.Image))
	}
	if g.Delay[0] != 3 {
		t.Fatalf("delay = %d cs, want 3 cs from single round of 30ms", g.Delay[0])
	}
}

func TestQuantizeDelaysFPSGrid(t *testing.T) {
	t.Parallel()

	opts := options{frameRateSet: true, frameRate: 10}
	nominalCS := 10

	cases := []struct {
		name  string
		delay time.Duration
		want  int
	}{
		{"sub-tick rounds up", 47 * time.Millisecond, nominalCS},
		{"triple tick", 300 * time.Millisecond, 3 * nominalCS},
		{"exact tick", 100 * time.Millisecond, nominalCS},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := quantizeDelays([]time.Duration{tc.delay}, opts)
			if got[0] != tc.want {
				t.Fatalf("quantizeDelays(%v) = %d cs, want %d cs", tc.delay, got[0], tc.want)
			}
		})
	}
}

func TestOptimizeDisposalsMergesDurationDelays(t *testing.T) {
	t.Parallel()

	pal := defaultGIFPalette()
	base := palettize(solidFrame(0, 8, 8), pal)
	rec := &gifRecording{
		config: image.Config{Width: 8, Height: 8, ColorModel: pal},
		images: []*image.Paletted{
			clonePaletted(base),
			clonePaletted(base),
			clonePaletted(base),
		},
		delays: []time.Duration{
			100 * time.Millisecond,
			50 * time.Millisecond,
			50 * time.Millisecond,
		},
	}
	optimizeDisposals(rec, nil, false)

	if len(rec.images) != 1 {
		t.Fatalf("got %d frames, want 1 merged canvas-identical frame", len(rec.images))
	}
	if rec.delays[0] != 200*time.Millisecond {
		t.Fatalf("merged delay = %v, want 200ms", rec.delays[0])
	}
}

func TestGIFSubRectTransparentPaletteSlot(t *testing.T) {
	t.Parallel()

	frames := cursorBlinkSequence(64, 48, 3)

	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, frames, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) == 0 {
		t.Fatal("expected frames")
	}

	pal := g.Image[0].Palette
	if len(pal) <= int(TransparentPaletteIndex) {
		t.Fatalf("palette len = %d, want > transparent slot %d", len(pal), TransparentPaletteIndex)
	}
	if _, _, _, a := pal[TransparentPaletteIndex].RGBA(); a != 0 {
		t.Fatalf("palette[%d] alpha = %d, want 0 for GIF transparency", TransparentPaletteIndex, a>>8)
	}

	replayed, err := ReplayGIF(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for i := range frames {
		assertPalettizedSimilar(t, frames[i], replayed[i])
	}
}

func TestOptimizeDisposalsScrollStripReplay(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{B: 0x80, A: 0xff}
	w, h := 128, 96
	base := solidNRGBA(w, h, bg)
	strip1 := cloneNRGBA(base)
	paintRect(strip1, image.Rect(0, 72, w, h), color.NRGBA{R: 0xff, A: 0xff})
	strip2 := cloneNRGBA(base)
	paintRect(strip2, image.Rect(0, 48, w, h), color.NRGBA{R: 0xff, A: 0xff})

	originals := []image.Image{base, strip1, strip2}
	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, originals, 50*time.Millisecond,
		WithBackground(bg),
		WithOptimize(OptimizeDirtyRects),
	); err != nil {
		t.Fatal(err)
	}

	g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Disposal) < 2 {
		t.Fatal("expected disposal metadata for delta frame")
	}
	if g.Disposal[1] != gif.DisposalNone {
		t.Fatalf("scroll strip disposal = %d, want DisposalNone", g.Disposal[1])
	}

	replayed, err := ReplayGIF(bytes.NewReader(buf.Bytes()))
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

func TestOptimizeDisposalsSelectionRowRevert(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 0x0e, G: 0x11, B: 0x16, A: 0xff}
	mark := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	sel := color.NRGBA{R: 0x5f, G: 0x37, B: 0xff, A: 0xff} // xterm-57 selection purple
	w, h := 200, 120
	rowY := []int{20, 44, 68, 92}

	table := solidNRGBA(w, h, bg)
	for _, y := range rowY {
		paintRect(table, image.Rect(20, y, 80, y+12), mark)
	}

	row1 := cloneNRGBA(table)
	paintRect(row1, image.Rect(0, 36, w, 56), sel)

	row2 := cloneNRGBA(table)
	paintRect(row2, image.Rect(0, 60, w, 80), sel)

	originals := []image.Image{table, row1, row2}
	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, originals, 50*time.Millisecond,
		WithBackground(bg),
		WithOptimize(OptimizeDirtyRects),
	); err != nil {
		t.Fatal(err)
	}

	g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Disposal) < 2 || g.Disposal[1] != gif.DisposalNone {
		t.Fatalf("selection strip disposal = %v, want DisposalNone on delta frame", g.Disposal)
	}

	replayed, err := ReplayGIF(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for i := range originals {
		assertPalettizedSimilar(t, originals[i], replayed[i])
	}

	var fullBuf bytes.Buffer
	if recErr := recordViaChannelWriter(&fullBuf, originals, 50*time.Millisecond,
		WithBackground(bg),
		WithOptimize(OptimizeFrames),
	); recErr != nil {
		t.Fatal(recErr)
	}
	fullReplay, err := ReplayGIF(bytes.NewReader(fullBuf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	wrong, pixels := replayMismatchPct(fullReplay, replayed, bg)
	t.Logf("selection row optimized vs full replay mismatch: %.4f%% (%d/%d)", wrong, int(wrong*float64(pixels)/100), pixels)
	if wrong > 0.01 {
		t.Fatalf("optimized replay mismatch %.4f%% exceeds 0.01%%", wrong)
	}
}

func TestOptimizeDisposalsBackgroundHole(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 0x0e, G: 0x11, B: 0x16, A: 0xff}
	mark := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	w, h := 128, 96
	base := solidNRGBA(w, h, bg)
	paintRect(base, image.Rect(10, 10, 30, 30), mark)

	patch := cloneNRGBA(base)
	paintRect(patch, image.Rect(0, 72, w, 80), color.NRGBA{R: 0xff, A: 0xff})
	restored := cloneNRGBA(base)

	originals := []image.Image{base, patch, restored}
	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, originals, 50*time.Millisecond,
		WithBackground(bg),
		WithOptimize(OptimizeDirtyRects),
	); err != nil {
		t.Fatal(err)
	}

	replayed, err := ReplayGIF(bytes.NewReader(buf.Bytes()))
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

func TestGIFStrictFPSFixedDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		frame := 0
		path := t.TempDir() + "/strict-delay.gif"
		closer, err := GIF(path, WithFrameRate(10, true, func() image.Image {
			img := solidFrame(frame%4, 4, 4)
			frame++
			return img
		}))
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(300 * time.Millisecond)
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Image) == 0 {
			t.Fatal("expected at least one frame")
		}
		for i, d := range g.Delay {
			if d != 10 {
				t.Fatalf("delay[%d] = %d cs, want 10 cs for strict 10fps", i, d)
			}
		}
	})
}

func TestGIFArenaFrameRetention(t *testing.T) {
	t.Parallel()

	rec := &gifRecording{loopCount: 0}
	frameA := terminalBenchmarkFrame(0)
	frameB := terminalBenchmarkFrame(1)

	const n = 8
	snapshots := make([][]byte, n)
	for i := range n {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeNone, nil, false, frame, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		snapshots[i] = bytes.Clone(rec.images[i].Pix)
	}

	for range 4 {
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeNone, nil, false, frameA, time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}

	for i := range n {
		if !bytes.Equal(rec.images[i].Pix, snapshots[i]) {
			t.Fatalf("frame %d Pix mutated after subsequent captures", i)
		}
	}
	for i := range n {
		for j := i + 1; j < n; j++ {
			if pixSharesBacking(rec.images[i].Pix, rec.images[j].Pix) {
				t.Fatalf("frames %d and %d share Pix backing", i, j)
			}
		}
	}
}

func pixSharesBacking(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return &a[0] == &b[0]
}

func TestPalettizePreservesDistinctColors(t *testing.T) {
	t.Parallel()

	a := solidFrame(1, 8, 8)
	b := solidFrame(2, 8, 8)
	pal := defaultGIFPalette()
	pa := palettize(a, pal)
	pb := palettize(b, pal)
	if bytes.Equal(pa.Pix, pb.Pix) {
		t.Fatal("expected distinct palettized output")
	}
}

func TestGIFChannelExplicitDelay(t *testing.T) {
	t.Parallel()

	frames := []image.Image{
		solidFrame(1, 8, 8),
		solidFrame(2, 8, 8),
	}
	path := t.TempDir() + "/channel.gif"
	if err := recordViaChannel(path, frames, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 2 {
		t.Fatalf("got %d frames, want 2", len(g.Image))
	}
	if g.Delay[0] != 25 {
		t.Fatalf("delay[0] = %d cs, want 25 cs for 250ms", g.Delay[0])
	}
}

func TestGIFChannelDedup(t *testing.T) {
	t.Parallel()

	same := solidFrame(0, 4, 4)
	frames := []image.Image{same, same, same}
	path := t.TempDir() + "/dedup.gif"
	if err := recordViaChannel(path, frames, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 1 {
		t.Fatalf("got %d frames, want 1 deduped frame", len(g.Image))
	}
	if g.Delay[0] < 3 {
		t.Fatalf("delay = %d cs, want merged delay >= 3 cs", g.Delay[0])
	}
}

func TestGIFChannelAutoDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ch := make(chan Frame, 2)
		path := t.TempDir() + "/auto.gif"
		closer, err := GIF(path, WithChannel(ch))
		if err != nil {
			t.Fatal(err)
		}

		ch <- Frame{Image: solidFrame(1, 4, 4)}
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		ch <- Frame{Image: solidFrame(2, 4, 4)}
		close(ch)

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Image) != 2 {
			t.Fatalf("got %d frames, want 2", len(g.Image))
		}
		if g.Delay[1] < 5 {
			t.Fatalf("auto delay[1] = %d cs, want >= 5 cs for 50ms since prior emit", g.Delay[1])
		}
	})
}

func TestGIFChannelCloserBeforeClose(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ch := make(chan Frame, 1)
		path := t.TempDir() + "/early.gif"
		closer, err := GIF(path, WithChannel(ch))
		if err != nil {
			t.Fatal(err)
		}

		ch <- Frame{Image: solidFrame(1, 4, 4), Delay: 10 * time.Millisecond}
		synctest.Wait()

		if cerr := closer(); cerr != nil {
			t.Fatal(cerr)
		}
		if cerr := closer(); cerr != nil {
			t.Fatal("second closer call should be no-op")
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(g.Image); n != 1 {
			t.Fatalf("got %d frames, want 1 from early closer", n)
		}
	})
}

func TestGIFRejectsNilFrame(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame, 1)
	path := t.TempDir() + "/nil.gif"
	closer, err := GIF(path, WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}

	ch <- Frame{Image: nil}
	close(ch)

	if cerr := closer(); cerr == nil {
		t.Fatal("expected error for nil frame")
	}
}

func TestGIFRejectsEmptyFrame(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame, 1)
	path := t.TempDir() + "/empty.gif"
	closer, err := GIF(path, WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}

	ch <- Frame{Image: image.NewNRGBA(image.Rect(0, 0, 0, 0))}
	close(ch)

	if cerr := closer(); cerr == nil {
		t.Fatal("expected error for empty frame bounds")
	}
}

func TestGIFTransparentPixelsRoundTrip(t *testing.T) {
	t.Parallel()

	src := frameWithTransparentCenter(16, 16)
	path := t.TempDir() + "/transparent.gif"
	if err := recordViaChannel(path, []image.Image{src}, 100*time.Millisecond, WithOptimize(OptimizeNone)); err != nil {
		t.Fatal(err)
	}

	replayed, err := replayGIFFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 {
		t.Fatalf("got %d frames, want 1", len(replayed))
	}
	assertTransparentCenter(t, src, replayed[0])
}

func TestOptimizeDisposalsOverlayRevert(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{B: 0x80, A: 0xff}
	highlight := color.NRGBA{R: 0xff, A: 0xff}
	base := solidNRGBA(64, 48, bg)
	flash := cloneNRGBA(base)
	paintRect(flash, image.Rect(20, 20, 28, 28), highlight)

	originals := []image.Image{base, flash, base}
	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, originals, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) < 3 {
		t.Fatalf("got %d optimized frames, want >= 3 so revert is not dropped", len(g.Image))
	}

	replayed, err := ReplayGIF(bytes.NewReader(buf.Bytes()))
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

func TestOptimizeDisposalsReducesCursorBlinkSize(t *testing.T) {
	t.Parallel()

	frames := cursorBlinkSequence(64, 48, 20)

	var dedupOnly bytes.Buffer
	if err := recordViaChannelWriter(&dedupOnly, frames, 50*time.Millisecond, WithOptimize(OptimizeFrames)); err != nil {
		t.Fatal(err)
	}

	var withDisposals bytes.Buffer
	if err := recordViaChannelWriter(&withDisposals, frames, 50*time.Millisecond, WithOptimize(OptimizeFrames|OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	if withDisposals.Len() >= dedupOnly.Len() {
		t.Fatalf("dirty-rect GIF size %d >= dedup-only %d", withDisposals.Len(), dedupOnly.Len())
	}
}

// frameWithTransparentCenter is an opaque green canvas with a transparent hole.
func frameWithTransparentCenter(w, h int) *image.NRGBA {
	img := solidNRGBA(w, h, color.NRGBA{G: 0x80, A: 0xff})
	hole := image.Rect(w/4, h/4, 3*w/4, 3*h/4)
	for y := hole.Min.Y; y < hole.Max.Y; y++ {
		for x := hole.Min.X; x < hole.Max.X; x++ {
			img.SetNRGBA(x, y, color.NRGBA{A: 0})
		}
	}
	return img
}

// assertTransparentCenter verifies replayed output preserves alpha=0 in the source hole.
func assertTransparentCenter(t *testing.T, src, got image.Image) {
	t.Helper()
	b := src.Bounds()
	cx, cy := b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2
	_, _, _, sa := src.At(cx, cy).RGBA()
	_, _, _, ga := got.At(cx, cy).RGBA()
	if sa != 0 {
		t.Fatalf("fixture center alpha = %d, want transparent test pixel", sa>>8)
	}
	if ga != 0 {
		t.Fatalf("replayed center alpha = %d, want 0", ga>>8)
	}
}
