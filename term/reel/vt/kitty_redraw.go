// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"compress/zlib"
	"fmt"

	"github.com/charmbracelet/x/ansi"

	"github.com/lrstanley/x/term/reel/internal/kittycmd"
)

// redrawImagesLocked writes Kitty graphics commands that restore the active
// screen's placements onto a terminal that shows the same text: each placed
// image once, then its placements, with the cursor saved around them.
// Placements partly above the screen are cropped to what shows; those
// entirely in the scrollback, and images with no placement, are left out.
func (t *Terminal) redrawImagesLocked(b *bytes.Buffer) {
	g, gen, ok := t.kittyGraphicsLocked()
	if !ok || gen == 0 {
		return
	}
	offset := t.scrollbackOffsetLocked()
	sent := map[uint32]bool{}
	var places bytes.Buffer
	for _, e := range t.kittyPlacementsLocked(g) {
		cmd, onScreen := t.placementCommand(e, offset)
		if !onScreen {
			continue
		}
		if img := e.image; !sent[img.ID] {
			sent[img.ID] = true
			var z bytes.Buffer
			zw := zlib.NewWriter(&z)
			_, _ = zw.Write(img.Pixels.Pix)
			_ = zw.Close()
			r := img.Pixels.Rect
			kittycmd.AppendTransmit(b, []string{
				"a=t", "f=32", "o=z", "q=2",
				fmt.Sprintf("i=%d", img.ID), fmt.Sprintf("s=%d", r.Dx()), fmt.Sprintf("v=%d", r.Dy()),
			}, z.Bytes())
		}
		places.WriteString(cmd)
	}
	if places.Len() > 0 {
		b.WriteString(ansi.SaveCursor)
		b.Write(places.Bytes())
		b.WriteString(ansi.RestoreCursor)
	}
}

// placementCommand returns the commands that put e back: a cursor move and a
// put that leaves the cursor in place. offset is how far the viewport is
// scrolled up. ok is false when e is not on the active screen.
func (t *Terminal) placementCommand(e kittyEntry, offset int) (string, bool) {
	keys := []string{"a=p", fmt.Sprintf("i=%d", e.image.ID), "q=2"}
	add := func(key string, v int) {
		if v != 0 {
			keys = append(keys, fmt.Sprintf("%s=%d", key, v))
		}
	}
	add("p", int(e.placement))
	add("z", int(e.z))
	if e.virtual {
		add("c", e.cols)
		add("r", e.rows)
		return ansi.KittyGraphics(nil, append(keys, "U=1")...), true
	}
	row, rows, yoff, src := e.row-offset, e.rows, e.yoff, e.source
	if row+e.gridRows <= 0 || row >= t.rows || e.height <= 0 {
		return "", false
	}
	if row < 0 {
		// Drop the rows above the screen, and the source they showed.
		cut := (-row*int(t.cellH) - yoff) * src.Dy() / e.height
		src.Min.Y += cut
		if rows > 0 {
			rows += row
		}
		row, yoff = 0, 0
		if src.Empty() {
			return "", false
		}
	}
	add("x", src.Min.X)
	add("y", src.Min.Y)
	add("w", src.Dx())
	add("h", src.Dy())
	add("X", e.xoff)
	add("Y", yoff)
	add("c", e.cols)
	add("r", rows)
	return ansi.CursorPosition(e.col+1, row+1) + ansi.KittyGraphics(nil, append(keys, "C=1")...), true
}
