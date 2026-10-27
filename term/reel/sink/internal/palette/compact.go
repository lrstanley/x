// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"image"
	"image/color"
	"image/gif"
)

// Same reports whether a and b are the same palette slice. Equal
// indices under two palettes are different pictures, so dedup and disposal
// treat a different slice as a different palette even when the colors match.
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

// Groups splits frames by palette identity. Frames that share a slice
// stay in one group even when another palette sits between them.
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

// Compact compacts the palette frames share to only the entries they use,
// keeping their order, and moves the transparent slot to the end when a frame
// uses it, so a palette with few colors gets a smaller table. GIF's LZW output
// does not depend on which index a color has, only on how many bits the table
// needs, so reordering by frequency would gain nothing. Frames are updated in
// place with remapped indices and the new palette.
func Compact(frames []*image.Paletted) color.Palette {
	oldPal := frames[0].Palette
	var used [256]bool
	// top is the highest index each frame uses. Indices below the first one
	// compaction moves keep their place, so a frame entirely below it (often
	// a keyframe, which an adaptive palette fills from 0 up) is not rewritten.
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

// CompactGIF compacts each palette to the colors its frames use. The shared
// palette is compacted over the frames that use it and stays the global
// color table.
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
// package builds. GIF writers give a frame's transparent index to its
// palette's zero-alpha color.
func transparentMarkerColor() color.Color {
	return color.RGBA{A: 0}
}
