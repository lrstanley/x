// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
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

		rec, err := GIF(WithFrameRate(MaxFrameRate, false, func() image.Image {
			img := image.NewNRGBA(bounds)
			solidFrameInto(img, frame%4)
			frame++
			return img
		}))
		if err != nil {
			t.Fatal(err)
		}

		synctest.Sleep(265 * time.Millisecond)

		if err := finishGIF(path, rec); err != nil {
			t.Fatal(err)
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

func TestGIFDedupIncrementalDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := t.TempDir() + "/dedup-delay.gif"
		rec, err := GIF(WithFrameRate(MaxFrameRate, false, func() image.Image {
			return solidFrame(0, 4, 4)
		}))
		if err != nil {
			t.Fatal(err)
		}

		synctest.Sleep(500 * time.Millisecond)

		if err := finishGIF(path, rec); err != nil {
			t.Fatal(err)
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
		rec, err := GIF(WithFrameRate(10, true, func() image.Image {
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

		synctest.Sleep(400 * time.Millisecond)

		if err := finishGIF(path, rec); err != nil {
			t.Fatal(err)
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
		rec, err := GIF(WithFrameRate(10, false, func() image.Image {
			img := solidFrame(frame%3, 4, 4)
			frame++
			return img
		}))
		if err != nil {
			t.Fatal(err)
		}

		synctest.Sleep(300 * time.Millisecond)

		if err := finishGIF(path, rec); err != nil {
			t.Fatal(err)
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

func TestQuantizeDelaysDelayGrid(t *testing.T) {
	t.Parallel()

	opts := options{delayGridSet: true, delayGrid: 20}
	nominalCS := 5

	cases := []struct {
		name  string
		delay time.Duration
		want  int
	}{
		{"zero floors to one step", 0, nominalCS},
		{"sub-step rounds up", 3 * time.Millisecond, nominalCS},
		{"snaps to nearest step", 120 * time.Millisecond, 2 * nominalCS},
		{"exact step", 50 * time.Millisecond, nominalCS},
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
		rec, err := GIF(WithFrameRate(10, true, func() image.Image {
			img := solidFrame(frame%4, 4, 4)
			frame++
			return img
		}))
		if err != nil {
			t.Fatal(err)
		}

		synctest.Sleep(300 * time.Millisecond)

		if err := finishGIF(path, rec); err != nil {
			t.Fatal(err)
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
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeNone, nil, false, frame, time.Millisecond, 0); err != nil {
			t.Fatal(err)
		}
		snapshots[i] = bytes.Clone(rec.images[i].Pix)
	}

	for range 4 {
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeNone, nil, false, frameA, time.Millisecond, 0); err != nil {
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

func TestGIFMaxFramesRetention(t *testing.T) {
	t.Parallel()

	const n = 8
	frames := make([]image.Image, n)
	for i := range n {
		frames[i] = solidFrame(i, 4, 4)
	}
	path := t.TempDir() + "/max-frames.gif"
	if err := recordViaChannel(path, frames, 50*time.Millisecond, WithMaxFrames(3)); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 3 {
		t.Fatalf("got %d frames, want 3 retained", len(g.Image))
	}
	for i := range 3 {
		assertPalettizedSimilar(t, frames[n-3+i], g.Image[i])
	}
}

func TestGIFMaxFramesDelaysNotInflated(t *testing.T) {
	t.Parallel()

	const n = 8
	frames := make([]image.Image, n)
	for i := range n {
		frames[i] = solidFrame(i, 4, 4)
	}
	path := t.TempDir() + "/max-frames-delay.gif"
	if err := recordViaChannel(path, frames, 100*time.Millisecond, WithMaxFrames(2)); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 2 {
		t.Fatalf("got %d frames, want 2 retained", len(g.Image))
	}
	for i, d := range g.Delay {
		if d != 10 {
			t.Fatalf("delay[%d] = %d cs, want 10 cs (100ms explicit, not merged from evicted frames)", i, d)
		}
	}
}

func TestGIFMaxFramesUnlimited(t *testing.T) {
	t.Parallel()

	const n = 8
	frames := make([]image.Image, n)
	for i := range n {
		frames[i] = solidFrame(i, 4, 4)
	}
	path := t.TempDir() + "/max-frames-unlimited.gif"
	if err := recordViaChannel(path, frames, 50*time.Millisecond, WithMaxFrames(0)); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != n {
		t.Fatalf("got %d frames, want %d with WithMaxFrames(0)", len(g.Image), n)
	}
}

func TestGIFMaxFramesDedupCap(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame)
	path := t.TempDir() + "/max-frames-dedup.gif"
	rec, err := GIF(
		WithChannel(ch),
		WithMaxFrames(2),
		WithOptimize(OptimizeFrames),
	)
	if err != nil {
		t.Fatal(err)
	}

	frameA := solidFrame(0, 4, 4)
	frameB := solidFrame(1, 4, 4)
	frameC := solidFrame(2, 4, 4)
	for range 20 {
		ch <- Frame{Image: frameA, Delay: 10 * time.Millisecond}
	}
	ch <- Frame{Image: frameB, Delay: 10 * time.Millisecond}
	ch <- Frame{Image: frameC, Delay: 10 * time.Millisecond}
	close(ch)

	if err := finishGIF(path, rec); err != nil {
		t.Fatal(err)
	}

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) > 2 {
		t.Fatalf("got %d frames, want at most 2 after dedup+cap", len(g.Image))
	}
	if len(g.Image) != 2 {
		t.Fatalf("got %d frames, want 2 (B and C after A deduped)", len(g.Image))
	}
	assertPalettizedSimilar(t, frameB, g.Image[0])
	assertPalettizedSimilar(t, frameC, g.Image[1])
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

func TestGIFChannelAutoDelay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ch := make(chan Frame, 2)
		path := t.TempDir() + "/auto.gif"
		rec, err := GIF(WithChannel(ch))
		if err != nil {
			t.Fatal(err)
		}

		ch <- Frame{Image: solidFrame(1, 4, 4)}
		synctest.Sleep(50 * time.Millisecond)
		ch <- Frame{Image: solidFrame(2, 4, 4)}
		close(ch)

		if err := finishGIF(path, rec); err != nil {
			t.Fatal(err)
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
		rec, err := GIF(WithChannel(ch))
		if err != nil {
			t.Fatal(err)
		}

		ch <- Frame{Image: solidFrame(1, 4, 4), Delay: 10 * time.Millisecond}
		synctest.Wait()

		if err := rec.Close(); err != nil {
			t.Fatal(err)
		}
		if err := rec.Close(); err != nil {
			t.Fatal("second Close call should be no-op")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := rec.EncodeAll(f); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}

		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(g.Image); n != 1 {
			t.Fatalf("got %d frames, want 1 from early Close", n)
		}
	})
}

func TestGIFRejectsNilFrame(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame, 1)
	path := t.TempDir() + "/nil.gif"
	rec, err := GIF(WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}

	ch <- Frame{Image: nil}
	close(ch)

	if err := finishGIF(path, rec); err == nil {
		t.Fatal("expected error for nil frame")
	}
}

func TestGIFRejectsEmptyFrame(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame, 1)
	path := t.TempDir() + "/empty.gif"
	rec, err := GIF(WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}

	ch <- Frame{Image: image.NewNRGBA(image.Rect(0, 0, 0, 0))}
	close(ch)

	if err := finishGIF(path, rec); err == nil {
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

func TestGIFLongDelaySplits(t *testing.T) {
	t.Parallel()

	const wantCS = 70000 // 700s, past the 65535cs GIF frame limit
	src := solidFrame(4, 16, 16)
	var buf bytes.Buffer
	if err := recordViaChannelWriter(&buf, []image.Image{src}, 700*time.Second); err != nil {
		t.Fatal(err)
	}

	g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) < 2 {
		t.Fatalf("got %d frames, want a continuation past 65535cs", len(g.Image))
	}
	sum := 0
	for i, d := range g.Delay {
		if d < 1 || d > 65535 {
			t.Fatalf("delay[%d] = %d, want 1..65535", i, d)
		}
		sum += d
	}
	if sum != wantCS {
		t.Fatalf("delay sum = %d cs, want %d", sum, wantCS)
	}
	if g.Image[1].Bounds().Dx() != 1 || g.Image[1].Bounds().Dy() != 1 {
		t.Fatalf("continuation bounds = %v, want 1x1", g.Image[1].Bounds())
	}

	replayed, err := ReplayGIF(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(g.Image) {
		t.Fatalf("replayed %d frames, gif has %d", len(replayed), len(g.Image))
	}
	for i := range replayed {
		assertPalettizedSimilar(t, src, replayed[i])
	}
}

func TestGIFLoopCount(t *testing.T) {
	t.Parallel()

	frames := []image.Image{solidFrame(0, 4, 4), solidFrame(1, 4, 4)}
	tests := []struct {
		name string
		opts []Option
		want int
	}{
		{name: "default infinite", want: LoopForever},
		{name: "explicit infinite", opts: []Option{WithLoopCount(LoopForever)}, want: LoopForever},
		{name: "play once", opts: []Option{WithLoopCount(LoopOnce)}, want: LoopOnce},
		{name: "play n plus one", opts: []Option{WithLoopCount(4)}, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := recordViaChannelWriter(&buf, frames, 50*time.Millisecond, tt.opts...); err != nil {
				t.Fatal(err)
			}
			g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if g.LoopCount != tt.want {
				t.Fatalf("LoopCount = %d, want %d", g.LoopCount, tt.want)
			}
		})
	}
}

func TestChannelCloseKeepsBufferedFrames(t *testing.T) {
	t.Parallel()

	const n = 20
	ch := make(chan Frame, n)
	rec, err := GIF(WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		ch <- Frame{Image: solidFrame(i*10, 64, 64), Delay: 10 * time.Millisecond}
	}
	close(ch)
	if err = rec.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(rec.Frames()); got != n {
		t.Fatalf("recorded %d frames, want all %d buffered frames", got, n)
	}
}

func TestPalettizeRGBAMatchesNRGBA(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 64, 4))
	for x := range 64 {
		// Rows: opaque, translucent, barely visible, and fully transparent.
		for y, a := range []uint8{0xff, 0x99, 0x01, 0x00} {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: 0x80, B: uint8(255 - x*4), A: a}) //nolint:gosec // bounded.
		}
	}
	rgba := toRGBA(src)
	// Premultiplying loses precision at low alpha, so compare against the
	// NRGBA image the RGBA one converts back to.
	back := toNRGBA(rgba)

	pal := defaultGIFPalette()
	want := palettize(back, pal)
	got := image.NewPaletted(src.Bounds(), pal)
	palettizeInto(got, rgba)
	for y := range 4 {
		for x := range 64 {
			if w, g := want.ColorIndexAt(x, y), got.ColorIndexAt(x, y); w != g {
				t.Errorf("pixel (%d,%d): RGBA source index %d, NRGBA source index %d", x, y, g, w)
			}
		}
	}
	if !bytes.Equal(palettize(src, pal).Pix[:64], got.Pix[:64]) {
		t.Error("opaque pixels differ between RGBA and NRGBA sources")
	}
	for x := range 64 {
		if got.ColorIndexAt(x, 3) != TransparentPaletteIndex {
			t.Errorf("pixel (%d,3) is fully transparent, want TransparentPaletteIndex", x)
		}
	}
}

func TestGIFFrameDoneRecyclesImage(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame)
	rec, err := GIF(WithChannel(ch), WithOptimize(OptimizeNone))
	if err != nil {
		t.Fatal(err)
	}

	// One buffer is reused for every frame: Done poisons it, so a recorder
	// reading it afterwards would see the poison.
	const n = 5
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	done := make(chan struct{}, 1)
	for i := range n {
		solidFrameInto(img, i+1)
		ch <- Frame{Image: img, Delay: 100 * time.Millisecond, Done: func() {
			for j := range img.Pix {
				img.Pix[j] = 0xAA
			}
			done <- struct{}{}
		}}
		<-done
	}
	close(ch)

	path := t.TempDir() + "/done.gif"
	if err := finishGIF(path, rec); err != nil {
		t.Fatal(err)
	}
	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != n {
		t.Fatalf("got %d frames, want %d", len(g.Image), n)
	}
	for i, fr := range g.Image {
		want := color.NRGBA{R: uint8((i + 1) * 40), A: 0xff}
		if got := color.NRGBAModel.Convert(fr.At(0, 0)); got != color.Color(want) {
			t.Errorf("frame %d: got %v, want %v", i, got, want)
		}
	}
}

// TestGIFDeltaRecordingMatchesFullFrames checks that frames stored as bbox
// deltas rebuild into the same full frames a full-frame recording keeps,
// including when maxFrames folds old deltas into the base.
func TestGIFDeltaRecordingMatchesFullFrames(t *testing.T) {
	t.Parallel()

	const w, h, n = 40, 24, 12
	pal := defaultGIFPalette()
	cur := solidFrame(0, w, h)
	var frames []*image.NRGBA
	for i := range n {
		if i%4 != 3 { // every fourth frame repeats the previous one
			x := (i * 3) % (w - 4)
			for y := 2; y < 6; y++ {
				for dx := range 4 {
					cur.SetNRGBA(x+dx, y, color.NRGBA{R: uint8(30 * i), G: 200, B: 10, A: 255}) //nolint:gosec // small.
				}
			}
		}
		c := *cur
		c.Pix = bytes.Clone(cur.Pix)
		frames = append(frames, &c)
	}

	for _, maxFrames := range []int{0, 1, 5, n} {
		var full, delta gifRecording
		for _, f := range frames {
			if err := appendRecordingFrame(&full, pal, true, OptimizeFrames, nil, false, f, time.Millisecond, maxFrames); err != nil {
				t.Fatal(err)
			}
			if err := appendRecordingFrame(&delta, pal, true, OptimizeFrames|OptimizeDirtyRects, nil, false, f, time.Millisecond, maxFrames); err != nil {
				t.Fatal(err)
			}
		}
		got := (&gifRecorder{rec: &delta}).Frames()
		want := (&gifRecorder{rec: &full}).Frames()
		if len(got) != len(want) {
			t.Fatalf("maxFrames=%d: got %d frames, want %d", maxFrames, len(got), len(want))
		}
		for i := range want {
			g, wnt := got[i].Image.(*image.Paletted), want[i].Image.(*image.Paletted)
			if g.Rect != wnt.Rect || !bytes.Equal(g.Pix, wnt.Pix) || got[i].Delay != want[i].Delay {
				t.Fatalf("maxFrames=%d: frame %d differs from full-frame recording", maxFrames, i)
			}
		}
		if len(delta.images) > 1 && delta.images[1].Rect.Eq(delta.images[0].Rect) && maxFrames == 0 && n > 3 {
			t.Fatalf("frame 1 stored as full frame, want bbox delta")
		}
	}
}

func TestGIFDeltaFramesSizeChange(t *testing.T) {
	t.Parallel()

	rec := &gifRecording{}
	solid := func(w, h int, c color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
		}
		return img
	}
	sizes := []image.Point{{20, 10}, {20, 10}, {30, 16}, {30, 16}}
	for i, s := range sizes {
		c := color.NRGBA{R: uint8(i * 60), G: 50, B: 90, A: 255}
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeAll, nil, false, solid(s.X, s.Y, c), time.Millisecond, 0); err != nil {
			t.Fatal(err)
		}
	}

	frames := (&gifRecorder{rec: rec}).Frames()
	if len(frames) != len(sizes) {
		t.Fatalf("got %d frames, want %d", len(frames), len(sizes))
	}
	for i, f := range frames {
		if got, want := f.Image.Bounds().Size(), sizes[i]; got != want {
			t.Errorf("frame %d size = %v, want %v", i, got, want)
		}
	}
}
