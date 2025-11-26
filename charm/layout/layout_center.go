// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

var _ Layout = (*centerLayout)(nil)

type centerLayout struct {
	child any
}

// Center creates a new layout that centers the provided children.
func Center(child any) Layout {
	if child == nil {
		return nil
	}
	return &centerLayout{child: child}
}

func (r *centerLayout) Render(availableWidth, availableHeight int) Layer {
	if r.child == nil {
		return nil
	}

	layer := resolveLayer(r.child, availableWidth, availableHeight)
	if layer == nil {
		return nil
	}

	bounds := layer.Bounds()

	return layer.
		X(max(0, (availableWidth-bounds.Dx())/2)).
		Y(max(0, (availableHeight-bounds.Dy())/2)).
		Z(1)
}
