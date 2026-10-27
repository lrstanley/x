// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// writeGIF writes imgs, each playing for delay, to a GIF at path encoded
// with opts.
func writeGIF(path string, imgs []image.Image, delay time.Duration, opts ...Option) error {
	enc, err := CreateGIF(path, opts...)
	if err != nil {
		return err
	}
	return writeAll(enc, imgs, delay)
}

// writeGIFTo is [writeGIF] writing the GIF to w.
func writeGIFTo(w io.Writer, imgs []image.Image, delay time.Duration, opts ...Option) error {
	enc, err := NewGIF(w, opts...)
	if err != nil {
		return err
	}
	return writeAll(enc, imgs, delay)
}

// writeAll writes imgs to s, each playing for delay, and closes it.
func writeAll(s Sink, imgs []image.Image, delay time.Duration) error {
	for _, img := range imgs {
		if err := s.WriteFrame(Frame{Image: img, Delay: delay}); err != nil {
			return errors.Join(err, s.Discard())
		}
	}
	return s.Close()
}

// gifDecodeFile reads and decodes all frames from a GIF file at path.
func gifDecodeFile(path string) (*gif.GIF, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return gif.DecodeAll(f)
}

// replayGIFFile opens path and reconstructs full frames via [replayGIF].
func replayGIFFile(path string) ([]image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return replayGIF(f)
}

// solidFrame returns an w×h NRGBA image filled with a distinct solid color.
func solidFrame(c, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	solidFrameInto(img, c)
	return img
}

// solidFrameInto fills img with a solid color derived from c.
func solidFrameInto(img *image.NRGBA, c int) {
	col := color.NRGBA{R: uint8(c * 40), A: 0xff}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetNRGBA(x, y, col)
		}
	}
}

// cursorBlinkSequence returns n full-canvas frames with a 2×1 cursor toggling
// at the center — a typical TUI delta pattern for disposal optimization tests.
func cursorBlinkSequence(w, h, n int) []image.Image {
	bg := color.NRGBA{B: 0x80, A: 0xff}
	base := solidNRGBA(w, h, bg)
	cursor := color.NRGBA{R: 0xff, A: 0xff}
	r := image.Rect(w/2, h/2, w/2+2, h/2+1)

	frames := make([]image.Image, n)
	for i := range n {
		img := cloneNRGBA(base)
		if i%2 == 1 {
			testutil.PaintRect(img, r, cursor)
		}
		frames[i] = img
	}
	return frames
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	return &image.NRGBA{Pix: slices.Clone(src.Pix), Stride: src.Stride, Rect: src.Rect}
}

func solidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	testutil.PaintRect(img, img.Bounds(), c)
	return img
}

// assertImagesSimilar checks that got shows want, tolerating GIF
// quantization drift in round-trips: pixels transparent in one are
// transparent in the other, and other pixels differ by at most 24 summed
// over R, G, and B, compared by color alone.
func assertImagesSimilar(t *testing.T, want, got image.Image) {
	t.Helper()
	w, g := testutil.ToNRGBA(want), testutil.ToNRGBA(got)
	if w.Rect != g.Rect {
		t.Fatalf("bounds = %v, want %v", g.Rect, w.Rect)
	}
	for y := w.Rect.Min.Y; y < w.Rect.Max.Y; y++ {
		for x := w.Rect.Min.X; x < w.Rect.Max.X; x++ {
			wc, gc := w.NRGBAAt(x, y), g.NRGBAAt(x, y)
			if (wc.A == 0) != (gc.A == 0) || (wc.A != 0 && rgbDelta(wc, gc) > 24) {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, gc, wc)
			}
		}
	}
}

// replayMismatchPct returns the percentage of non-excluded pixels that differ
// between want and got replays (RGB tolerance 3, skipping bg-colored margin).
func replayMismatchPct(want, got []image.Image, exclude color.NRGBA) (pct float64, pixels int) {
	const tol = 3
	n := min(len(got), len(want))
	totalWrong, totalPixels := 0, 0
	for fi := range n {
		w := testutil.ToNRGBA(want[fi])
		g := testutil.ToNRGBA(got[fi])
		b := w.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				wc := w.NRGBAAt(x, y)
				if colorNear(wc, exclude, 30) {
					continue
				}
				totalPixels++
				if rgbDelta(wc, g.NRGBAAt(x, y)) > tol {
					totalWrong++
				}
			}
		}
	}
	if totalPixels == 0 {
		return 0, 0
	}
	return 100 * float64(totalWrong) / float64(totalPixels), totalPixels
}

func colorNear(a, b color.NRGBA, tol int) bool {
	return a.A == b.A && rgbDelta(a, b) <= tol
}

// rgbDelta is the summed absolute difference of a and b over R, G, and B.
func rgbDelta(a, b color.NRGBA) int {
	d := func(x, y uint8) int { return max(int(x), int(y)) - min(int(x), int(y)) }
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}
