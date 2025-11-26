// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

// Cell represents a cell in a Rows or Columns layout with percentage-based or exact sizing.
type Cell struct {
	percent  float64
	size     int
	hidePerc float64
	hideSize int
	child    any
}

// NewCell creates a new Cell with the specified percentage and child.
// The percentage is converted from int to float64 for convenience.
func NewCell(child any) *Cell {
	return &Cell{
		child: child,
	}
}

// Percent sets the percentage of available space this cell should occupy (0-1).
// If 0, the cell will receive an equal share of remaining space after
// percentage-based and exact-size cells. Setting a percentage unsets any exact size.
func (c *Cell) Percent(percent float64) *Cell {
	c.percent = clamp(percent, 0, 1)
	if c.percent > 0 {
		c.size = 0
	}
	return c
}

// Size sets the exact size (width for columns, height for rows) this cell should occupy.
// Setting an exact size unsets any percentage.
func (c *Cell) Size(size int) *Cell {
	c.size = max(0, size)
	if c.size > 0 {
		c.percent = 0
		c.hideSize = 0
	}
	return c
}

// HidePercent sets the minimum calculated percentage threshold. If the calculated
// percentage is less than this value, the cell will be hidden.
func (c *Cell) HidePercent(percent float64) *Cell {
	c.hidePerc = clamp(percent, 0, 1)
	return c
}

// HideSize sets the minimum calculated size threshold. If the calculated width or
// height is less than this value, the cell will be hidden.
func (c *Cell) HideSize(hideSize int) *Cell {
	c.hideSize = max(0, hideSize)
	return c
}

// CalculateSize calculates the actual size this cell should occupy based on:
// - totalSize: the total available size (width for columns, height for rows)
// - usedPercent: the sum of percentages from all cells with Percent > 0
// - zeroPercentCount: the number of cells with Percent == 0
//
// If this cell has Percent > 0, it uses that percentage.
// If this cell has Percent == 0, it gets an equal share of remaining space.
func (c *Cell) CalculateSize(totalSize int, usedPercent float64, zeroPercentCount int) int {
	if c.percent > 0 {
		// Use percentage-based sizing
		return int(float64(totalSize) * c.percent)
	}

	// Zero-percent cells get equal share of remaining space
	if zeroPercentCount <= 0 {
		return 0
	}
	remainingPercent := 1.0 - usedPercent
	remainingSize := int(float64(totalSize) * remainingPercent)
	return remainingSize / zeroPercentCount
}

// CalculatedPercent calculates the actual percentage this cell occupies after size calculation.
func (c *Cell) CalculatedPercent(calculatedSize int, totalSize int) float64 {
	if totalSize == 0 {
		return 0
	}
	return float64(calculatedSize) / float64(totalSize)
}

// ShouldHide determines if this cell should be hidden based on HidePerc and HideSize thresholds.
// It checks both the calculated percentage and calculated size against the thresholds.
func (c *Cell) ShouldHide(calculatedSize int, calculatedPerc float64) bool {
	if c.hideSize > 0 && calculatedSize < c.hideSize {
		return true
	}
	if c.hidePerc > 0 && calculatedPerc < c.hidePerc {
		return true
	}
	return false
}
