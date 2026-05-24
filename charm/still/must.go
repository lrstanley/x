// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"sync"
	"time"
)

// MustPNG is a temporary helper to create a PNG file from an [image.Image].
func MustPNG(img image.Image, path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	err = png.Encode(f, img)
	if err != nil {
		panic(err)
	}
}

// MustGIF is a temporary helper to create a GIF file from a function that returns
// each frame. fn is called at the specified fps and must return fast enough to
// keep up (palettization takes a few ms). fn typically calls [Renderer.Draw] and
// returns the result; [Renderer.Draw] reuses an internal buffer, so fn must not
// retain the returned [image.Image] beyond the call.
func MustGIF(fps int, path string, fn func() image.Image) (closer func()) { //nolint:gocognit // Temporary helper.
	fps = clamp(fps, 1, 120)

	pal := terminalGIFPalette()
	out := &gif.GIF{
		LoopCount: 0,
		Config: image.Config{
			ColorModel: pal,
		},
	}

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Go(func() {
		tick := time.NewTicker(time.Second / time.Duration(fps))
		defer tick.Stop()

		var (
			pm    *image.Paletted
			delay int
		)
		last := time.Now()
		for {
			select {
			case <-tick.C:
				frame := fn()
				if frame == nil {
					panic("MustGIF: nil frame")
				}
				bounds := frame.Bounds()
				if bounds.Empty() {
					panic("MustGIF: empty frame bounds")
				}

				delay = max(1, int(time.Since(last)/(10*time.Millisecond)))
				last = time.Now()

				if pm == nil || !pm.Bounds().Eq(bounds) {
					pm = image.NewPaletted(bounds, pal)
					out.Config.Width = bounds.Dx()
					out.Config.Height = bounds.Dy()
				}

				palettizeInto(pm, frame)

				if n := len(out.Image); n > 0 && bytes.Equal(out.Image[n-1].Pix, pm.Pix) {
					out.Delay[n-1] += delay
					continue
				}

				out.Image = append(out.Image, clonePaletted(pm))
				out.Delay = append(out.Delay, delay)
			case <-done:
				tick.Stop()
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
				if err != nil {
					panic(err)
				}
				err = gif.EncodeAll(f, out)
				if err != nil {
					panic(err)
				}
				err = f.Close()
				if err != nil {
					panic(err)
				}
				return
			}
		}
	})

	closer = sync.OnceFunc(func() {
		close(done)
		wg.Wait()
	})

	return closer
}

// terminalGIFPalette returns the standard xterm-256color palette for GIF
// encoding. Terminal renders mostly use ANSI, cube, and grayscale slots;
// mapping through this palette preserves lipgloss and emulator colors better
// than generic palettes such as Plan9.
func terminalGIFPalette() color.Palette {
	p := make(color.Palette, 256)

	ansi := [16]color.RGBA{
		{0x00, 0x00, 0x00, 0xff},
		{0x80, 0x00, 0x00, 0xff},
		{0x00, 0x80, 0x00, 0xff},
		{0x80, 0x80, 0x00, 0xff},
		{0x00, 0x00, 0x80, 0xff},
		{0x80, 0x00, 0x80, 0xff},
		{0x00, 0x80, 0x80, 0xff},
		{0xc0, 0xc0, 0xc0, 0xff},
		{0x80, 0x80, 0x80, 0xff},
		{0xff, 0x00, 0x00, 0xff},
		{0x00, 0xff, 0x00, 0xff},
		{0xff, 0xff, 0x00, 0xff},
		{0x00, 0x00, 0xff, 0xff},
		{0xff, 0x00, 0xff, 0xff},
		{0x00, 0xff, 0xff, 0xff},
		{0xff, 0xff, 0xff, 0xff},
	}
	for i, c := range ansi {
		p[i] = c
	}

	for r := range 6 {
		for g := range 6 {
			for b := range 6 {
				i := 16 + 36*r + 6*g + b
				p[i] = color.RGBA{
					R: cubeComponent(r),
					G: cubeComponent(g),
					B: cubeComponent(b),
					A: 0xff,
				}
			}
		}
	}

	for i := range 24 {
		v := uint8(8 + i*10) //nolint:gosec // This is bounded.
		p[232+i] = color.RGBA{R: v, G: v, B: v, A: 0xff}
	}

	return p
}

func cubeComponent(v int) uint8 {
	if v == 0 {
		return 0
	}
	return uint8(55 + v*40) //nolint:gosec // This is bounded.
}

// terminalGIFLUT maps 5-bit-per-channel RGB to the nearest xterm-256 palette
// index. It is built once from [terminalGIFPalette] and avoids the per-pixel
// palette search that [draw.Draw] performs when targeting [image.Paletted].
var (
	terminalGIFLUT     [32][32][32]uint8
	initTerminalGIFLUT = sync.OnceFunc(func() {
		pal := terminalGIFPalette()
		for ri := range 32 {
			r := uint8(ri * 255 / 31) //nolint:gosec // ri is bounded.
			for gi := range 32 {
				g := uint8(gi * 255 / 31) //nolint:gosec // gi is bounded.
				for bi := range 32 {
					b := uint8(bi * 255 / 31)                                                            //nolint:gosec // bi is bounded.
					terminalGIFLUT[ri][gi][bi] = uint8(pal.Index(color.RGBA{R: r, G: g, B: b, A: 0xff})) //nolint:gosec // Index is bounded.
				}
			}
		}
	})
)

func palettizeInto(dst *image.Paletted, src image.Image) {
	if nrgba, ok := src.(*image.NRGBA); ok {
		initTerminalGIFLUT()
		palettizeNRGBA(dst, nrgba)
		return
	}
	draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
}

func palettizeNRGBA(dst *image.Paletted, src *image.NRGBA) {
	area := src.Bounds().Intersect(dst.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		sy := (y - src.Rect.Min.Y) * src.Stride
		dy := (y - dst.Rect.Min.Y) * dst.Stride
		for x := area.Min.X; x < area.Max.X; x++ {
			sx := sy + (x-src.Rect.Min.X)*4
			r, g, b := src.Pix[sx], src.Pix[sx+1], src.Pix[sx+2]
			dst.Pix[dy+(x-dst.Rect.Min.X)] = terminalGIFLUT[r>>3][g>>3][b>>3]
		}
	}
}

func clonePaletted(src *image.Paletted) *image.Paletted {
	return &image.Paletted{
		Pix:     bytes.Clone(src.Pix),
		Stride:  src.Stride,
		Rect:    src.Rect,
		Palette: src.Palette,
	}
}
