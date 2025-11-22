// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import (
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/charm/layout/internal/pool"
)

var layerPool = pool.New(func() []*lipgloss.Layer {
	return make([]*lipgloss.Layer, 0, 3)
})

// Layout is a generic layout interface. All layouts must implement this interface.
type Layout interface {
	// Render renders the layout into a [lipgloss.Layer]. The child can use the provided
	// availableWidth and availableHeight to calculate the size of the layout it can
	// consume.
	Render(availableWidth, availableHeight int) *lipgloss.Layer
}

var (
	_ Layout = (*baseLayout)(nil)
	_ Layout = (*verticalLayout)(nil)
	_ Layout = (*horizontalLayout)(nil)
	_ Layout = (*centerLayout)(nil)
	_ Layout = (*stackLayout)(nil)
	_ Layout = (*spacer)(nil)
	_ Layout = (*frameLayout)(nil)
)

// baseLayout is a base layout implementation that has no-op generation methods.
type baseLayout struct{}

func (r *baseLayout) Render(_, _ int) *lipgloss.Layer {
	return nil
}

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

func (r *centerLayout) Render(availableWidth, availableHeight int) *lipgloss.Layer {
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

func (r *stackLayout) Render(availableWidth, availableHeight int) *lipgloss.Layer {
	if len(r.children) == 0 {
		return nil
	}

	layers := layerPool.Get()
	defer func() {
		layers = layers[:0]
		layerPool.Put(layers)
	}()

	for _, child := range r.children {
		if _, isSpace := child.(*spacer); isSpace {
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

	var baseZ int

	for _, layer := range layers {
		baseZ = max(baseZ, layer.MaxZ())
	}

	for z, layer := range layers {
		layer.Z(baseZ + z + 1)
	}

	return lipgloss.NewLayer("layout_stack", "").Z(1).AddLayers(layers...)
}

type spacer struct {
	baseLayout
}

// Space creates a new space layout, which will consume all free space that is available.
func Space() Layout {
	return &spacer{}
}

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

func (r *frameLayout) Render(availableWidth, availableHeight int) *lipgloss.Layer {
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
	return lipgloss.NewLayer(
		"layout_frame",
		r.style.
			Width(bounds.Dx()+hFrame).
			Height(bounds.Dy()+vFrame).
			Render(),
	).Z(1).AddLayers(layer.X(hFrame / 2).Y(vFrame / 2).Z(2))
}
