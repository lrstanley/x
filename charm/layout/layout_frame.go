// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import "charm.land/lipgloss/v2"

var _ Layout = (*frameLayout)(nil)

type frameLayout struct {
	style lipgloss.Style
	child any
}

// Frame creates a new layout which contains the provided child within it,
// with the free space left over after the style has been applied. Use this for
// wrapping children in borders, padding, etc.
func Frame(style lipgloss.Style, child any) Layout {
	if child == nil {
		return nil
	}
	return &frameLayout{style: style, child: child}
}

func (r *frameLayout) Render(availableWidth, availableHeight int) Layer {
	if r.child == nil {
		return nil
	}

	// Get frame sizes
	hFrame := r.style.GetHorizontalFrameSize()
	vFrame := r.style.GetVerticalFrameSize()

	// Render the child
	layer := resolveLayer(
		r.child,
		max(0, availableWidth-hFrame),
		max(0, availableHeight-vFrame),
	)
	if layer == nil {
		return nil
	}
	bounds := layer.Bounds()
	return NewLayer(
		"",
		r.style.
			Width(bounds.Dx()+hFrame).
			Height(bounds.Dy()+vFrame).
			Render(),
	).Z(1).AddChild(layer.X(hFrame / 2).Y(vFrame / 2).Z(2))
}
