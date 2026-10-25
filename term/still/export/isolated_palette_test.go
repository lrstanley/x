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
	"time"
)

func distinctColors(n int) []color.NRGBA {
	out := make([]color.NRGBA, 0, n)
	for r := 0; r < 256 && len(out) < n; r += 16 {
		for g := 0; g < 256 && len(out) < n; g += 16 {
			for b := 0; b < 256 && len(out) < n; b += 16 {
				out = append(out, color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}) //nolint:gosec // bounded.
			}
		}
	}
	return out
}

// offGridColors are far from the 16-step grid of [distinctColors], so a shared
// palette that already holds that grid cannot match them within adaptiveMatchDist.
func offGridColors(n int) []color.NRGBA {
	out := make([]color.NRGBA, n)
	for i := range out {
		out[i] = color.NRGBA{
			R: 8,
			G: uint8((i%16)*16 + 8), //nolint:gosec // bounded.
			B: uint8((i/16)*16 + 8), //nolint:gosec // bounded.
			A: 0xff,
		}
	}
	return out
}

func solidColorsFrame(colors []color.NRGBA, block int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, block*len(colors), block))
	for i, c := range colors {
		paintRect(img, image.Rect(i*block, 0, (i+1)*block, block), c)
	}
	return img
}

func encodeFrames(t *testing.T, frames []Frame, opts ...Option) (g *gif.GIF, raw []byte) {
	t.Helper()
	ch := make(chan Frame)
	var buf bytes.Buffer
	enc, err := NewGIF(&buf, opts...)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Record(enc, WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		ch <- frame
	}
	close(ch)
	if err = rec.Close(); err != nil {
		t.Fatal(err)
	}
	g, err = gif.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return g, buf.Bytes()
}

func colorDist(a, b color.Color) int {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	if aa == 0 || ba == 0 {
		if aa == 0 && ba == 0 {
			return 0
		}
		return 255 * 255 * 3
	}
	dr := int(ar>>8) - int(br>>8)
	dg := int(ag>>8) - int(bg>>8)
	db := int(ab>>8) - int(bb>>8)
	return dr*dr + dg*dg + db*db
}

func assertReplayClose(t *testing.T, src, got []image.Image) {
	t.Helper()
	if len(got) != len(src) {
		t.Fatalf("replayed %d frames, want %d", len(got), len(src))
	}
	for i := range src {
		sb, gb := src[i].Bounds(), got[i].Bounds()
		if sb.Dx() != gb.Dx() || sb.Dy() != gb.Dy() {
			t.Fatalf("frame %d size %v, want %v", i, gb.Size(), sb.Size())
		}
		for y := range sb.Dy() {
			for x := range sb.Dx() {
				d := colorDist(got[i].At(gb.Min.X+x, gb.Min.Y+y), src[i].At(sb.Min.X+x, sb.Min.Y+y))
				if d > adaptiveMatchDist {
					t.Fatalf("frame %d pixel (%d,%d) distance %d, want <= %d", i, x, y, d, adaptiveMatchDist)
				}
			}
		}
	}
}

func usesGlobalPalette(frame, global color.Palette) bool {
	if len(frame) != len(global) {
		return false
	}
	extras := 0
	for i := range frame {
		if colorDist(frame[i], global[i]) == 0 {
			continue
		}
		_, _, _, fa := frame[i].RGBA()
		gr, gg, gb, _ := global[i].RGBA()
		// GIF drops alpha. A transparent slot comes back as black in the
		// global table and transparent on the frame that uses it.
		if fa == 0 && gr>>8 == 0 && gg>>8 == 0 && gb>>8 == 0 {
			extras++
			continue
		}
		return false
	}
	return extras <= 1
}

func TestIsolatedFramesSkipSharedPalette(t *testing.T) {
	t.Parallel()

	ap := newAdaptivePalette(true)
	rec := &gifRecording{adaptive: ap, config: image.Config{ColorModel: ap.pal}}
	intro := solidColorsFrame(offGridColors(40), 8)
	if err := appendRecordingFrame(rec, nil, false, OptimizeAll, nil, false, true, intro, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if ap.n != 0 {
		t.Fatalf("shared palette used %d slots after an isolated frame, want 0", ap.n)
	}
	if rec.isolated == nil || rec.isolated.n == 0 {
		t.Fatal("isolated frame did not build its own palette")
	}

	shared := solidColorsFrame(distinctColors(255), 8)
	if err := appendRecordingFrame(rec, nil, false, OptimizeAll, nil, false, false, shared, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if ap.n != adaptiveSlots {
		t.Fatalf("shared palette used %d slots, want %d", ap.n, adaptiveSlots)
	}
}

func TestIsolatedRunAroundSharedFrames(t *testing.T) {
	t.Parallel()

	const w, h = 32, 24
	introA := solidNRGBA(w, h, offGridColors(1)[0])
	introB := cloneNRGBA(introA)
	paintRect(introB, image.Rect(2, 2, 6, 6), offGridColors(2)[1])
	midA := solidNRGBA(w, h, distinctColors(1)[0])
	midB := cloneNRGBA(midA)
	paintRect(midB, image.Rect(8, 4, 14, 10), distinctColors(2)[1])
	outro := solidNRGBA(w, h, color.NRGBA{R: 1, G: 2, B: 3, A: 0xff})

	delay := 40 * time.Millisecond
	for _, tc := range []struct {
		name   string
		frames []Frame
	}{
		{"isolated then shared", []Frame{
			{Image: introA, Delay: delay, Isolated: true},
			{Image: introB, Delay: delay, Isolated: true},
			{Image: midA, Delay: delay},
			{Image: midB, Delay: delay},
		}},
		{"shared then isolated", []Frame{
			{Image: midA, Delay: delay},
			{Image: midB, Delay: delay},
			{Image: outro, Delay: delay, Isolated: true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, raw := encodeFrames(t, tc.frames)
			replayed, err := ReplayGIF(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			src := make([]image.Image, len(tc.frames))
			for i, frame := range tc.frames {
				src[i] = frame.Image
			}
			assertReplayClose(t, src, replayed)

			global, _ := g.Config.ColorModel.(color.Palette)
			canvas := image.Rect(0, 0, g.Config.Width, g.Config.Height)
			prevLocal := false
			for i, frame := range g.Image {
				local := !usesGlobalPalette(frame.Palette, global)
				if i > 0 && local != prevLocal && !frame.Bounds().Eq(canvas) {
					t.Errorf("frame %d after a palette change is %v, want full canvas %v", i, frame.Bounds(), canvas)
				}
				prevLocal = local
			}
		})
	}
}

func TestOutroExactWhenSharedPaletteFull(t *testing.T) {
	t.Parallel()

	shared := solidColorsFrame(distinctColors(255), 8)
	b := shared.Bounds()
	outroColor := color.NRGBA{R: 1, G: 2, B: 3, A: 0xff}
	outro := solidNRGBA(b.Dx(), b.Dy(), outroColor)
	intro := solidNRGBA(b.Dx(), b.Dy(), offGridColors(1)[0])
	frames := []Frame{
		{Image: intro, Delay: 40 * time.Millisecond, Isolated: true},
		{Image: shared, Delay: 40 * time.Millisecond},
		{Image: outro, Delay: 40 * time.Millisecond, Isolated: true},
	}
	_, raw := encodeFrames(t, frames)
	replayed, err := ReplayGIF(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	assertReplayClose(t, []image.Image{intro, shared, outro}, replayed)
	if d := colorDist(replayed[2].At(0, 0), outroColor); d != 0 {
		t.Fatalf("outro color distance %d, want 0", d)
	}
}

func TestDedupAcrossPaletteChange(t *testing.T) {
	t.Parallel()

	img := solidFrame(1, 8, 8)
	delay := 40 * time.Millisecond
	for _, optimize := range []OptimizeFlags{OptimizeFrames, OptimizeAll} {
		same := encodeCount(t, []Frame{
			{Image: img, Delay: delay, Isolated: true},
			{Image: img, Delay: delay, Isolated: true},
		}, WithOptimize(optimize))
		if same != 1 {
			t.Errorf("optimize %v: identical isolated frames = %d, want 1", optimize, same)
		}
		// Dedup compares indices, so it keeps both. Disposal optimization
		// compares colors across the change and finds nothing to write.
		across := encodeCount(t, []Frame{
			{Image: img, Delay: delay},
			{Image: img, Delay: delay, Isolated: true},
		}, WithOptimize(optimize))
		want := 2
		if optimize.has(OptimizeDirtyRects) {
			want = 1
		}
		if across != want {
			t.Errorf("optimize %v: frames across a palette change = %d, want %d", optimize, across, want)
		}
	}
}

func encodeCount(t *testing.T, frames []Frame, opts ...Option) int {
	t.Helper()
	g, _ := encodeFrames(t, frames, opts...)
	return len(g.Image)
}

func TestDisposalDiffsAcrossPalettesByColor(t *testing.T) {
	t.Parallel()

	const w, h = 32, 32
	base := solidFrame(1, w, h)
	changed := cloneNRGBA(base)
	changed.SetNRGBA(4, 4, color.NRGBA{R: 0xff, G: 0x80, B: 0x40, A: 0xff})
	delay := 40 * time.Millisecond

	across, raw := encodeFrames(t, []Frame{
		{Image: base, Delay: delay},
		{Image: changed, Delay: delay, Isolated: true},
	})
	if got, want := across.Image[1].Bounds(), image.Rect(4, 4, 5, 5); !got.Eq(want) {
		t.Fatalf("frame after a palette change is %v, want only the pixel that changed color %v", got, want)
	}
	if samePalette(across.Image[0].Palette, across.Image[1].Palette) {
		t.Fatal("isolated frame shares the first frame's palette")
	}
	replayed, err := ReplayGIF(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if pct, _ := replayMismatchPct([]image.Image{base, changed}, replayed, color.NRGBA{}); pct != 0 {
		t.Fatalf("replay differs from the source in %.2f%% of pixels", pct)
	}

	within, _ := encodeFrames(t, []Frame{
		{Image: base, Delay: delay, Isolated: true},
		{Image: changed, Delay: delay, Isolated: true},
	})
	if got, want := within.Image[1].Bounds(), image.Rect(4, 4, 5, 5); !got.Eq(want) {
		t.Fatalf("same-palette isolated frame is %v, want sub-rect %v", got, want)
	}
}

func TestIsolatedPaletteCompactsToPowerOfTwo(t *testing.T) {
	t.Parallel()

	// A shared frame first, so the isolated one is not the only palette and
	// does not become the global table.
	const colors = 40
	frame := solidColorsFrame(offGridColors(colors), 8)
	g, _ := encodeFrames(t, []Frame{
		{Image: solidNRGBA(frame.Rect.Dx(), frame.Rect.Dy(), color.NRGBA{A: 0xff}), Delay: 40 * time.Millisecond},
		{Image: frame, Delay: 40 * time.Millisecond, Isolated: true},
	})
	got := len(g.Image[1].Palette)
	want := 1
	for want < colors {
		want <<= 1
	}
	if got != want {
		t.Fatalf("local table has %d entries, want %d for %d colors", got, want, colors)
	}
	global, _ := g.Config.ColorModel.(color.Palette)
	if usesGlobalPalette(g.Image[1].Palette, global) {
		t.Fatal("isolated frame used the global color table")
	}
}
