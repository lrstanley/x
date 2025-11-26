// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

var _ Layout = (*stackLayout)(nil)

type stackLayout struct {
	children []any
}

// Stack creates a new stacked layout with the provided children, where each
// child (left to right) is stacked on top of the previous one, with an increasing
// Z-index.
func Stack(children ...any) Layout {
	children = filterNil(children)
	if len(children) == 0 {
		return nil
	}
	return &stackLayout{children: children}
}

func (r *stackLayout) Render(availableWidth, availableHeight int) Layer {
	if len(r.children) == 0 {
		return nil
	}

	layers := make([]Layer, 0, len(r.children))

	for _, child := range r.children {
		if IsSpace(child) {
			continue // Spaces are ignored in Stack.
		}
		layer := resolveLayer(child, availableWidth, availableHeight)
		if layer == nil {
			continue
		}
		layers = append(layers, layer)
	}

	switch len(layers) {
	case 0:
		return nil
	case 1:
		return layers[0].Z(1)
	}

	baseZ := getMaxLayerZ(layers)

	for z, layer := range layers {
		layer.Z(baseZ + z + 1)
	}

	return NewLayer("", "").Z(1).AddChild(layers...)
}
