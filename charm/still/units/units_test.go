// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package units_test

import (
	"testing"

	"github.com/lrstanley/x/charm/still/units"
)

func TestPxInt(t *testing.T) {
	t.Parallel()
	if got := units.Px(12).Int(); got != 12 {
		t.Fatalf("Int() = %d, want 12", got)
	}
}

func TestPtPixels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pt   units.Pt
		dpi  units.DPI
		want int
	}{
		{11, 96, 15},
		{12, 96, 16},
		{0.5, 96, 1},
	}
	for _, tt := range tests {
		if got := tt.pt.Pixels(tt.dpi).Int(); got != tt.want {
			t.Fatalf("Pt(%v).Pixels(%v) = %d, want %d", tt.pt, tt.dpi, got, tt.want)
		}
	}
}

func TestPercentFloat64(t *testing.T) {
	t.Parallel()
	if got := units.Percent(0.2).Float64(); got != 0.2 {
		t.Fatalf("Float64() = %v, want 0.2", got)
	}
}

func TestFractionalPxFloat64(t *testing.T) {
	t.Parallel()
	if got := units.FractionalPx(10.5).Float64(); got != 10.5 {
		t.Fatalf("Float64() = %v, want 10.5", got)
	}
}

func TestNewAdjustment_clamps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   float64
		want float64
	}{
		{0.5, 0.5},
		{-2, -1},
		{2, 1},
	}
	for _, tt := range tests {
		if got := float64(units.NewAdjustment(tt.in)); got != tt.want {
			t.Fatalf("NewAdjustment(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestAdjustmentApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		adj  units.Adjustment
		base int
		want int
	}{
		{0, 10, 10},
		{0.2, 10, 12},
		{-0.5, 10, 5},
		{0, 0, 1},
	}
	for _, tt := range tests {
		if got := tt.adj.Apply(tt.base); got != tt.want {
			t.Fatalf("Apply(%d) = %d, want %d", tt.base, got, tt.want)
		}
	}
}

func TestClamp(t *testing.T) {
	t.Parallel()

	if got := units.Clamp(5, 0, 10); got != 5 {
		t.Fatalf("Clamp(5) = %d, want 5", got)
	}
	if got := units.Clamp(-1, 0, 10); got != 0 {
		t.Fatalf("Clamp(-1) = %d, want 0", got)
	}
	if got := units.Clamp(11, 0, 10); got != 10 {
		t.Fatalf("Clamp(11) = %d, want 10", got)
	}
}
