// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"cmp"
	"image"
	"image/color"
	"image/gif"
	"slices"
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

// From returns a GIF's global palette: colorModel when it is a palette,
// otherwise the first frame's, or [Default] when there is none.
func From(colorModel color.Model, frames []*image.Paletted) color.Palette {
	if p, ok := colorModel.(color.Palette); ok && p != nil {
		return p
	}
	if len(frames) > 0 && frames[0].Palette != nil {
		return frames[0].Palette
	}
	return Default()
}

// Compact compacts oldPal to only entries referenced across frames,
// reordering by descending frequency so low indices dominate (gifsicle-style
// colormap canonicalization). Transparent pixels map to a dedicated slot
// marked with zero alpha in the palette for replay detection. Frames are
// updated in place with remapped indices and the new palette.
func Compact(frames []*image.Paletted, oldPal color.Palette) color.Palette {
	var freq [256]int
	for _, frame := range frames {
		for _, idx := range frame.Pix {
			freq[idx]++
		}
	}

	usesTransparent := freq[TransparentIndex] > 0
	freq[TransparentIndex] = 0

	var used []uint8
	for idx, count := range &freq {
		if count > 0 {
			used = append(used, uint8(idx)) //nolint:gosec // idx is 0..255.
		}
	}
	slices.SortFunc(used, func(a, b uint8) int {
		return cmp.Or(cmp.Compare(freq[b], freq[a]), cmp.Compare(a, b))
	})

	var remap [256]uint8
	newPal := make(color.Palette, 0, len(used)+1)
	for i, idx := range used {
		newPal = append(newPal, oldPal[idx])
		remap[idx] = uint8(i) //nolint:gosec // i is bounded by palette size.
	}
	if usesTransparent {
		remap[TransparentIndex] = uint8(len(newPal)) //nolint:gosec // bounded by 256 colors.
		newPal = append(newPal, transparentMarkerColor())
	}

	for _, frame := range frames {
		frame.Palette = newPal
		for i, idx := range frame.Pix {
			frame.Pix[i] = remap[idx]
		}
	}
	return newPal
}

// CompactGIF compacts each palette to the colors its frames use.
// keepGlobal leaves a caller-supplied shared palette as given; isolated
// palettes are still compacted. Otherwise the shared palette is compacted
// over the frames that use it and becomes the global color table.
func CompactGIF(out *gif.GIF, keepGlobal bool) {
	global := From(out.Config.ColorModel, out.Image)
	var shared color.Palette
	for _, g := range Groups(out.Image) {
		if keepGlobal && Same(g.Palette, global) {
			continue
		}
		compacted := Compact(g.Frames, g.Palette)
		if Same(g.Palette, global) {
			shared = compacted
		}
	}
	if shared != nil {
		out.Config.ColorModel = shared
	}
}

// MarkTransparent marks TransparentIndex as a zero-alpha
// palette color so GIF encoders emit a transparent color index for sub-rect
// frames. Each palette is patched on its own, so an isolated frame keeps its
// local table. Custom palettes may still use an opaque xterm slot 255. A
// palette only needs the slot when a frame on it uses transparency; shorter
// palettes have already dropped an unused slot during compaction.
func MarkTransparent(out *gif.GIF) {
	if len(out.Image) == 0 {
		return
	}
	global, _ := out.Config.ColorModel.(color.Palette)
	for _, g := range Groups(out.Image) {
		if len(g.Palette) <= int(TransparentIndex) {
			continue
		}
		if _, _, _, a := g.Palette[TransparentIndex].RGBA(); a == 0 {
			continue
		}
		newPal := make(color.Palette, len(g.Palette))
		copy(newPal, g.Palette)
		newPal[TransparentIndex] = transparentMarkerColor()
		if Same(g.Palette, global) {
			out.Config.ColorModel = newPal
		}
		for _, frame := range g.Frames {
			frame.Palette = newPal
		}
	}
}

// transparentMarkerColor is stored in compact palettes to mark the transparent
// slot, which a replay finds by its zero alpha.
func transparentMarkerColor() color.Color {
	return color.RGBA{A: 0}
}
