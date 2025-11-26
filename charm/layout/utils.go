// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import (
	"cmp"
	"fmt"
	"os"
)

func printLayer(layer Layer) {
	f, err := os.OpenFile("layers.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	_, _ = f.WriteString(layer.String())
}

func clamp[T cmp.Ordered](value, min, max T) T {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func filterNil(slice []any) []any {
	v := make([]any, 0, len(slice))
	for _, item := range slice {
		if item != nil {
			v = append(v, item)
		}
	}
	return v
}

func filterNilLayers(layers []Layer) []Layer {
	v := make([]Layer, 0, len(layers))
	for _, layer := range layers {
		if layer != nil {
			v = append(v, layer)
		}
	}
	return v
}

// getID returns the ID of the model. The model can implement one of the following methods
// (in this order):
//
//   - GetID() string
//   - ID() string
//   - UUID() string
//
// If the model does not implement any of these methods, an empty string is returned.
func getID(model any) string {
	if model == nil {
		return ""
	}

	type hasGetID interface {
		GetID() string
	}
	if v, ok := model.(hasGetID); ok {
		return v.GetID()
	}

	type hasID interface {
		ID() string
	}
	if v, ok := model.(hasID); ok {
		return v.ID()
	}

	type hasUUID interface {
		UUID() string
	}
	if v, ok := model.(hasUUID); ok {
		return v.UUID()
	}

	return ""
}

// resolveLayer resolves a child into a [Layer]. A child can be one of many types,
// primarily either a resulting type, or a model which returns a resulting type
// through a "View" method. The following types are supported (and in the provided
// order):
//
//   - Layer
//   - Layout
//   - string
//   - View() Layer
//   - View() Layout
//   - View() string
//   - View(availableWidth, availableHeight) Layer
//   - View(availableWidth, availableHeight) Layout
//   - View(availableWidth, availableHeight) string
func resolveLayer(child any, availableWidth, availableHeight int) Layer {
	if child == nil {
		return nil
	}

	switch v := child.(type) {
	case Layer:
		return v
	case Layout:
		return v.Render(availableWidth, availableHeight)
	case string:
		return NewLayer("", v)
	case interface{ View() Layer }:
		return v.View()
	case interface{ View() Layout }:
		return v.View().Render(availableWidth, availableHeight)
	case interface{ View() string }:
		view := v.View()
		return NewLayer(getID(v), view)
	case interface{ View(int, int) Layer }:
		return v.View(availableWidth, availableHeight)
	case interface{ View(int, int) Layout }:
		return v.View(availableWidth, availableHeight).Render(availableWidth, availableHeight)
	case interface{ View(int, int) string }:
		view := v.View(availableWidth, availableHeight)
		return NewLayer(getID(v), view)
	default:
		panic(fmt.Sprintf("unsupported child type: %T", child))
	}
}

func calculateSpaceDistribution(numSpaces, remainingSpace int) []int {
	if numSpaces <= 0 {
		return nil
	}

	out := make([]int, numSpaces)
	if remainingSpace <= 0 {
		return out
	}

	spaceSize := remainingSpace / numSpaces
	spaceRemainder := remainingSpace % numSpaces

	for i := range numSpaces {
		out[i] = spaceSize
		if spaceRemainder > 0 {
			out[i]++
			spaceRemainder--
		}
	}
	return out
}
