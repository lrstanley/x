// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	icol "github.com/lrstanley/x/term/still/internal/color"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/internal/fontset"
	imetrics "github.com/lrstanley/x/term/still/internal/metrics"
	"github.com/lrstanley/x/term/still/internal/raster"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/vt/screen"
	xfont "golang.org/x/image/font"
)

// How far the title bar, its bottom edge, and the title text blend from the
// window background toward the foreground.
const (
	chromeBarTint      = 0.08
	chromeHairlineTint = 0.16
	chromeTitleTint    = 0.7
)

// chromeTextScale is the title bar text size relative to the grid font, so
// the bar reads as a frame around the content rather than another line of it.
const chromeTextScale = 0.88

// chromeButtons are the window button colors, left to right.
var chromeButtons = [...]*image.Uniform{
	image.NewUniform(color.NRGBA{R: 0xff, G: 0x5f, B: 0x57, A: 0xff}),
	image.NewUniform(color.NRGBA{R: 0xfe, G: 0xbc, B: 0x2e, A: 0xff}),
	image.NewUniform(color.NRGBA{R: 0x28, G: 0xc8, B: 0x40, A: 0xff}),
}

// chromeTextMax is the most runes of title bar text that are drawn: more than
// any title bar has room for, so huge titles cost no more than long ones.
const chromeTextMax = 512

// buildChromeFonts loads the faces title bar text is drawn with, when there
// is window chrome. They are a second [fontset.Set] at chromeTextScale of the
// grid size, so the title keeps the grid's family, codepoint map, and
// fallbacks.
func (d *Renderer) buildChromeFonts() error {
	old := d.chromeCtx.fonts
	d.chromeCtx = chromeText{}
	if err := old.Close(); err != nil {
		return err
	}
	if !d.opts.WindowChrome {
		return nil
	}
	opts := d.opts
	opts.Scale *= chromeTextScale
	fonts, err := fontset.Build(opts)
	if err != nil {
		return err
	}
	d.chromeCtx = chromeText{
		fonts:   fonts,
		metrics: imetrics.Derive(opts, fonts.GridMetrics()),
		capH:    fonts.GridMetrics().CapHeight.Float64(),
	}
	return nil
}

// chromeText is the glyph context for title bar text: a frame, with the
// chrome faces and their metrics in place of the grid's. Its fonts are nil
// without window chrome.
type chromeText struct {
	*renderFrame
	fonts   *fontset.Set
	metrics types.Metrics
	capH    float64 // cap height of the chrome faces, in pixels
}

func (c *chromeText) Metrics() types.Metrics { return c.metrics }

func (c *chromeText) FontFace(cell *screen.Cell) xfont.Face { return c.fonts.FaceForCell(cell) }

func (c *chromeText) UsesGridLayout(face xfont.Face) bool { return c.fonts.UsesGridLayout(face) }

// cell is the size of one title bar text cell.
func (c *chromeText) cell() (w, h int) {
	return max(c.metrics.CellWidth.Int(), 1), c.metrics.CellHeight.Int()
}

// plainText makes untrusted text safe to draw as one plain line in the title
// bar. Controls (C0 and C1, which covers ESC, so no escape sequence survives)
// and white space, which includes line and paragraph separators, become
// spaces, and runs of them collapse, with none at either end. Format
// characters (Unicode Cf: every bidi embedding, override, and isolate, and
// zero-width characters, the soft hyphen, and the byte order mark), other
// default ignorable code points, noncharacters, and invalid UTF-8 are dropped.
// Nothing is read as markup, and cells are laid out left to right in the
// order received, so "\u202egnp.exe" reads "gnp.exe", not "exe.png". At most
// chromeTextMax runes are read. Text that needs no change is returned as is.
func plainText(s string) string {
	var (
		b     strings.Builder
		built bool // b holds the text so far; until then it is s[:i]
		space bool // a space is owed before the next rune kept
		runes int
	)
	for i, r := range s {
		runes++
		if runes > chromeTextMax {
			if !built {
				return strings.TrimSuffix(s[:i], " ")
			}
			break
		}
		blank, drop := r == ' ', false
		if r < ' ' || r > '~' { // printable ASCII is kept as is
			blank = unicode.IsControl(r) || unicode.IsSpace(r)
			drop = !blank && (r == utf8.RuneError ||
				unicode.In(r, unicode.Cf, unicode.Other_Default_Ignorable_Code_Point, unicode.Noncharacter_Code_Point))
		}
		if !built {
			switch {
			case !blank && !drop:
				space = false
				continue
			case r == ' ' && i > 0 && !space:
				space = true // one space between words is kept as is
				continue
			}
			// The first rune that changes: copy the text before it, less
			// the space it owes.
			built = true
			b.Grow(len(s))
			b.WriteString(strings.TrimSuffix(s[:i], " "))
		}
		switch {
		case blank:
			space = b.Len() > 0
		case drop:
		default:
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
		}
	}
	if !built {
		return strings.TrimSuffix(s, " ")
	}
	return b.String()
}

// fitText returns s truncated with an ellipsis to cells, and its width.
func fitText(s string, cells int) (fit string, width int) {
	if width = ansi.StringWidth(s); width <= cells {
		return s, width
	}
	fit = ansi.Truncate(s, cells, "…")
	return fit, ansi.StringWidth(fit)
}

// chromeLine is a line of title bar text: where it is drawn, centered on mid,
// and how.
type chromeLine struct {
	dst draw.Image
	f   *renderFrame
	mid float64
	// box, when set, fits icons (Nerd Font and other fitted glyphs) into a
	// box-pixel square centered on mid instead of a cell, so status icons
	// line up with the window buttons. Sprites such as braille keep a full
	// cell.
	box int
	// usePass adds glyphs to the foreground pass, so glyph masks are cached.
	usePass bool
}

// text draws s as a row of chrome cells from x, with its cap height centered
// on the line, in fg, and returns the x after it. Cells go through the same
// layout as the grid, so fallbacks, Nerd Font fitting, and wide cells behave
// the same, just smaller.
func (l *chromeLine) text(x int, s string, fg color.NRGBA, bold bool) int {
	d := l.f.r
	// The context is the renderer's, lent the frame for this call: one built
	// here would escape through the raster.GlyphContext interface and
	// allocate on every call.
	ctx := &d.chromeCtx
	ctx.renderFrame = l.f
	cw, ch := ctx.cell()
	// Optical centering: the cap height, not the cell box, sits on mid.
	baseline := int(math.Round(l.mid + ctx.capH/2))
	bottom := baseline + ctx.metrics.FontBaseline.Int()
	// The color goes to the layout directly: setting Style.Fg would box it.
	var cell screen.Cell
	if bold {
		cell.Style.Attrs |= screen.AttrBold
	}
	var (
		state  byte
		layout raster.Layout
		prev   string // content of the previous cell
	)
	for s != "" {
		var n int
		cell.Content, cell.Width, n, state = ansi.DecodeSequence(s, state, nil)
		s = s[n:]
		if cell.Width == 0 {
			continue
		}
		area := image.Rect(x, bottom-ch, x+cell.Width*cw, bottom)
		x = area.Max.X
		// Icons grow into a space after them, the same as in the grid. The
		// end of the text counts as an edge, like the end of a row.
		before := prev
		prev = cell.Content
		spansNext := false
		if cell.Width == 1 && s != "" && raster.IsSpanIcon(cell.Content) {
			next, _, _, _ := ansi.DecodeSequence(s, state, nil)
			spansNext = raster.IconRoom(before, next)
		}
		if !cellHasForegroundGlyph(&cell) || !raster.LayoutGlyph(ctx, area, &cell, spansNext, fg, &layout) {
			continue
		}
		if layout.Kind != raster.KindGrid {
			// Icons and sprites are fitted to their area rather than set on
			// the baseline, so center the area itself on the line.
			h := ch
			if l.box > 0 && layout.Kind != raster.KindSprite {
				h = l.box
				area.Max.X = area.Min.X + max(l.box, area.Dx())
				x = area.Max.X
				if area.Dx() > l.box {
					area.Min.X += (area.Dx() - l.box) / 2
					area.Max.X = area.Min.X + l.box
				}
			}
			area.Min.Y = int(math.Round(l.mid - float64(h)/2))
			area.Max.Y = area.Min.Y + h
			layout.SetArea(ctx, area, &cell)
		}
		if l.usePass {
			d.fgPass.AddGlyph(ctx, &layout)
		} else {
			raster.DrawGlyphLayout(ctx, l.dst, &layout)
		}
	}
	ctx.renderFrame = nil
	return x
}

// drawChrome paints the title bar. The title, or the OSC 7501 program status
// in its place, is drawn with the chrome faces, optically centered on the
// window buttons.
func (d *Renderer) drawChrome(dst draw.Image, f *renderFrame, usePass bool) {
	bar := f.chromeBounds
	if !bar.Overlaps(dst.Bounds()) {
		return
	}
	fg, bg := f.foregroundNRGBA(nil), f.backgroundNRGBA(nil)
	tint := func(k float64) color.NRGBA {
		return icol.ApplyAlphaNRGBA(icol.BlendNRGBA(bg, fg, k), f.opts.BackgroundOpacity)
	}

	body := bar
	body.Max.Y-- // the hairline
	idraw.Fill(dst, bar, tint(chromeBarTint))
	idraw.Fill(dst, image.Rectangle{Min: image.Pt(bar.Min.X, body.Max.Y), Max: bar.Max}, tint(chromeHairlineTint))

	diameter := body.Dy() / 3
	if d.chromeMask == nil || d.chromeMask.Rect.Dx() != diameter {
		d.chromeMask = idraw.CircleMask(diameter)
	}
	top := body.Min.Y + (body.Dy()-diameter)/2
	x := body.Min.X + body.Dy()/2
	for _, src := range chromeButtons {
		dr := image.Rect(x, top, x+diameter, top+diameter)
		draw.DrawMask(dst, dr, src, image.Point{}, d.chromeMask, image.Point{}, draw.Over)
		x += 2 * diameter
	}

	// The title is centered in the room the buttons leave on both sides, on
	// the buttons' centerline. A program status replaces it while there is
	// one to show.
	if !f.hasEmu {
		return
	}
	room := body.Dx() - 2*(x-body.Min.X)
	line := chromeLine{dst: dst, f: f, mid: float64(top) + float64(diameter)/2, usePass: usePass}
	if f.status.view.shown {
		d.drawStatusTitle(&line, body, room, diameter)
		return
	}
	cw, _ := d.chromeCtx.cell()
	title, w := fitText(plainText(f.emu.Title), room/cw)
	line.text(body.Min.X+(body.Dx()-w*cw)/2, title, icol.BlendNRGBA(bg, fg, chromeTitleTint), false)
}
