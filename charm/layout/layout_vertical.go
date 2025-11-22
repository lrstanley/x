// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import "charm.land/lipgloss/v2"

var _ Layout = (*verticalLayout)(nil)

type verticalLayout struct {
	children []any
}

// Vertical creates a new vertical layout with the provided children.
func Vertical(children ...any) Layout {
	children = filterNil(children)
	if len(children) == 0 {
		return nil
	}
	return &verticalLayout{children: children}
}

func (r *verticalLayout) Render(availableWidth, availableHeight int) *lipgloss.Layer {
	if len(r.children) == 0 {
		return nil
	}

	var spaces int
	var totalFixedHeight int

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

		layer := resolveLayer(child, availableWidth, availableHeight-totalFixedHeight)
		if layer == nil {
			continue
		}
		totalFixedHeight += layer.Bounds().Dy()
		layers = append(layers, layer)
	}

	switch len(layers) {
	case 0:
		return nil
	case 1:
		return layers[0].Z(1)
	}

	yOffset := 0
	spaceIndex := 0
	spaceDistrib := calculateSpaceDistribution(spaces, max(0, availableHeight-totalFixedHeight))
	for _, layer := range layers {
		if layer == nil { // Is space.
			yOffset += spaceDistrib[spaceIndex]
			spaceIndex++
			continue
		}
		yOffset += layer.GetY()
		layer.Y(yOffset).Z(1)
		yOffset += layer.Bounds().Dy()
	}

	return lipgloss.NewLayer("layout_vertical", "").
		Z(1).
		AddLayers(filterNilLayers(layers)...)
}
