// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"image/color"
	"image/draw"
	"time"

	"github.com/lrstanley/x/term/vt/screen"
)

// logoBitmap is a teacup beside "STEEP" in a 5x7 pixel font, seven pixel rows
// tall. "#" is lit.
var logoBitmap = []string{
	"##########........####.#####.#####.#####.####.",
	"##########.##....#.......#...#.....#.....#...#",
	"##########..#....#.......#...#.....#.....#...#",
	"##########.##.....###....#...####..####..####.",
	".########............#...#...#.....#.....#....",
	"..######.............#...#...#.....#.....#....",
	"############.....####....#...#####.#####.#....",
}

const (
	// logoSteamRows is the height above the logo that steam rises into, in
	// logo pixels.
	logoSteamRows = 6
	// logoMinPixel is the smallest logo pixel size worth drawing. Smaller
	// terminals get the plain text card.
	logoMinPixel = 4

	logoEmptyHold = 300 * time.Millisecond
	logoFillStep  = 50 * time.Millisecond
	logoSteamStep = 150 * time.Millisecond
)

// logoSteamX are the cup columns steam rises from.
var logoSteamX = []int{2, 5, 8}

// logoTea is the tea gradient, top pixel row to bottom. The colors are on the
// xterm-256 cube, so any GIF palette that includes it keeps them exact.
var logoTea = []color.Color{
	screen.Indexed(229), screen.Indexed(228), screen.Indexed(221), screen.Indexed(220),
	screen.Indexed(214), screen.Indexed(208), screen.Indexed(202),
}

var (
	// logoEmpty colors pixel rows the tea has not reached yet.
	logoEmpty = screen.Indexed(237)
	// logoSteam fades from the bottom of a wisp to its top.
	logoSteam = []color.Color{screen.Indexed(245), screen.Indexed(242), screen.Indexed(239), screen.Indexed(236)}
	// logoSway is the sideways drift of a wisp, in half logo pixels.
	logoSway = []int{0, 0, 1, 1, 1, 0, 0, -1, -1, -1}
)

// steepLogo emits the frames of the animated intro: the teacup and wordmark
// start empty and fill with tea from the bottom up, one pixel row per step,
// then steam rises from the cup for d while the test name fades in. It
// emits nothing and returns false when the terminal is too small for the
// logo.
func (ec ExportContext) steepLogo(d time.Duration, emit func(ExportFrame)) bool {
	logoW, logoH := len(logoBitmap[0]), len(logoBitmap)
	base := ec.Card()
	b := base.Bounds()
	px := min(b.Dx()*2/3/logoW, b.Dy()/3/(logoH+logoSteamRows))
	px -= px % 2 // Steam is drawn in half pixels.
	if px < logoMinPixel {
		return false
	}

	// The wordmark sits a pixel above the middle, and the test name below it,
	// a line of its own text height plus a logo pixel away.
	origin := image.Pt(b.Min.X+(b.Dx()-logoW*px)/2, b.Min.Y+(b.Dy()-logoH*px)/2-px)
	name := ec.cardText(ec.cardState(), []CardLine{{Text: ec.h.tb.Name(), Style: screen.Style{Attrs: screen.AttrFaint}}})
	var nameAt image.Rectangle
	if name != nil {
		nb := name.Bounds()
		nameAt = nb.Sub(nb.Min).Add(image.Pt(b.Min.X+(b.Dx()-nb.Dx())/2, origin.Y+logoH*px+px+nb.Dy()))
	}

	frame := func(level, phase int, showName bool, delay time.Duration) ExportFrame {
		img := cloneNRGBA(base)
		if showName && name != nil {
			draw.Draw(img, nameAt, name, name.Bounds().Min, draw.Src)
		}
		dot := func(at image.Point, size int, c color.Color) {
			draw.Draw(img, image.Rect(0, 0, size, size).Add(at), image.NewUniform(c), image.Point{}, draw.Src)
		}
		for y, row := range logoBitmap {
			c := logoTea[y]
			if y < level {
				c = logoEmpty
			}
			for x := range len(row) {
				if row[x] == '#' {
					dot(origin.Add(image.Pt(x*px, y*px)), px, c)
				}
			}
		}
		// Each wisp grows by two half pixels a step, up to the steam rows.
		half := px / 2
		height := min(2*phase, 2*logoSteamRows-2)
		for i, x := range logoSteamX {
			for k := range height {
				at := origin.Add(image.Pt(x*px+logoSway[(k+phase+4*i)%len(logoSway)]*half, -half-k*half))
				dot(at, half, logoSteam[k*len(logoSteam)/height])
			}
		}
		return ExportFrame{Image: img, Delay: delay, Isolated: true}
	}

	emit(frame(logoH, 0, false, logoEmptyHold))
	for level := logoH - 1; level >= 0; level-- {
		emit(frame(level, 0, false, logoFillStep))
	}
	steps := max(int(d/logoSteamStep), 1)
	for phase := 1; phase <= steps; phase++ {
		emit(frame(0, phase, phase >= min(3, steps), logoSteamStep))
	}
	return true
}
