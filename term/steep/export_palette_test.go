// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"testing"
	"time"

	"github.com/lrstanley/x/term/still/export"
)

// TestExportCardLocalPalettes records the key overlay at 56×22 with and
// without intro and outro cards. Cards use a local color table, so the
// overlay frames stay on the shared palette. The local-table byte cost is
// logged: 3 bytes per entry, padded to a power of two, on every card frame.
func TestExportCardLocalPalettes(t *testing.T) {
	t.Parallel()

	plain := recordKeyOverlay(t)
	withCards := recordKeyOverlay(t, WithExportIntro(1500*time.Millisecond), WithExportOutro(time.Second))

	if n := localTableBytes(plain.gif); n != 0 {
		t.Errorf("recording without cards stored %d bytes of local color tables, want 0", n)
	}
	local := localTableBytes(withCards.gif)
	if local == 0 {
		t.Fatal("intro and outro stored no local color tables")
	}
	global, _ := withCards.gif.Config.ColorModel.(color.Palette)
	prevLocal := false
	for i, frame := range withCards.gif.Image {
		localFrame := !usesGlobalTable(frame.Palette, global)
		if localFrame && !powerOfTwo(len(frame.Palette)) {
			t.Errorf("frame %d local table has %d entries, want a power of two", i, len(frame.Palette))
		}
		prevLocal = localFrame
	}
	if !prevLocal {
		t.Error("recording does not end on the outro's local palette")
	}

	plainStat, err := os.Stat(plain.path)
	if err != nil {
		t.Fatal(err)
	}
	cardStat, err := os.Stat(withCards.path)
	if err != nil {
		t.Fatal(err)
	}
	assertLocalTablesPacked(t, withCards.gif)
	t.Logf("key overlay 56x22: %d bytes without cards, %d bytes with cards, local color tables %d bytes (%v entries)", plainStat.Size(), cardStat.Size(), local, localTableSizes(withCards.gif))

	replayed, err := export.ReplayGIF(mustOpen(t, withCards.path))
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(withCards.gif.Image) {
		t.Fatalf("replayed %d frames, want %d", len(replayed), len(withCards.gif.Image))
	}
}

type recordedGIF struct {
	gif  *gif.GIF
	path string
}

func recordKeyOverlay(t *testing.T, opts ...Option) recordedGIF {
	t.Helper()
	h := NewHarness(t, rootTestModel{}, gifOptions(t, append([]Option{
		WithWindowSize(56, 22),
		WithExportKeys(1500 * time.Millisecond),
	}, opts...)...)...)
	h.Type("abc")
	h.Key("enter")
	g := finishGIF(t, h, false)
	return recordedGIF{gif: g, path: h.export.mediaPath("gif")}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func localTableBytes(g *gif.GIF) int {
	n := 0
	for _, size := range localTableSizes(g) {
		n += 3 * size
	}
	return n
}

func localTableSizes(g *gif.GIF) []int {
	global, _ := g.Config.ColorModel.(color.Palette)
	var sizes []int
	for _, frame := range g.Image {
		if usesGlobalTable(frame.Palette, global) {
			continue
		}
		sizes = append(sizes, len(frame.Palette))
	}
	return sizes
}

func usesGlobalTable(frame, global color.Palette) bool {
	if len(frame) != len(global) {
		return false
	}
	extras := 0
	for i := range frame {
		if paletteColorEqual(frame[i], global[i]) {
			continue
		}
		_, _, _, fa := frame[i].RGBA()
		gr, gg, gb, _ := global[i].RGBA()
		if fa == 0 && gr == 0 && gg == 0 && gb == 0 {
			extras++
			continue
		}
		return false
	}
	return extras <= 1
}

func paletteColorEqual(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func powerOfTwo(n int) bool {
	return n > 0 && n&(n-1) == 0
}

// nextPaletteSize is the GIF color-table length for n used colors. Tables are
// at least 2 entries and round up to a power of two.
func nextPaletteSize(n int) int {
	if n < 2 {
		return 2
	}
	size := 1
	for size < n {
		size <<= 1
	}
	return size
}

// assertLocalTablesPacked checks each run of frames that share a local table
// against the smallest power of two that fits the colors the run uses.
func assertLocalTablesPacked(t *testing.T, g *gif.GIF) {
	t.Helper()
	global, _ := g.Config.ColorModel.(color.Palette)
	var run []*image.Paletted
	flush := func() {
		t.Helper()
		if len(run) == 0 {
			return
		}
		var seen [256]bool
		n := 0
		for _, frame := range run {
			for _, idx := range frame.Pix {
				if int(idx) >= len(frame.Palette) {
					t.Fatalf("pixel index %d outside palette of %d", idx, len(frame.Palette))
				}
				if !seen[idx] {
					seen[idx] = true
					n++
				}
			}
		}
		if got, want := len(run[0].Palette), nextPaletteSize(n); got != want {
			t.Errorf("local table has %d entries for %d colors, want %d", got, n, want)
		}
		run = nil
	}
	for _, frame := range g.Image {
		if usesGlobalTable(frame.Palette, global) {
			flush()
			continue
		}
		if len(run) > 0 && !paletteEqual(frame.Palette, run[0].Palette) {
			flush()
		}
		run = append(run, frame)
	}
	flush()
}

func paletteEqual(a, b color.Palette) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !paletteColorEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}
