// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import (
	"fmt"
	"image"
	"iter"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func LayerTreeIter(layer Layer) iter.Seq[Layer] {
	return func(yield func(Layer) bool) {
		if !yield(layer) {
			return
		}
		for _, layer := range layer.GetChildren() {
			if !yield(layer) {
				return
			}
			LayerTreeIter(layer)(yield)
		}
	}
}

func GetLayerByID(layer Layer, id string) Layer {
	for l := range LayerTreeIter(layer) {
		if l.GetID() == id {
			return l
		}
	}
	return nil
}

func getMaxLayerZ(layers []Layer) int {
	maxZ := 0
	for _, l := range layers {
		for l := range LayerTreeIter(l) {
			maxZ = max(maxZ, l.GetZ())
		}
	}
	return maxZ
}

type Layer interface {
	// GetID returns the ID of the Layer.
	GetID() string
	// GetX returns the x-offset of the Layer, relative to the root.
	GetX() int
	// GetY returns the y-offset of the Layer, relative to the root.
	GetY() int
	// GetZ returns the z-index of the Layer.
	GetZ() int
	// X sets the x-offset of the Layer.
	X(x int) Layer
	// Y sets the y-offset of the Layer.
	Y(y int) Layer
	// Z sets the z-index of the Layer.
	Z(z int) Layer
	// GetRoot returns the root [Layer] of the Layer, returning itself if it has no parent.
	GetRoot() Layer
	// Parent sets the parent [Layer] of the Layer.
	Parent(layer Layer) Layer
	// GetParent returns the parent [Layer] of the Layer.
	GetParent() Layer
	// GetChildren returns the children [Layer] of the Layer.
	GetChildren() []Layer
	// AddChild adds child layers to the Layer.
	AddChild(layers ...Layer) Layer
	// Bounds returns the bounds of the Layer as a [image.Rectangle].
	Bounds() image.Rectangle
	// Draw draws the [Layer] and its children onto the given [uv.Screen].
	Draw(scr uv.Screen, area image.Rectangle)
	// DrawSelf draws the content of the [Layer] onto the given [uv.Screen].
	DrawSelf(scr uv.Screen, area image.Rectangle)
	// Hit checks if the given point hits the [Layer] or any of its child layers. If
	// a hit is detected, it returns the ID of the top-most [Layer] that was hit. If
	// no hit is detected, it returns an empty string.
	Hit(x, y int) string
	// String returns a debug-friendly string representation of the [Layer] and all
	// its children. It is indented based on the depth of the [Layer] relative to the
	// parent. This does not render the actual layer -- [Layer.Draw] is how you would
	// achieve that.
	String() string
}

var _ Layer = (*layer)(nil)

// layer holds metadata about a layer.
type layer struct {
	id            string
	content       string
	width, height int
	x, y, z       int

	parent   Layer
	children []Layer
}

// NewLayer creates a new [Layer] with the given id and styled content.
func NewLayer(id, content string, layers ...Layer) Layer {
	l := &layer{
		id:      id,
		content: content,
	}

	if content != "" {
		l.width = lipgloss.Width(content)
		l.height = lipgloss.Height(content)
	}

	l.AddChild(layers...)
	return l
}

// GetID returns the ID of the Layer.
func (l *layer) GetID() string {
	return l.id
}

// X sets the x-coordinate of the Layer.
func (l *layer) X(x int) Layer {
	l.x = x
	return l
}

// Y sets the y-coordinate of the Layer.
func (l *layer) Y(y int) Layer {
	l.y = y
	return l
}

// Z sets the z-index of the Layer.
func (l *layer) Z(z int) Layer {
	l.z = z
	return l
}

// GetX returns the x-coordinate of the Layer.
func (l *layer) GetX() int {
	if l.parent != nil {
		return l.parent.GetX() + l.x
	}
	return l.x
}

// GetY returns the y-coordinate of the Layer.
func (l *layer) GetY() int {
	if l.parent != nil {
		return l.parent.GetY() + l.y
	}
	return l.y
}

// GetZ returns the z-index of the Layer.
func (l *layer) GetZ() int {
	return l.z
}

// GetRoot returns the root [Layer] of the Layer, returning itself if it has no parent.
func (l *layer) GetRoot() Layer {
	if l.parent == nil {
		return l
	}
	return l.parent.GetRoot()
}

// Parent sets the parent [Layer] of the Layer.
func (l *layer) Parent(parent Layer) Layer {
	l.parent = parent
	return l
}

// GetParent returns the parent [Layer] of the Layer.
func (l *layer) GetParent() Layer {
	return l.parent
}

// GetChildren returns the children [Layer] of the Layer.
func (l *layer) GetChildren() []Layer {
	return l.children
}

// Bounds returns the bounds of the Layer as a [image.Rectangle].
func (l *layer) Bounds() image.Rectangle {
	// Calculate bounds based on child layers
	x, y := l.GetX(), l.GetY()
	this := image.Rectangle{
		Min: image.Pt(x, y),
		Max: image.Pt(x+l.width, y+l.height),
	}
	for _, layer := range l.children {
		this = this.Union(layer.Bounds())
	}

	// Adjust the size of the layer if it's negative
	if this.Min.X < 0 {
		this = this.Add(image.Pt(-this.Min.X, 0))
	}
	if this.Min.Y < 0 {
		this = this.Add(image.Pt(0, -this.Min.Y))
	}

	return this
}

// Hit checks if the given point hits the [Layer] or any of its child layers. If
// a hit is detected, it returns the ID of the top-most [Layer] that was hit. If
// no hit is detected, it returns an empty string.
func (l *layer) Hit(x, y int) string {
	// Reverse the order of the layers so that the top-most layer is checked
	// first.
	layers := slices.Collect(LayerTreeIter(l))
	sortByZ(layers)

	for i := len(layers) - 1; i >= 0; i-- {
		if layers[i].GetID() != "" && image.Pt(x, y).In(layers[i].Bounds()) {
			return layers[i].GetID()
		}
	}

	return ""
}

// AddChild adds child layers to the Layer.
func (l *layer) AddChild(layers ...Layer) Layer {
	for i, layer := range layers {
		if layer == nil {
			panic(fmt.Sprintf("layer at index %d is nil", i))
		}
		l.children = append(l.children, layer.Parent(l))
	}
	return l
}

// DrawSelf draws the content of the [Layer] onto the given [uv.Screen].
func (l *layer) DrawSelf(scr uv.Screen, area image.Rectangle) {
	if l.content != "" {
		if bounds := l.Bounds(); bounds.Overlaps(area) {
			uv.NewStyledString(l.content).Draw(scr, area.Intersect(bounds))
		}
	}
}

// Draw draws the [Layer] and its children onto the given [uv.Screen].
func (l *layer) Draw(scr uv.Screen, area image.Rectangle) {
	layers := slices.Collect(LayerTreeIter(l))
	sortByZ(layers)

	for _, l := range layers {
		l.DrawSelf(scr, area.Intersect(l.Bounds()))
	}
}

// String returns a debug-friendly string representation of the [Layer] and all
// its children. It is indented based on the depth of the [Layer] relative to the
// parent. This does not render the actual layer -- [Layer.Draw] is how you would
// achieve that.
func (l *layer) String() string {
	indent := strings.Repeat("  ", layerDepth(l))
	bounds := l.Bounds()

	var sb strings.Builder

	sb.WriteString(indent + fmt.Sprintf(
		"Layer(id:%q, z:%d, x:%v, y:%v, w:%d, h:%d",
		l.id, l.z, bounds.Min.X, bounds.Min.Y, bounds.Dx(), bounds.Dy(),
	))

	if l.content != "" {
		sb.WriteString(", content:true)\n")
	} else {
		sb.WriteString(")\n")
	}

	layers := l.children
	sortByZ(layers)

	for _, layer := range layers {
		sb.WriteString(layer.String())
	}

	return sb.String()
}

// sortByZ sorts layers by their z-index.
func sortByZ(layers []Layer) {
	slices.SortFunc(layers, func(a, b Layer) int {
		return a.GetZ() - b.GetZ()
	})
}

func layerDepth(layer Layer) int {
	if layer.GetParent() == nil {
		return 0
	}
	return layerDepth(layer.GetParent()) + 1
}
