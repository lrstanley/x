// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"image/color"
	"image/draw"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/vt/screen"
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

// Logo returns an effect that shows steep's animated logo before the first
// frame of a recording: a teacup and wordmark fill with tea, then steam rises
// for d while the recording's title ([reel.Context.Title], the test name for
// a [Harness]) fades in below. Terminals too small for the logo get a card of
// "steep" and the title instead (see [effects.CardRenderer.Card]). A d of
// zero or less is no logo. The speed does not scale it.
//
// Its frames are palettized on their own GIF palette ([reel.Frame.Isolated]),
// and have no terminal ([reel.Frame.Cells] is nil), so effects after it, such
// as [effects.Keys], leave them alone. They count toward any GIF frame or
// byte limit.
func Logo(d time.Duration) reel.Effect {
	return logo{d: d}
}

type logo struct{ d time.Duration }

func (e logo) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.d <= 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	cards, err := effects.NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	return func(f reel.Frame, emit func(reel.Frame)) {
		if f.First && !logoFrames(cards, f.Cells, ctx.Title, e.d, emit) {
			title := []effects.Line{{Text: "steep", Style: screen.Style{Attrs: screen.AttrBold}}}
			if ctx.Title != "" {
				title = append(title, effects.Line{Text: ctx.Title, Style: faint})
			}
			emit(reel.Frame{Image: cards.Card(f.Cells, title...), Delay: e.d, Isolated: true})
		}
		emit(f)
	}, nil
}

// logoFrames emits the frames of the logo on cards the size of src: the
// teacup and wordmark start empty and fill with tea from the bottom up, one
// pixel row per step, then steam rises from the cup for d while title fades
// in. It emits nothing and returns false when the card is too small for the
// logo.
func logoFrames(cards *effects.CardRenderer, src *reel.CellFrame, title string, d time.Duration, emit func(reel.Frame)) bool {
	logoW, logoH := len(logoBitmap[0]), len(logoBitmap)
	base := cards.Card(src)
	b := base.Bounds()
	px := min(b.Dx()*2/3/logoW, b.Dy()/3/(logoH+logoSteamRows))
	px -= px % 2 // Steam is drawn in half pixels.
	if px < logoMinPixel {
		return false
	}

	// The wordmark sits a pixel above the middle, and the title below it,
	// a line of its own text height plus a logo pixel away.
	origin := image.Pt(b.Min.X+(b.Dx()-logoW*px)/2, b.Min.Y+(b.Dy()-logoH*px)/2-px)
	var name *image.NRGBA
	if title != "" {
		name = cards.Text(src, effects.Line{Text: title, Style: faint})
	}
	var nameAt image.Rectangle
	if name != nil {
		nb := name.Bounds()
		nameAt = nb.Sub(nb.Min).Add(image.Pt(b.Min.X+(b.Dx()-nb.Dx())/2, origin.Y+logoH*px+px+nb.Dy()))
	}

	frame := func(level, phase int, showName bool, delay time.Duration) reel.Frame {
		img := &image.NRGBA{Pix: slices.Clone(base.Pix), Stride: base.Stride, Rect: base.Rect}
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
		return reel.Frame{Image: img, Delay: delay, Isolated: true}
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
