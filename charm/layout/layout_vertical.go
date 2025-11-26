// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

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

func (r *verticalLayout) Render(availableWidth, availableHeight int) Layer {
	if len(r.children) == 0 {
		return nil
	}

	var spaces int
	var totalFixedHeight int

	layers := make([]Layer, 0, len(r.children))

	for _, child := range r.children {
		if IsSpace(child) {
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
		layer.Y(yOffset).Z(2)
		yOffset += layer.Bounds().Dy()
	}

	return NewLayer("", "").
		Z(1).
		AddChild(filterNilLayers(layers)...)
}
