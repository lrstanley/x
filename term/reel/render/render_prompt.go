// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/draw"
	"strconv"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	// promptTickTint is how far scrollbar ticks blend from the window
	// background toward the foreground, brighter than the thumb so they show
	// over it.
	promptTickTint = 0.85
	// promptBadgeTint is how far a badge's fill blends from the window
	// background toward the failure color.
	promptBadgeTint = 0.18
)

// drawPromptMarks draws, over the right end of each prompt row on the screen
// whose command failed, a badge with the exit status. It runs after the
// grid's text, which the badge covers, so the badge text gets a foreground
// pass of its own.
func (d *Renderer) drawPromptMarks(dst *image.NRGBA, f *renderFrame) {
	if !d.opts.PromptMarks || len(f.st.PromptMarks) == 0 || !f.gridBounds.Overlaps(dst.Bounds()) {
		return
	}
	top := f.st.ScrollbackLen - f.st.ScrollbackOffset
	badged := false
	for _, m := range f.st.PromptMarks {
		if !m.Failed() {
			continue
		}
		row := f.cellBounds(f.screenBounds.Min.X, f.screenBounds.Min.Y+m.Row-top)
		if row.Empty() {
			continue
		}
		if row.Max.X = f.gridBounds.Max.X; !row.Overlaps(dst.Bounds()) {
			continue
		}
		if !badged {
			// The grid's pass bounds, so the coverage buffer is reused rather
			// than reallocated.
			d.fgPass.Reset(f.gridBounds.Union(f.chromeBounds))
			badged = true
		}
		d.drawPromptBadge(dst, f, row, m.ExitCode)
	}
	if badged {
		d.fgPass.Composite(dst)
		d.fgPass.DrawPending(f, dst)
	}
}

// drawPromptBadge fills a badge at the right end of row and adds the exit
// status code to the foreground pass, with half a grid cell around it.
func (d *Renderer) drawPromptBadge(dst draw.Image, f *renderFrame, row image.Rectangle, code int) {
	cw, _ := d.chromeCtx.cell()
	// The icon fits a box as tall as the code's digits, which can be wider
	// than its cell.
	box := int(d.chromeCtx.capH)
	iconW := max(box-cw, 0)
	text, w := fitText(statusIconError+" "+strconv.Itoa(code), (row.Dx()-f.cellW-iconW)/cw)
	badge := row
	badge.Min.X = row.Max.X - w*cw - iconW - f.cellW
	red := f.statusColor(vt.ProgramError)
	fill := icol.ApplyAlphaNRGBA(icol.BlendNRGBA(f.backgroundNRGBA(nil), red, promptBadgeTint), f.opts.BackgroundOpacity)
	idraw.Fill(dst, badge, fill)
	line := chromeLine{dst: dst, f: f, mid: float64(row.Min.Y) + float64(row.Dy())/2, box: box}
	line.text(badge.Min.X+f.cellW/2, text, red, true)
}
