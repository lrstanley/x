// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/internal/plaintext"
	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/fontset"
	imetrics "github.com/lrstanley/x/term/reel/render/internal/metrics"
	"github.com/lrstanley/x/term/reel/render/internal/raster"
	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/vt"
	xfont "golang.org/x/image/font"
)

// How far the title bar, its bottom edge, and the title text blend from the
// window background toward the foreground.
const (
	chromeBarTint      = 0.08
	chromeHairlineTint = 0.16
	chromeTitleTint    = 0.7
	// A subtitle, and the dash before it, are dimmer than the title.
	chromeSubtitleTint = 0.45
	chromeDashTint     = 0.3
)

// chromeSubtitleMin is the fewest cells left for a subtitle before it is
// dropped instead of truncated.
const chromeSubtitleMin = 12

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

// buildChromeFonts loads the faces title bar text and prompt mark badges are
// drawn with, when there is window chrome or there are prompt marks. They are
// a second [fontset.Set] at chromeTextScale of the grid size, so the text
// keeps the grid's family, codepoint map, and fallbacks.
func (d *Renderer) buildChromeFonts() error {
	old := d.chromeCtx.fonts
	d.chromeCtx = chromeText{}
	if err := old.Close(); err != nil {
		return err
	}
	if !d.opts.WindowChrome && !d.opts.PromptMarks {
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
// without window chrome or prompt marks.
type chromeText struct {
	*renderFrame
	fonts   *fontset.Set
	metrics types.Metrics
	capH    float64 // cap height of the chrome faces, in pixels
}

func (c *chromeText) Metrics() types.Metrics { return c.metrics }

func (c *chromeText) FontFace(cell *vt.Cell, cluster idraw.Cluster) xfont.Face {
	return c.fonts.FaceForCell(cell, cluster)
}

func (c *chromeText) UsesGridLayout(face xfont.Face) bool { return c.fonts.UsesGridLayout(face) }

// RegisteredGlyph is nil: the program's glyphs are for its cells, not the
// window's title.
func (c *chromeText) RegisteredGlyph(idraw.Cluster) *vt.Glyph { return nil }

// cell is the size of one title bar text cell.
func (c *chromeText) cell() (w, h int) {
	return max(c.metrics.CellWidth.Int(), 1), c.metrics.CellHeight.Int()
}

// plainText is title bar text made safe to draw (see [plaintext.Line]).
func plainText(s string) string { return plaintext.Line(s, chromeTextMax) }

// fitText returns s truncated with an ellipsis to cells, and its width.
func fitText(s string, cells int) (fit string, width int) {
	if width = ansi.StringWidth(s); width <= cells {
		return s, width
	}
	fit = ansi.Truncate(s, cells, "…")
	return fit, ansi.StringWidth(fit)
}

// fitTextLeft is [fitText] for text whose end matters more than its start,
// such as a path: it keeps the end and puts the ellipsis first.
func fitTextLeft(s string, cells int) (fit string, width int) {
	if width = ansi.StringWidth(s); width <= cells {
		return s, width
	}
	if cells < 1 {
		return "", 0
	}
	// TruncateLeft keeps a wide grapheme the cut lands in, so cut one more
	// cell when it does.
	for n := width - cells + 1; ; n++ {
		if fit = ansi.TruncateLeft(s, n, "…"); ansi.StringWidth(fit) <= cells {
			return fit, ansi.StringWidth(fit)
		}
	}
}

// chromeDirectory is the path of an OSC 7 working directory report: the
// path of a file URL, decoded, or of a kitty-shell-cwd URL, which is sent
// as is. The host is dropped. A bare absolute path is kept, and anything
// else has no path to show. Only the last chromeTextMax runes are kept, so
// [plainText] cuts nothing and the directory's own name survives.
func chromeDirectory(report string) string {
	var path string
	if scheme, rest, ok := strings.Cut(report, "://"); !ok {
		if strings.HasPrefix(report, "/") {
			path = report
		}
	} else if i := strings.IndexByte(rest, '/'); i >= 0 {
		switch scheme {
		case "file":
			path = rest[i:] // shown undecoded when it does not parse
			if u, err := url.Parse(report); err == nil {
				path = u.Path
			}
		case "kitty-shell-cwd":
			path = rest[i:]
		}
	}
	i := len(path)
	for n := 0; i > 0 && n < chromeTextMax; n++ {
		_, size := utf8.DecodeLastRuneInString(path[:i])
		i -= size
	}
	return path[i:]
}

// chromeSpan is a run of title bar text in one color.
type chromeSpan struct {
	text string
	col  color.NRGBA
	bold bool
}

// appendSubtitle appends sub to spans after a dash, dimmer than the title,
// fitted with fit into the room cells left after the cells spans already
// take, and returns spans with their new width. It is dropped when it is
// empty or fewer than chromeSubtitleMin cells are left, so it goes before
// the title when room is short.
func (f *renderFrame) appendSubtitle(spans []chromeSpan, cells, room int, sub string, fit func(string, int) (string, int)) (out []chromeSpan, width int) {
	const dash = " — "
	if sub == "" || room < chromeSubtitleMin {
		return spans, cells
	}
	fg, bg := f.foregroundNRGBA(nil), f.backgroundNRGBA(nil)
	t, w := fit(sub, room-ansi.StringWidth(dash))
	spans = append(spans,
		chromeSpan{text: dash, col: icol.BlendNRGBA(bg, fg, chromeDashTint)},
		chromeSpan{text: t, col: icol.BlendNRGBA(bg, fg, chromeSubtitleTint)},
	)
	return spans, cells + ansi.StringWidth(dash) + w
}

// titleLabel appends the title bar text for the window title to spans, in
// at most maxCells cells, and returns them with their width in cells. The
// OSC 7 working directory is the title's subtitle, or stands in for a title
// the program did not set. It is trimmed from the left, so the directory's
// own name stays. The bell, while it rings, goes before the title.
func (f *renderFrame) titleLabel(spans []chromeSpan, maxCells int) (out []chromeSpan, cells int) {
	col := icol.BlendNRGBA(f.backgroundNRGBA(nil), f.foregroundNRGBA(nil), chromeTitleTint)
	dir := plainText(chromeDirectory(f.st.WorkingDirectory))
	title := f.bell.titleText(plainText(f.st.Title))
	if title == "" {
		t, w := fitTextLeft(dir, maxCells)
		return append(spans, chromeSpan{text: t, col: col}), w
	}
	t, w := fitText(title, maxCells)
	spans = append(spans, chromeSpan{text: t, col: col})
	return f.appendSubtitle(spans, w, maxCells-w, dir, fitTextLeft)
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
}

// text adds s to the foreground pass as a row of chrome cells from x, with
// its cap height centered on the line, in fg, and returns the x after it.
// Cells go through the same layout as the grid, so fallbacks, Nerd Font
// fitting, and wide cells behave the same, just smaller.
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
	var cell vt.Cell
	if bold {
		cell.Style.Attrs |= vt.AttrBold
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
		d.fgPass.AddGlyph(ctx, &layout)
	}
	ctx.renderFrame = nil
	return x
}

// spans draws each span with [chromeLine.text] from x, one after the other,
// and returns the x after the last.
func (l *chromeLine) spans(x int, runs []chromeSpan) int {
	for _, s := range runs {
		x = l.text(x, s.text, s.col, s.bold)
	}
	return x
}

// drawChrome paints the title bar. The title, with the bell before it while
// it rings, and working directory, or the OSC 7501 program status in their
// place, are drawn with the chrome faces, optically centered on the window
// buttons.
func (d *Renderer) drawChrome(dst draw.Image, f *renderFrame) {
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
	room := body.Dx() - 2*(x-body.Min.X)
	line := chromeLine{dst: dst, f: f, mid: float64(top) + float64(diameter)/2}
	if f.status.view.shown {
		d.drawStatusTitle(&line, body, room, diameter)
		return
	}
	cw, _ := d.chromeCtx.cell()
	var buf [3]chromeSpan
	spans, cells := f.titleLabel(buf[:0], room/cw)
	line.spans(body.Min.X+(body.Dx()-cells*cw)/2, spans)
}
