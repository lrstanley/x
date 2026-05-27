// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/charm/still/fonts"
)

func TestPowerlineIconRendering(t *testing.T) {
	t.Parallel()

	const icon = "\ue0b0"
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	palette := WithPalette(Palette{DefaultForeground: fg, DefaultBackground: bg})

	t.Run("fills cell height", func(t *testing.T) {
		t.Parallel()

		scr := newTestScreen(1, 1)
		scr.SetCell(0, 0, &uv.Cell{Content: icon, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		d := MustNew(WithFontSizePt(20), palette)
		img := drawNRGBA(t, d, scr)
		cell := d.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		ink, ok := inkBounds(img, cell, bg)
		if !ok {
			t.Fatal("powerline separator produced no ink")
		}
		if ink.Min.Y > cell.Min.Y+1 || ink.Max.Y < cell.Max.Y-1 {
			t.Fatalf("separator should fill cell height: ink=%v cell=%v", ink, cell)
		}
	})

	t.Run("bottom ink survives next row", func(t *testing.T) {
		t.Parallel()

		scr := newTestScreen(1, 2)
		scr.SetCell(0, 0, &uv.Cell{Content: icon, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		scr.SetCell(0, 1, &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

		d := MustNew(
			WithCellHeight(-0.25),
			WithNow(func() time.Time { return time.Unix(1, 0) }),
			palette,
		)
		img := drawNRGBA(t, d, scr)
		cell0 := d.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		rows := min(3, cell0.Dy())
		nonBg := 0
		for y := cell0.Max.Y - rows; y < cell0.Max.Y; y++ {
			for x := cell0.Min.X; x < cell0.Max.X; x++ {
				if img.NRGBAAt(x, y) != bg {
					nonBg++
				}
			}
		}
		if nonBg < max(2, rows*cell0.Dx()/10) {
			t.Fatalf("powerline icon bottom ink = %d (rows=%d cell=%#v); overflow likely erased by next row",
				nonBg, rows, cell0)
		}
	})
}

func TestIconHeightRendering(t *testing.T) {
	t.Parallel()

	const nerdIcon = "\uf0ac"
	const terminalGraphic = "\ue0b0"
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	palette := WithPalette(Palette{DefaultForeground: fg, DefaultBackground: bg})
	fontOpts := []Option{WithFontSizePt(20), palette}

	t.Run("scales single cell nerd icon", func(t *testing.T) {
		t.Parallel()

		scr := newTestScreen(1, 1)
		scr.SetCell(0, 0, &uv.Cell{Content: nerdIcon, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

		base := MustNew(fontOpts...)
		scaled := MustNew(append(fontOpts, WithIconHeight(0.5))...)
		singleScaled := MustNew(append(fontOpts, WithIconHeightSingle(0.5))...)

		baseCell := base.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		scaledCell := scaled.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		if baseCell != scaledCell {
			t.Fatalf("WithIconHeight changed cell bounds: %v -> %v", baseCell, scaledCell)
		}
		singleScaledCell := singleScaled.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		if baseCell != singleScaledCell {
			t.Fatalf("WithIconHeightSingle changed cell bounds: %v -> %v", baseCell, singleScaledCell)
		}

		baseBounds, ok := inkBounds(drawNRGBA(t, base, scr), baseCell, bg)
		if !ok {
			t.Fatal("base icon produced no ink")
		}
		scaledBounds, ok := inkBounds(drawNRGBA(t, scaled, scr), scaledCell, bg)
		if !ok {
			t.Fatal("scaled icon produced no ink")
		}
		if scaledBounds.Dy() >= baseBounds.Dy() {
			t.Fatalf("WithIconHeight(0.5) ink height = %d, want less than default %d", scaledBounds.Dy(), baseBounds.Dy())
		}
		singleScaledBounds, ok := inkBounds(drawNRGBA(t, singleScaled, scr), singleScaledCell, bg)
		if !ok {
			t.Fatal("single-cell scaled icon produced no ink")
		}
		if singleScaledBounds.Dy() >= baseBounds.Dy() {
			t.Fatalf("WithIconHeightSingle(0.5) ink height = %d, want less than default %d", singleScaledBounds.Dy(), baseBounds.Dy())
		}
	})

	t.Run("default fits icon height metric", func(t *testing.T) {
		t.Parallel()

		scr := newTestScreen(1, 1)
		scr.SetCell(0, 0, &uv.Cell{Content: nerdIcon, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

		d := MustNew(fontOpts...)
		ctx := d.contextLocked(image.Point{}, scr)
		bounds, ok := inkBounds(drawNRGBA(t, d, scr), ctx.CellBounds(0, 0), bg)
		if !ok {
			t.Fatal("icon produced no ink")
		}
		textScr := newTestScreen(1, 1)
		textScr.SetCell(0, 0, &uv.Cell{Content: "H", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		textBounds, ok := inkBounds(drawNRGBA(t, d, textScr), ctx.CellBounds(0, 0), bg)
		if !ok {
			t.Fatal("text glyph produced no ink")
		}
		if bounds.Dy() < textBounds.Dy() {
			t.Fatalf("default icon ink height = %d, want at least text cap height %d", bounds.Dy(), textBounds.Dy())
		}
		if bounds.Dy() > ctx.Metrics().IconHeightSingle.Int() {
			t.Fatalf("default icon ink height = %d, want <= IconHeightSingle %d", bounds.Dy(), ctx.Metrics().IconHeightSingle)
		}
	})

	t.Run("does not shrink terminal graphics", func(t *testing.T) {
		t.Parallel()

		scr := newTestScreen(1, 1)
		scr.SetCell(0, 0, &uv.Cell{Content: terminalGraphic, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

		base := MustNew(fontOpts...)
		scaled := MustNew(append(fontOpts, WithIconHeight(0.5))...)
		baseCell := base.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		scaledCell := scaled.contextLocked(image.Point{}, scr).CellBounds(0, 0)
		if baseCell != scaledCell {
			t.Fatalf("WithIconHeight changed cell bounds: %v -> %v", baseCell, scaledCell)
		}
		baseBounds, ok := inkBounds(drawNRGBA(t, base, scr), baseCell, bg)
		if !ok {
			t.Fatal("base terminal graphic produced no ink")
		}
		scaledBounds, ok := inkBounds(drawNRGBA(t, scaled, scr), scaledCell, bg)
		if !ok {
			t.Fatal("scaled terminal graphic produced no ink")
		}
		if scaledBounds != baseBounds {
			t.Fatalf("terminal graphic bounds changed with WithIconHeight: %v -> %v", baseBounds, scaledBounds)
		}
	})
}

func TestSyntheticNerdIconStylesRenderDifferentlyAndFit(t *testing.T) {
	t.Parallel()

	const icon = "\uf0ac"
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	styles := map[string]uv.Style{
		"regular":     {},
		"bold":        {Attrs: uv.AttrBold},
		"italic":      {Attrs: uv.AttrItalic},
		"bold italic": {Attrs: uv.AttrBold | uv.AttrItalic},
	}

	signatures := map[string]string{}
	for name, style := range styles {
		scr := newTestScreen(1, 1)
		scr.SetCell(0, 0, &uv.Cell{Content: icon, Width: 1, Style: style})
		d := MustNew(
			WithFontSizePt(20),
			WithPalette(Palette{DefaultForeground: fg, DefaultBackground: bg}),
		)
		img := drawNRGBA(t, d, scr)
		ctx := d.contextLocked(image.Point{}, scr)
		cell := ctx.CellBounds(0, 0)
		bounds, ok := inkBounds(img, cell, bg)
		if !ok {
			t.Fatalf("%s icon produced no ink", name)
		}
		if bounds.Dy() > ctx.Metrics().IconHeightSingle.Int() {
			t.Fatalf("%s icon ink height = %d, want <= IconHeightSingle %d", name, bounds.Dy(), ctx.Metrics().IconHeightSingle)
		}
		signatures[name] = cellInkSignature(img, cell)
	}
	for _, name := range []string{"bold", "italic", "bold italic"} {
		if signatures[name] == signatures["regular"] {
			t.Fatalf("%s icon rendered the same as regular", name)
		}
	}
}

func TestRendererBoundsNormalizePaddingAndScrollbarGutter(t *testing.T) {
	t.Parallel()

	scr := offsetScreen{
		bounds: image.Rect(5, 7, 7, 8),
		cells: map[image.Point]*uv.Cell{
			image.Pt(5, 7): {Content: "a", Width: 1},
			image.Pt(6, 7): {Content: "b", Width: 1},
		},
	}
	var areas []image.Rectangle
	d := MustNew(
		WithPadding(2),
		WithMargin(3, color.NRGBA{R: 0xff, A: 0xff}),
		WithScrollbar(true),
		WithCellFgDrawer(func(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell) {
			areas = append(areas, area)
			DrawCellFg(ctx, img, area, cell)
		}),
	)

	_ = d.Draw(scr)

	cell := d.CellSize()
	wantSize := image.Pt(cell.X*2+1+2*2+3*2, cell.Y+2*2+3*2)
	if got := d.Size(scr); got != wantSize {
		t.Fatalf("Size() = %v, want %v", got, wantSize)
	}
	if len(areas) != 2 {
		t.Fatalf("drawn cell areas = %d, want 2", len(areas))
	}
	if got, want := areas[0].Min, image.Pt(5, 5); got != want {
		t.Fatalf("first cell min = %v, want normalized %v", got, want)
	}
	if got, want := areas[1].Min, image.Pt(5+cell.X, 5); got != want {
		t.Fatalf("second cell min = %v, want %v", got, want)
	}
}

func TestRendererWideCellsCoverContinuations(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{B: 0xff, A: 0xff}
	green := color.NRGBA{G: 0xff, A: 0xff}
	scr := newTestScreen(3, 1)
	scr.SetCell(0, 0, &uv.Cell{Content: "w", Width: 2, Style: uv.Style{Bg: blue}})
	scr.SetCell(2, 0, &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: green}})
	d := MustNew()
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)

	if got := sample(img, ctx.CellBounds(1, 0)); got != blue {
		t.Fatalf("continuation cell pixel = %#v, want wide-cell background %#v", got, blue)
	}
	if got := sample(img, ctx.CellBounds(2, 0)); got != green {
		t.Fatalf("next cell pixel = %#v, want %#v", got, green)
	}
}

func TestRendererBoxDrawingGlyphsHaveDistinctShapes(t *testing.T) {
	t.Parallel()

	glyphs := []string{"┌", "─", "┐", "│", "└", "┘"}
	scr := newTestScreen(len(glyphs), 1)
	for x, glyph := range glyphs {
		scr.SetCell(x, 0, &uv.Cell{
			Content: glyph,
			Width:   1,
			Style: uv.Style{
				Fg: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
				Bg: color.NRGBA{A: 0xff},
			},
		})
	}

	d := MustNew()
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)
	signatures := map[string]struct{}{}
	for x := range glyphs {
		signatures[cellInkSignature(img, ctx.CellBounds(x, 0))] = struct{}{}
	}
	if len(signatures) < 3 {
		t.Fatalf("box drawing glyphs rendered as only %d distinct shapes, want at least 3", len(signatures))
	}
}

func TestRendererContiguousBoxDrawingLinesAvoidSeams(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	d := MustNew(WithFontSizePt(14))

	t.Run("vertical", func(t *testing.T) {
		t.Parallel()

		rows := 4
		scr := newTestScreen(1, rows)
		for y := range rows {
			scr.SetCell(0, y, &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		}
		img := drawNRGBA(t, d, scr)
		ctx := d.contextLocked(image.Point{}, scr)
		cx := ctx.CellBounds(0, 0).Min.X + ctx.Metrics().CellWidth.Int()/2
		midY := ctx.CellBounds(0, 0).Min.Y + 10
		juncY := ctx.CellBounds(0, 1).Min.Y
		if core := img.NRGBAAt(cx, juncY).R; core < 100 {
			t.Fatalf("vertical line gap at junction core R=%d, want >= 100", core)
		}
		mid := img.NRGBAAt(cx-1, midY).R
		junc := img.NRGBAAt(cx-1, juncY).R
		if delta := int(junc) - int(mid); delta > 8 || delta < -8 {
			t.Fatalf("vertical fringe delta at junction = %d (mid=%d junc=%d), want within ±8", delta, mid, junc)
		}
	})

	t.Run("horizontal", func(t *testing.T) {
		t.Parallel()

		cols := 4
		scr := newTestScreen(cols, 1)
		for x := range cols {
			scr.SetCell(x, 0, &uv.Cell{Content: "─", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		}
		img := drawNRGBA(t, d, scr)
		ctx := d.contextLocked(image.Point{}, scr)
		cy := ctx.CellBounds(0, 0).Min.Y + ctx.Metrics().CellHeight.Int()/2
		midX := ctx.CellBounds(0, 0).Min.X + 5
		juncX := ctx.CellBounds(1, 0).Min.X
		if core := img.NRGBAAt(juncX, cy).R; core < 100 {
			t.Fatalf("horizontal line gap at junction core R=%d, want >= 100", core)
		}
		mid := img.NRGBAAt(midX, cy+1).R
		junc := img.NRGBAAt(juncX, cy+1).R
		if delta := int(junc) - int(mid); delta > 8 || delta < -8 {
			t.Fatalf("horizontal fringe delta at junction = %d (mid=%d junc=%d), want within ±8", delta, mid, junc)
		}
	})
}

func TestRendererRoundedCornerBoxDrawingAvoidsOutsideArtifacts(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	d := MustNew(WithFontSizePt(40), WithCellWidth(-0.1))

	scr := newTestScreen(2, 2)
	scr.SetCell(0, 0, &uv.Cell{Content: "╭", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
	scr.SetCell(1, 0, &uv.Cell{Content: "─", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
	scr.SetCell(0, 1, &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)
	corner := ctx.CellBounds(0, 0)
	bar := ctx.CellBounds(0, 1)
	cx := corner.Min.X + ctx.Metrics().CellWidth.Int()/2
	juncY := bar.Min.Y

	if core := img.NRGBAAt(cx, juncY).R; core < 100 {
		t.Fatalf("rounded corner vertical junction core R=%d, want >= 100", core)
	}

	barMidY := bar.Min.Y + bar.Dy()/2
	juncW := boxStrokeWidthAtRow(img, corner, juncY, 100)
	barW := boxStrokeWidthAtRow(img, bar, barMidY, 100)
	if juncW == 0 {
		t.Fatalf("no vertical stroke at junction y=%d", juncY)
	}
	if barW == 0 {
		t.Fatalf("no vertical stroke in bar cell at y=%d", barMidY)
	}
	if juncW > barW+2 {
		t.Fatalf("junction stroke width = %d, bar width = %d; want junction not bloated", juncW, barW)
	}

	// Outside the intended L-shape: left of the corner stroke column.
	outsideX := corner.Min.X + 1
	outsideY := corner.Min.Y + 2
	if img.NRGBAAt(outsideX, outsideY).R > 32 {
		t.Fatalf("spurious ink outside corner at (%d,%d) R=%d, want <= 32",
			outsideX, outsideY, img.NRGBAAt(outsideX, outsideY).R)
	}

	// No spurious vertical stroke above the corner cell.
	for y := corner.Min.Y - 8; y < corner.Min.Y; y++ {
		if img.NRGBAAt(cx, y).R > 32 {
			t.Fatalf("spurious ink above corner at (%d,%d) R=%d, want <= 32", cx, y, img.NRGBAAt(cx, y).R)
		}
	}
}

func TestRendererBoxCornerVerticalJunctionContinuity(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	d := MustNew(WithFontSizePt(40), WithCellWidth(-0.1))

	type cornerCase struct {
		name      string
		glyph     string
		cornerPos image.Point
		barPos    image.Point
	}
	cases := []cornerCase{
		{"top-left", "╭", image.Pt(0, 0), image.Pt(0, 1)},
		{"top-right", "╮", image.Pt(1, 0), image.Pt(1, 1)},
		{"bottom-left", "╰", image.Pt(0, 1), image.Pt(0, 0)},
		{"bottom-right", "╯", image.Pt(1, 1), image.Pt(1, 0)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scr := newTestScreen(2, 2)
			scr.SetCell(tc.cornerPos.X, tc.cornerPos.Y, &uv.Cell{Content: tc.glyph, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
			scr.SetCell(tc.barPos.X, tc.barPos.Y, &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

			img := drawNRGBA(t, d, scr)
			ctx := d.contextLocked(image.Point{}, scr)
			cornerCell := ctx.CellBounds(tc.cornerPos.X, tc.cornerPos.Y)
			barCell := ctx.CellBounds(tc.barPos.X, tc.barPos.Y)

			juncY := barCell.Min.Y
			if tc.cornerPos.Y > tc.barPos.Y {
				juncY = cornerCell.Min.Y
			}

			strokeX := cornerCell.Min.X + ctx.Metrics().CellWidth.Int()/2
			for x := cornerCell.Min.X; x < cornerCell.Max.X; x++ {
				if img.NRGBAAt(x, juncY-1).R > 50 || img.NRGBAAt(x, juncY).R > 50 {
					strokeX = x
					break
				}
			}

			if core := img.NRGBAAt(strokeX, juncY).R; core < 100 {
				t.Fatalf("vertical junction at (%d,%d) R=%d, want >= 100", strokeX, juncY, core)
			}
			if strings.HasPrefix(tc.name, "top-") {
				if bridge := img.NRGBAAt(strokeX, juncY-1).R; bridge < 100 {
					t.Fatalf("top corner bridge at (%d,%d) R=%d, want >= 100", strokeX, juncY-1, bridge)
				}
			}

			barMidY := barCell.Min.Y + barCell.Dy()/2
			juncW := boxStrokeWidthAtRow(img, cornerCell, juncY, 100)
			barW := boxStrokeWidthAtRow(img, barCell, barMidY, 100)
			if juncW > barW+2 {
				t.Fatalf("junction stroke width = %d, bar width = %d; want junction not bloated", juncW, barW)
			}
		})
	}
}

func TestRendererFiraRoundedCornerBorderWithMargin(t *testing.T) {
	t.Parallel()

	family, err := fonts.LoadFamily(fonts.FontFamilyNames{
		Regular: "FiraMonoNerdFontMono-Regular",
	})
	if err != nil {
		t.Skip(err)
	}

	fg := color.NRGBA{R: 0x88, G: 0x88, B: 0x88, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	margin := color.NRGBA{R: 0x81, G: 0x2c, B: 0xd1, A: 0xff}
	d := MustNew(
		WithFontFamily(family),
		WithFontSizePt(Pt(40)),
		WithCellWidth(-0.1),
		WithBorderRadius(10),
		WithMargin(40, margin),
		WithPadding(10),
	)

	scr := newTestScreen(2, 2)
	scr.SetCell(0, 0, &uv.Cell{Content: "╭", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
	scr.SetCell(1, 0, &uv.Cell{Content: "─", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
	scr.SetCell(0, 1, &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})

	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)
	corner := ctx.CellBounds(0, 0)
	bar := ctx.CellBounds(0, 1)
	cx := corner.Min.X + ctx.Metrics().CellWidth.Int()/2
	juncY := bar.Min.Y

	if core := img.NRGBAAt(cx, juncY).R; core < 100 {
		t.Fatalf("fira corner vertical junction core R=%d, want >= 100", core)
	}

	barMidY := bar.Min.Y + bar.Dy()/2
	juncW := boxStrokeWidthAtRow(img, corner, juncY, 100)
	barW := boxStrokeWidthAtRow(img, bar, barMidY, 100)
	if juncW > barW+2 {
		t.Fatalf("fira junction stroke width = %d, bar width = %d; want junction not bloated", juncW, barW)
	}

	for y := corner.Min.Y - 12; y < corner.Min.Y; y++ {
		for x := corner.Min.X - 12; x < corner.Min.X; x++ {
			c := img.NRGBAAt(x, y)
			if c == margin {
				continue
			}
			if c.R > 32 && c.R < 200 && c.G == c.R && c.B == c.R {
				t.Fatalf("box artifact in margin at (%d,%d) = %#v (corner cell min=%v)", x, y, c, corner.Min)
			}
		}
	}
}

func TestRendererContiguousBoxDrawingLinesAvoidSeamsLargeCell(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	d := MustNew(WithFontSizePt(40), WithCellWidth(-0.1))

	t.Run("vertical", func(t *testing.T) {
		t.Parallel()

		rows := 4
		scr := newTestScreen(1, rows)
		for y := range rows {
			scr.SetCell(0, y, &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		}
		img := drawNRGBA(t, d, scr)
		ctx := d.contextLocked(image.Point{}, scr)
		cx := ctx.CellBounds(0, 0).Min.X + ctx.Metrics().CellWidth.Int()/2
		juncY := ctx.CellBounds(0, 1).Min.Y
		if core := img.NRGBAAt(cx, juncY).R; core < 100 {
			t.Fatalf("large vertical line gap at junction core R=%d, want >= 100", core)
		}
	})

	t.Run("horizontal", func(t *testing.T) {
		t.Parallel()

		cols := 4
		scr := newTestScreen(cols, 1)
		for x := range cols {
			scr.SetCell(x, 0, &uv.Cell{Content: "─", Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
		}
		img := drawNRGBA(t, d, scr)
		ctx := d.contextLocked(image.Point{}, scr)
		cy := ctx.CellBounds(0, 0).Min.Y + ctx.Metrics().CellHeight.Int()/2
		juncX := ctx.CellBounds(1, 0).Min.X
		if core := img.NRGBAAt(juncX, cy).R; core < 100 {
			t.Fatalf("large horizontal line gap at junction core R=%d, want >= 100", core)
		}
	})
}

func TestRendererColorAttributes(t *testing.T) {
	t.Parallel()

	red := color.NRGBA{R: 0xff, A: 0xff}
	blue := color.NRGBA{B: 0xff, A: 0xff}
	gray := color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}

	tests := map[string]struct {
		cell       *uv.Cell
		wantBG     color.NRGBA
		wantLine   color.NRGBA
		lineSample func(Context, image.Rectangle) image.Point
	}{
		"reverse": {
			cell: &uv.Cell{
				Content: " ",
				Width:   1,
				Style: uv.Style{
					Fg:        red,
					Bg:        blue,
					Underline: uv.UnderlineSingle,
					Attrs:     uv.AttrReverse,
				},
			},
			wantBG:   red,
			wantLine: blue,
			lineSample: func(ctx Context, area image.Rectangle) image.Point {
				return image.Pt(area.Min.X, area.Min.Y+ctx.Metrics().UnderlinePosition.Int())
			},
		},
		"faint": {
			cell: &uv.Cell{
				Content: " ",
				Width:   1,
				Style: uv.Style{
					Fg:        color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
					Bg:        color.NRGBA{A: 0xff},
					Underline: uv.UnderlineSingle,
					Attrs:     uv.AttrFaint,
				},
			},
			wantBG:   color.NRGBA{A: 0xff},
			wantLine: gray,
			lineSample: func(ctx Context, area image.Rectangle) image.Point {
				return image.Pt(area.Min.X, area.Min.Y+ctx.Metrics().UnderlinePosition.Int())
			},
		},
		"conceal": {
			cell: &uv.Cell{
				Content: " ",
				Width:   1,
				Style: uv.Style{
					Fg:        red,
					Bg:        blue,
					Underline: uv.UnderlineSingle,
					Attrs:     uv.AttrConceal,
				},
			},
			wantBG:   blue,
			wantLine: blue,
			lineSample: func(ctx Context, area image.Rectangle) image.Point {
				return image.Pt(area.Min.X, area.Min.Y+ctx.Metrics().UnderlinePosition.Int())
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scr := newTestScreen(1, 1)
			scr.SetCell(0, 0, tt.cell)
			d := MustNew()
			img := drawNRGBA(t, d, scr)
			ctx := d.contextLocked(image.Point{}, scr)
			area := ctx.CellBounds(0, 0)

			if got := sample(img, area.Inset(2)); got != tt.wantBG {
				t.Fatalf("background pixel = %#v, want %#v", got, tt.wantBG)
			}
			if got := img.NRGBAAt(tt.lineSample(ctx, area).X, tt.lineSample(ctx, area).Y); got != tt.wantLine {
				t.Fatalf("decoration pixel = %#v, want %#v", got, tt.wantLine)
			}
		})
	}
}

func TestDottedUnderlineGlobalSpacing(t *testing.T) {
	t.Parallel()

	const cols = 8
	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	style := uv.Style{Fg: fg, Bg: bg, Underline: uv.UnderlineDotted}

	scr := newTestScreen(cols, 1)
	for x := range cols {
		scr.SetCell(x, 0, &uv.Cell{Content: " ", Width: 1, Style: style})
	}

	d := MustNew(WithPalette(Palette{DefaultForeground: fg, DefaultBackground: bg}))
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)
	metrics := ctx.Metrics()
	y := ctx.CellBounds(0, 0).Min.Y + metrics.UnderlinePosition.Int()
	step := max(2, metrics.UnderlineThickness.Int()*2)

	var dots []int
	for x := ctx.GridBounds().Min.X; x < ctx.GridBounds().Max.X; x++ {
		if img.NRGBAAt(x, y) != fg {
			continue
		}
		if x > ctx.GridBounds().Min.X && img.NRGBAAt(x-1, y) == fg {
			continue
		}
		dots = append(dots, x)
	}
	if len(dots) < 2 {
		t.Fatalf("dot count = %d, want at least 2", len(dots))
	}
	for i := 1; i < len(dots); i++ {
		if got, want := dots[i]-dots[i-1], step; got != want {
			t.Fatalf("dot gap[%d] = %d, want uniform %d (dots=%v)", i, got, want, dots)
		}
	}
	thickness := metrics.UnderlineThickness.Int()
	for _, x := range dots {
		width := 0
		for px := x; px < ctx.GridBounds().Max.X && img.NRGBAAt(px, y) == fg; px++ {
			width++
		}
		if width != thickness {
			t.Fatalf("dot at x=%d width = %d, want %d", x, width, thickness)
		}
	}
}

func TestRendererPaletteIndexedColors(t *testing.T) {
	t.Parallel()

	mapped := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	scr := newTestScreen(1, 1)
	scr.SetCell(0, 0, &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: ansi.IndexedColor(42)}})
	d := MustNew(WithPalette(Palette{Indexed: map[int]color.Color{42: mapped}}))
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)

	if got := sample(img, ctx.CellBounds(0, 0)); got != mapped {
		t.Fatalf("indexed background = %#v, want palette color %#v", got, mapped)
	}
}

func TestRendererBackgroundOpacity(t *testing.T) {
	t.Parallel()

	red := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(1, 1)
	scr.SetCell(0, 0, &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: red}})

	opaqueCells := MustNew(WithBackgroundOpacity(0.5))
	img := drawNRGBA(t, opaqueCells, scr)
	ctx := opaqueCells.contextLocked(image.Point{}, scr)
	if got := sample(img, ctx.CellBounds(0, 0)); got != red {
		t.Fatalf("explicit cell background = %#v, want opaque %#v", got, red)
	}

	transparentCells := MustNew(WithBackgroundOpacity(0.5), WithBackgroundOpacityCells(true))
	img = drawNRGBA(t, transparentCells, scr)
	ctx = transparentCells.contextLocked(image.Point{}, scr)
	if got := sample(img, ctx.CellBounds(0, 0)); got != (color.NRGBA{R: 0xff, A: 0x80}) {
		t.Fatalf("transparent cell background = %#v, want alpha-applied red", got)
	}
}

func TestRendererBlinkUsesDeterministicClock(t *testing.T) {
	t.Parallel()

	t.Run("text decoration", func(t *testing.T) {
		t.Parallel()

		red := color.NRGBA{R: 0xff, A: 0xff}
		scr := newTestScreen(1, 1)
		scr.SetCell(0, 0, &uv.Cell{
			Content: " ",
			Width:   1,
			Style:   uv.Style{Fg: red, Underline: uv.UnderlineSingle, Attrs: uv.AttrBlink},
		})

		visible := MustNew(WithNow(func() time.Time { return time.Unix(0, 0) }))
		hidden := MustNew(WithNow(func() time.Time { return time.Unix(0, int64(500*time.Millisecond)) }))

		visibleImg := drawNRGBA(t, visible, scr)
		hiddenImg := drawNRGBA(t, hidden, scr)
		ctx := visible.contextLocked(image.Point{}, scr)
		p := image.Pt(ctx.CellBounds(0, 0).Min.X, ctx.CellBounds(0, 0).Min.Y+ctx.Metrics().UnderlinePosition.Int())

		if got := visibleImg.NRGBAAt(p.X, p.Y); got != red {
			t.Fatalf("visible blink pixel = %#v, want %#v", got, red)
		}
		if got := hiddenImg.NRGBAAt(p.X, p.Y); got != (color.NRGBA{A: 0xff}) {
			t.Fatalf("hidden blink pixel = %#v, want background", got)
		}
	})

	t.Run("cursor bar", func(t *testing.T) {
		t.Parallel()

		cursor := color.NRGBA{R: 0xff, A: 0xff}
		scr := newTestScreen(1, 1)
		visibleState := EmulatorState{
			Focused:       true,
			CursorVisible: true,
			CursorColor:   cursor,
			CursorStyle:   uv.CursorBar,
			CursorBlink:   true,
		}
		visible := MustNew(
			WithEmulatorState(visibleState),
			WithCursorBlinkSpeed(time.Second),
			WithNow(func() time.Time { return time.Unix(0, 0) }),
		)
		img := drawNRGBA(t, visible, scr)
		ctx := visible.contextLocked(image.Point{}, scr)
		area := ctx.CellBounds(0, 0)
		if got := img.NRGBAAt(area.Min.X, area.Min.Y); got != cursor {
			t.Fatalf("cursor bar left pixel = %#v, want %#v", got, cursor)
		}
		if got := img.NRGBAAt(area.Min.X+ctx.Metrics().CursorThickness.Int(), area.Min.Y); got == cursor {
			t.Fatalf("cursor bar extended past configured thickness")
		}

		hidden := MustNew(
			WithEmulatorState(visibleState),
			WithCursorBlinkSpeed(time.Second),
			WithNow(func() time.Time { return time.Unix(1, 0) }),
		)
		img = drawNRGBA(t, hidden, scr)
		if got := img.NRGBAAt(area.Min.X, area.Min.Y); got == cursor {
			t.Fatalf("blink-hidden cursor pixel = %#v, want non-cursor", got)
		}
	})
}

func TestRendererScrollbarThumbPinnedToBottom(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(1, 4)
	d := MustNew(
		WithScrollbar(true),
		WithEmulatorState(EmulatorState{Focused: true, ScrollbackCount: 30}),
	)
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)
	sb := ctx.ScrollbarBounds()
	top := img.NRGBAAt(sb.Min.X, sb.Min.Y)
	bottom := img.NRGBAAt(sb.Min.X, sb.Max.Y-1)

	if top == bottom {
		t.Fatalf("scrollbar track and thumb matched: %#v", top)
	}
	if bottom.R <= top.R {
		t.Fatalf("scrollbar bottom thumb = %#v, want brighter than track %#v", bottom, top)
	}
}

func TestRendererFocusDimmingExcludesMargin(t *testing.T) {
	t.Parallel()

	margin := color.NRGBA{R: 0xff, A: 0xff}
	bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	scr := newTestScreen(1, 1)
	d := MustNew(
		WithMargin(1, margin),
		WithFocusDimming(0.5),
		WithPalette(Palette{DefaultBackground: bg}),
		WithEmulatorState(EmulatorState{Focused: false}),
	)
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)

	if got := img.NRGBAAt(0, 0); got != margin {
		t.Fatalf("margin pixel = %#v, want undimmed %#v", got, margin)
	}
	if got := sample(img, ctx.WindowBounds()); got != (color.NRGBA{R: 0x7f, G: 0x7f, B: 0x7f, A: 0xff}) {
		t.Fatalf("dimmed window pixel = %#v, want dimmed white", got)
	}
}

func TestRendererRoundedMaskAffectsWindowCorners(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	scr := newTestScreen(1, 1)
	d := MustNew(
		WithMargin(1, color.NRGBA{}),
		WithBorderRadius(4),
		WithPalette(Palette{DefaultBackground: bg}),
	)
	img := drawNRGBA(t, d, scr)
	ctx := d.contextLocked(image.Point{}, scr)
	window := ctx.WindowBounds()

	if got := img.NRGBAAt(window.Min.X, window.Min.Y); got.A != 0 {
		t.Fatalf("rounded corner alpha = %d, want transparent", got.A)
	}
	if got := sample(img, window.Inset(4)); got != bg {
		t.Fatalf("rounded center pixel = %#v, want %#v", got, bg)
	}
}

func TestBoxThicknessOverrideThickensHorizontalLine(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	scr := newTestScreen(1, 1)
	scr.SetCell(0, 0, &uv.Cell{
		Content: "─",
		Width:   1,
		Style:   uv.Style{Fg: fg, Bg: bg},
	})

	base := MustNew(WithFontSizePt(14))
	thick := MustNew(WithFontSizePt(14), WithBoxThickness(4))

	baseCell := base.contextLocked(image.Point{}, scr).CellBounds(0, 0)
	thickCell := thick.contextLocked(image.Point{}, scr).CellBounds(0, 0)
	baseInk, ok := inkBounds(drawNRGBA(t, base, scr), baseCell, bg)
	if !ok {
		t.Fatal("default horizontal box line produced no ink")
	}
	thickInk, ok := inkBounds(drawNRGBA(t, thick, scr), thickCell, bg)
	if !ok {
		t.Fatal("thickened horizontal box line produced no ink")
	}

	if thickInk.Dy() <= baseInk.Dy() {
		t.Fatalf("thickened ink height = %d, want greater than default %d", thickInk.Dy(), baseInk.Dy())
	}
	if thickInk.Dy() < 4 {
		t.Fatalf("thickened ink height = %d, want at least 4px", thickInk.Dy())
	}
}
