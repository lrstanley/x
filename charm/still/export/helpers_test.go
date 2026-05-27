// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"io"
	"os"
	"testing"
	"time"
)

// recordViaChannel encodes imgs to path using [WithChannel] and a fixed delay
// per frame. Additional opts (e.g. [WithOptimize]) are applied after the channel.
func recordViaChannel(path string, imgs []image.Image, delay time.Duration, opts ...Option) error {
	ch := make(chan Frame)
	allOpts := append([]Option{WithChannel(ch)}, opts...)
	closer, err := GIF(path, allOpts...)
	if err != nil {
		return err
	}
	for _, img := range imgs {
		ch <- Frame{Image: img, Delay: delay}
	}
	close(ch)
	return closer()
}

// recordViaChannelWriter is [recordViaChannel] writing encoded bytes to w.
func recordViaChannelWriter(w io.Writer, imgs []image.Image, delay time.Duration, opts ...Option) error {
	ch := make(chan Frame)
	allOpts := append([]Option{WithChannel(ch)}, opts...)
	closer, err := WriteGIF(w, allOpts...)
	if err != nil {
		return err
	}
	for _, img := range imgs {
		ch <- Frame{Image: img, Delay: delay}
	}
	close(ch)
	return closer()
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

// replayGIFFile opens path and reconstructs full frames via [ReplayGIF].
func replayGIFFile(path string) ([]image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReplayGIF(f)
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

// palettize converts src through pal using the package capture path.
func palettize(src *image.NRGBA, pal color.Palette) *image.Paletted {
	pm := image.NewPaletted(src.Bounds(), pal)
	palettizeInto(pm, src)
	return pm
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
			paintRect(img, r, cursor)
		}
		frames[i] = img
	}
	return frames
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func paintRect(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func solidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	paintRect(img, img.Bounds(), c)
	return img
}

// assertPalettizedSimilar compares want and got after palettizing with the
// default internal palette — tolerates GIF quantization drift in round-trips.
func assertPalettizedSimilar(t *testing.T, want, got image.Image) {
	t.Helper()
	pal := defaultGIFPalette()
	wantP := palettize(toNRGBA(want), pal)
	gotP := palettize(toNRGBA(got), pal)
	if !bytes.Equal(wantP.Pix, gotP.Pix) {
		t.Fatal("palettized pixels differ")
	}
}

func toNRGBA(img image.Image) *image.NRGBA {
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x, y, img.At(x, y))
		}
	}
	return dst
}

// replayMismatchPct returns the percentage of non-excluded pixels that differ
// between want and got replays (RGB tolerance 3, skipping bg-colored margin).
func replayMismatchPct(want, got []image.Image, exclude color.NRGBA) (pct float64, pixels int) {
	const tol = 3
	n := min(len(got), len(want))
	totalWrong, totalPixels := 0, 0
	for fi := range n {
		w := toNRGBA(want[fi])
		g := toNRGBA(got[fi])
		b := w.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				wc := w.NRGBAAt(x, y)
				if colorNear(wc, exclude, 30) {
					continue
				}
				totalPixels++
				gc := g.NRGBAAt(x, y)
				dr := int(wc.R) - int(gc.R)
				dg := int(wc.G) - int(gc.G)
				db := int(wc.B) - int(gc.B)
				if dr < 0 {
					dr = -dr
				}
				if dg < 0 {
					dg = -dg
				}
				if db < 0 {
					db = -db
				}
				if dr+dg+db > tol {
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
	if a.A != b.A {
		return false
	}
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	if dr < 0 {
		dr = -dr
	}
	if dg < 0 {
		dg = -dg
	}
	if db < 0 {
		db = -db
	}
	return dr+dg+db <= tol
}
