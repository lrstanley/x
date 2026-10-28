// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"image"
	"image/color"
	"image/gif"
)

// Same reports whether a and b are the same palette slice, which dedup and
// disposal require even when the colors of two slices match.
func Same(a, b color.Palette) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	return len(a) == len(b) && &a[0] == &b[0]
}

// Group is the frames that share a palette slice.
type Group struct {
	Palette color.Palette
	Frames  []*image.Paletted
}

// Groups splits frames by palette slice ([Same]), in order of first use.
func Groups(frames []*image.Paletted) []Group {
	var groups []Group
	for _, frame := range frames {
		if frame == nil {
			continue
		}
		found := false
		for i := range groups {
			if Same(groups[i].Palette, frame.Palette) {
				groups[i].Frames = append(groups[i].Frames, frame)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, Group{Palette: frame.Palette, Frames: []*image.Paletted{frame}})
		}
	}
	return groups
}

// Compact rewrites frames, which share a palette, in place onto a new palette
// of only the entries they use, in order, with the transparent slot moved to
// the end if used. It returns the new palette. Reordering by frequency would
// gain nothing: GIF's LZW output depends only on the table's bit width.
func Compact(frames []*image.Paletted) color.Palette {
	oldPal := frames[0].Palette
	var used [256]bool
	// top is the highest index each frame uses. A frame entirely below the
	// first index that moves is not rewritten.
	top := make([]int, len(frames))
	for i, frame := range frames {
		var seen [256]bool
		for _, idx := range frame.Pix {
			seen[idx] = true
		}
		for idx, ok := range seen {
			if ok {
				used[idx], top[i] = true, idx
			}
		}
	}

	var remap [256]uint8
	newPal := make(color.Palette, 0, len(oldPal))
	for idx := range TransparentIndex {
		if used[idx] {
			remap[idx] = uint8(len(newPal)) //nolint:gosec // bounded by 255 colors.
			newPal = append(newPal, oldPal[idx])
		}
	}
	if used[TransparentIndex] {
		remap[TransparentIndex] = uint8(len(newPal)) //nolint:gosec // bounded by 256 colors.
		newPal = append(newPal, transparentMarkerColor())
	}
	moved := 0
	for moved < len(remap) && (!used[moved] || int(remap[moved]) == moved) {
		moved++
	}

	for i, frame := range frames {
		frame.Palette = newPal
		if top[i] < moved {
			continue
		}
		for j, idx := range frame.Pix {
			frame.Pix[j] = remap[idx]
		}
	}
	return newPal
}

// CompactGIF compacts each palette of out with [Compact], keeping the global
// color table pointing at the compacted global palette.
func CompactGIF(out *gif.GIF) {
	global, _ := out.Config.ColorModel.(color.Palette)
	for _, g := range Groups(out.Image) {
		compacted := Compact(g.Frames)
		if Same(g.Palette, global) {
			out.Config.ColorModel = compacted
		}
	}
}

// transparentMarkerColor fills the transparent slot of every palette this
// package builds. image/gif makes a palette's zero-alpha color the frame's
// transparent index.
func transparentMarkerColor() color.Color {
	return color.RGBA{A: 0}
}
