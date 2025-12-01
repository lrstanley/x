// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

import "charm.land/lipgloss/v2"

var _ Layout = (*rowsLayout)(nil)

type rowsLayout struct {
	cells []*Cell
}

// Rows creates a new vertical layout with the provided cells, where each cell
// is sized based on its percentage of available height. Cells are arranged
// top to bottom.
func Rows(cells ...*Cell) Layout {
	if len(cells) == 0 {
		return nil
	}
	return &rowsLayout{cells: cells}
}

func (r *rowsLayout) Render(availableWidth, availableHeight int) *lipgloss.Layer {
	if len(r.cells) == 0 {
		return nil
	}

	// Validate all cell percentages
	var totalPercent float64
	var zeroPercentCount int
	for _, cell := range r.cells {
		if cell.size > 0 {
			// Exact-size cells don't count toward percentage validation
			continue
		}
		if cell.percent > 0 {
			totalPercent += cell.percent
		} else {
			zeroPercentCount++
		}
	}

	// Panic if total percentage exceeds 100%
	if totalPercent > 1.0 {
		panic("rows layout: total cell percentages exceed 100%")
	}

	// First pass: determine which cells should be hidden
	visibleCells := make([]*Cell, 0, len(r.cells))
	for _, cell := range r.cells {
		var size int
		if cell.size > 0 {
			size = cell.size
		} else {
			size = cell.CalculateSize(availableHeight, totalPercent, zeroPercentCount)
		}
		calculatedPerc := cell.CalculatedPercent(size, availableHeight)
		if !cell.ShouldHide(size, calculatedPerc) {
			visibleCells = append(visibleCells, cell)
		}
	}

	if len(visibleCells) == 0 {
		return nil
	}

	// Second pass: recalculate sizes for visible cells only
	var visibleTotalPercent float64
	var visibleZeroPercentCount int
	for _, cell := range visibleCells {
		if cell.size > 0 {
			// Exact-size cells don't count toward percentage calculation
			continue
		}
		if cell.percent > 0 {
			visibleTotalPercent += cell.percent
		} else {
			visibleZeroPercentCount++
		}
	}

	// Render visible cells with recalculated sizes
	layers := make([]*lipgloss.Layer, 0, len(visibleCells))

	// Calculate sizes for all cells, ensuring total equals availableHeight
	sizes := make([]int, len(visibleCells))
	usedSize := 0

	// First pass: allocate exact-size cells
	for i, cell := range visibleCells {
		if cell.size > 0 {
			sizes[i] = cell.size
			usedSize += sizes[i]
		}
	}

	// Second pass: allocate percentage-based cells (percentages are relative to total available space)
	for i, cell := range visibleCells {
		if cell.size == 0 && cell.percent > 0 {
			sizes[i] = int(float64(availableHeight) * cell.percent)
			usedSize += sizes[i]
		}
	}

	// Third pass: allocate remaining space to zero-percent cells
	remainingSize := availableHeight - usedSize
	if visibleZeroPercentCount > 0 && remainingSize > 0 {
		perCellSize := remainingSize / visibleZeroPercentCount
		remainder := remainingSize % visibleZeroPercentCount

		zeroCount := 0
		for i, cell := range visibleCells {
			if cell.size == 0 && cell.percent == 0 {
				sizes[i] = perCellSize
				if zeroCount < remainder {
					sizes[i]++
				}
				zeroCount++
			}
		}
	}

	yOffset := 0
	for i, cell := range visibleCells {
		size := sizes[i]

		// Render the child with the recalculated height
		layer := resolveLayer(cell.child, availableWidth, size)
		if layer == nil {
			continue
		}

		layer.Y(yOffset).Z(1)
		layers = append(layers, layer)
		yOffset += size
	}

	switch len(layers) {
	case 0:
		return nil
	case 1:
		return layers[0]
	}

	return lipgloss.NewLayer("").
		Z(1).
		AddLayers(layers...)
}
