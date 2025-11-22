// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import "charm.land/lipgloss/v2"

var _ Layout = (*horizontalLayout)(nil)

type horizontalLayout struct {
	children []any
}

// Horizontal creates a new horizontal layout with the provided children.
func Horizontal(children ...any) Layout {
	children = filterNil(children)
	if len(children) == 0 {
		return nil
	}
	return &horizontalLayout{children: children}
}

func (r *horizontalLayout) Render(availableWidth, availableHeight int) *lipgloss.Layer {
	if len(r.children) == 0 {
		return nil
	}

	var spaces int
	var totalFixedWidth int

	layers := layerPool.Get()
	defer func() {
		layers = layers[:0]
		layerPool.Put(layers)
	}()

	for _, child := range r.children {
		if _, isSpace := child.(*spacer); isSpace {
			layers = append(layers, nil)
			spaces++
			continue
		}

		layer := resolveLayer(child, availableWidth-totalFixedWidth, availableHeight)
		if layer == nil {
			continue
		}
		totalFixedWidth += layer.Bounds().Dx()
		layers = append(layers, layer)
	}

	switch len(layers) {
	case 0:
		return nil
	case 1:
		return layers[0].Z(1)
	}

	xOffset := 0
	spaceIndex := 0
	spaceDistrib := calculateSpaceDistribution(spaces, max(0, availableWidth-totalFixedWidth))
	for _, layer := range layers {
		if layer == nil { // Is space.
			xOffset += spaceDistrib[spaceIndex]
			spaceIndex++
			continue
		}
		xOffset += layer.GetX()
		layer.X(xOffset).Z(1)
		xOffset += layer.Bounds().Dx()
	}

	return lipgloss.NewLayer("layout_horizontal", "").
		Z(1).
		AddLayers(filterNilLayers(layers)...)
}
