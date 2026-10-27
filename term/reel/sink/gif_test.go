// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/gif"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/sink/internal/gifopt"
	"github.com/lrstanley/x/term/reel/sink/internal/palette"
	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

func TestOptimizeDisposalsBackgroundIndex(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 0x0e, G: 0x11, B: 0x16, A: 0xff}
	base := solidNRGBA(32, 32, bg)
	scroll := cloneNRGBA(base)
	testutil.PaintRect(scroll, image.Rect(0, 8, 32, 24), color.NRGBA{R: 0xff, A: 0xff})

	originals := []image.Image{base, scroll, base}
	var buf bytes.Buffer
	if err := writeGIFTo(&buf, originals, 50*time.Millisecond,
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
	testutil.PaintRect(frame, image.Rect(0, 0, 32, 2), margin)
	testutil.PaintRect(frame, image.Rect(0, 0, 2, 32), margin)

	var buf bytes.Buffer
	if err := writeGIFTo(&buf, []image.Image{frame, frame}, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
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

func TestGIFDedupMultiple(t *testing.T) {
	t.Parallel()

	frames := []image.Image{solidFrame(1, 4, 4), solidFrame(2, 4, 4), solidFrame(3, 4, 4), solidFrame(3, 4, 4)}
	path := t.TempDir() + "/dedup-multiple.gif"
	if err := writeGIF(path, frames, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 3 {
		t.Fatalf("got %d frames, want 3 (frames 3 and 4 deduped)", len(g.Image))
	}
	want := []int{10, 10, 20}
	for i, d := range g.Delay {
		if d != want[i] {
			t.Fatalf("delay[%d] = %d cs, want %d cs", i, d, want[i])
		}
	}
}

func TestGIFChannelRoundOnce(t *testing.T) {
	t.Parallel()

	same := solidFrame(0, 4, 4)
	frames := []image.Image{same, same, same}
	path := t.TempDir() + "/round-once.gif"
	if err := writeGIF(path, frames, 10*time.Millisecond); err != nil {
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

func TestQuantizeDelays(t *testing.T) {
	t.Parallel()

	in := []time.Duration{0, 4 * time.Millisecond, 15 * time.Millisecond, 70 * time.Millisecond}
	want := []int{1, 1, 2, 7}
	if got := quantizeDelays(in); !slices.Equal(got, want) {
		t.Fatalf("quantizeDelays(%v) = %v cs, want %v cs", in, got, want)
	}
}

func TestOptimizeDisposalsMergesDurationDelays(t *testing.T) {
	t.Parallel()

	pal := palette.Default()
	base := palettize(solidFrame(0, 8, 8), pal)
	rec := &gifRecording{
		config: image.Config{Width: 8, Height: 8, ColorModel: pal},
		images: []*image.Paletted{
			gifopt.Clone(base),
			gifopt.Clone(base),
			gifopt.Clone(base),
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
	if err := writeGIFTo(&buf, frames, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
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
	testutil.PaintRect(strip1, image.Rect(0, 72, w, h), color.NRGBA{R: 0xff, A: 0xff})
	strip2 := cloneNRGBA(base)
	testutil.PaintRect(strip2, image.Rect(0, 48, w, h), color.NRGBA{R: 0xff, A: 0xff})

	originals := []image.Image{base, strip1, strip2}
	var buf bytes.Buffer
	if err := writeGIFTo(&buf, originals, 50*time.Millisecond,
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
		testutil.PaintRect(table, image.Rect(20, y, 80, y+12), mark)
	}

	row1 := cloneNRGBA(table)
	testutil.PaintRect(row1, image.Rect(0, 36, w, 56), sel)

	row2 := cloneNRGBA(table)
	testutil.PaintRect(row2, image.Rect(0, 60, w, 80), sel)

	originals := []image.Image{table, row1, row2}
	var buf bytes.Buffer
	if err := writeGIFTo(&buf, originals, 50*time.Millisecond,
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
	if recErr := writeGIFTo(&fullBuf, originals, 50*time.Millisecond,
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
	testutil.PaintRect(base, image.Rect(10, 10, 30, 30), mark)

	patch := cloneNRGBA(base)
	testutil.PaintRect(patch, image.Rect(0, 72, w, 80), color.NRGBA{R: 0xff, A: 0xff})
	restored := cloneNRGBA(base)

	originals := []image.Image{base, patch, restored}
	var buf bytes.Buffer
	if err := writeGIFTo(&buf, originals, 50*time.Millisecond,
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

func TestGIFArenaFrameRetention(t *testing.T) {
	t.Parallel()

	rec := &gifRecording{loopCount: 0}
	frameA := testutil.TerminalFrame(0)
	frameB := testutil.TerminalFrame(1)

	const n = 8
	snapshots := make([][]byte, n)
	for i := range n {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, palette.Default(), true, OptimizeNone, nil, false, false, frame, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		snapshots[i] = bytes.Clone(rec.images[i].Pix)
	}

	for range 4 {
		if err := appendRecordingFrame(rec, palette.Default(), true, OptimizeNone, nil, false, false, frameA, time.Millisecond); err != nil {
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
	if err := writeGIF(path, frames, 50*time.Millisecond, WithMaxFrames(3)); err != nil {
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
	if err := writeGIF(path, frames, 100*time.Millisecond, WithMaxFrames(2)); err != nil {
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
	if err := writeGIF(path, frames, 50*time.Millisecond, WithMaxFrames(0)); err != nil {
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

	frameA := solidFrame(0, 4, 4)
	frameB := solidFrame(1, 4, 4)
	frameC := solidFrame(2, 4, 4)
	var frames []image.Image
	for range 20 {
		frames = append(frames, frameA)
	}
	frames = append(frames, frameB, frameC)
	path := t.TempDir() + "/max-frames-dedup.gif"
	if err := writeGIF(path, frames, 10*time.Millisecond, WithMaxFrames(2), WithOptimize(OptimizeFrames)); err != nil {
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
	if err := writeGIF(path, frames, 250*time.Millisecond); err != nil {
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

func TestGIFRejectsNilFrame(t *testing.T) {
	t.Parallel()

	enc, err := CreateGIF(t.TempDir() + "/nil.gif")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enc.Discard() }()
	if err = enc.WriteFrame(Frame{Image: nil, Delay: 10 * time.Millisecond}); err == nil {
		t.Fatal("expected error for nil frame")
	}
}

func TestGIFRejectsEmptyFrame(t *testing.T) {
	t.Parallel()

	enc, err := CreateGIF(t.TempDir() + "/empty.gif")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enc.Discard() }()
	if err = enc.WriteFrame(Frame{Image: image.NewNRGBA(image.Rect(0, 0, 0, 0)), Delay: 10 * time.Millisecond}); err == nil {
		t.Fatal("expected error for empty frame bounds")
	}
}

func TestGIFTransparentPixelsRoundTrip(t *testing.T) {
	t.Parallel()

	src := frameWithTransparentCenter(16, 16)
	path := t.TempDir() + "/transparent.gif"
	if err := writeGIF(path, []image.Image{src}, 100*time.Millisecond, WithOptimize(OptimizeNone)); err != nil {
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
	testutil.PaintRect(flash, image.Rect(20, 20, 28, 28), highlight)

	originals := []image.Image{base, flash, base}
	var buf bytes.Buffer
	if err := writeGIFTo(&buf, originals, 50*time.Millisecond, WithOptimize(OptimizeDirtyRects)); err != nil {
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
	if err := writeGIFTo(&dedupOnly, frames, 50*time.Millisecond, WithOptimize(OptimizeFrames)); err != nil {
		t.Fatal(err)
	}

	var withDisposals bytes.Buffer
	if err := writeGIFTo(&withDisposals, frames, 50*time.Millisecond, WithOptimize(OptimizeFrames|OptimizeDirtyRects)); err != nil {
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
	if err := writeGIFTo(&buf, []image.Image{src}, 700*time.Second); err != nil {
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
			if err := writeGIFTo(&buf, frames, 50*time.Millisecond, tt.opts...); err != nil {
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

func TestGIFDoesNotKeepImage(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/reuse.gif"
	enc, err := CreateGIF(path, WithOptimize(OptimizeNone))
	if err != nil {
		t.Fatal(err)
	}
	// One buffer is reused for every frame, and poisoned once the encoder
	// has returned from it, so an encoder that kept it would see the poison.
	const n = 5
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range n {
		solidFrameInto(img, i+1)
		if err = enc.WriteFrame(Frame{Image: img, Delay: 100 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		for j := range img.Pix {
			img.Pix[j] = 0xAA
		}
	}
	if err = enc.Close(); err != nil {
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
	pal := palette.Default()
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
		full, delta := gifRecording{maxFrames: maxFrames}, gifRecording{maxFrames: maxFrames}
		for _, f := range frames {
			if err := appendRecordingFrame(&full, pal, true, OptimizeFrames, nil, false, false, f, time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if err := appendRecordingFrame(&delta, pal, true, OptimizeFrames|OptimizeDirtyRects, nil, false, false, f, time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		got, want := delta.fullFrames(), full.fullFrames()
		if len(got) != len(want) {
			t.Fatalf("maxFrames=%d: got %d frames, want %d", maxFrames, len(got), len(want))
		}
		for i := range want {
			g, wnt := got[i], want[i]
			if g.Rect != wnt.Rect || !bytes.Equal(g.Pix, wnt.Pix) || delta.delays[i] != full.delays[i] {
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
		if err := appendRecordingFrame(rec, palette.Default(), true, OptimizeAll, nil, false, false, solid(s.X, s.Y, c), time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}

	frames := rec.fullFrames()
	if len(frames) != len(sizes) {
		t.Fatalf("got %d frames, want %d", len(frames), len(sizes))
	}
	for i, f := range frames {
		if got, want := f.Bounds().Size(), sizes[i]; got != want {
			t.Errorf("frame %d size = %v, want %v", i, got, want)
		}
	}
}

// encodeSyntheticGIF encodes deterministic gradient frames and returns the
// encoded bytes.
func encodeSyntheticGIF(tb testing.TB, frames int) []byte {
	tb.Helper()
	var buf bytes.Buffer
	enc, err := NewGIF(&buf)
	if err != nil {
		tb.Fatal(err)
	}
	for i := range frames {
		img := testutil.TerminalFrame(i / 2)
		// small localized change per frame so disposal planning emits sub-rects.
		for x := 100; x < 140; x++ {
			img.SetNRGBA(x, 100+i, color.NRGBA{R: 0xff, A: 0xff})
		}
		if err = enc.WriteFrame(Frame{Image: img, Delay: 50 * time.Millisecond}); err != nil {
			tb.Fatal(err)
		}
	}
	if err = enc.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// TestEncodeAllDeterministic prints the SHA-256 of a synthetic multi-frame GIF
// so before/after output identity can be compared (go test -v -run Deterministic).
func TestEncodeAllDeterministic(t *testing.T) {
	a, b := encodeSyntheticGIF(t, 6), encodeSyntheticGIF(t, 6)
	if !bytes.Equal(a, b) {
		t.Fatal("encode is not deterministic")
	}
	sum := sha256.Sum256(a)
	t.Logf("gif sha256=%s len=%d", hex.EncodeToString(sum[:]), len(a))
}

func BenchmarkEncodeAll(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		encodeSyntheticGIF(b, 8)
	}
}
