// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flex

import (
	"slices"
	"testing"
)

func TestSplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		budget int
		gap    int
		tracks []Track
		want   []int
	}{
		{"fixed and fill", 80, 0, []Track{Fixed(20), Fill(1)}, []int{20, 60}},
		{"gap", 80, 1, []Track{Fixed(20), Fill(1)}, []int{20, 59}},
		{"weights", 9, 0, []Track{Fill(1), Fill(2)}, []int{3, 6}},
		{"equal remainder goes first", 10, 0, []Track{Fill(1), Fill(1), Fill(1)}, []int{3, 3, 4}},
		{"fill max", 30, 0, []Track{{Grow: 1, Max: 5, Shrink: 1}, Fill(1)}, []int{5, 25}},
		{"fill min", 10, 0, []Track{{Grow: 1, Min: 8, Shrink: 1}, Fill(1)}, []int{8, 2}},
		{"fill max redistributes", 20, 0, []Track{{Grow: 3, Max: 6, Shrink: 1}, Fill(1)}, []int{6, 14}},
		{"auto keeps size", 80, 0, []Track{Auto(10), Fill(1)}, []int{10, 70}},
		{"auto shrinks", 10, 0, []Track{Auto(10), Auto(10)}, []int{5, 5}},
		{"auto shrinks by basis", 12, 0, []Track{Auto(8), Auto(16)}, []int{4, 8}},
		{"auto shrink stops at min", 10, 0, []Track{{Basis: 10, Min: 8, Shrink: 1}, Auto(10)}, []int{8, 2}},
		{"fixed never shrinks", 10, 0, []Track{Fixed(8), Auto(8)}, []int{8, 2}},
		{
			"collapse lowest priority first", 50, 0,
			[]Track{{Basis: 20, Min: 20, Max: 20, Collapse: 2}, {Grow: 1, Min: 30, Shrink: 1}, {Basis: 20, Min: 20, Max: 20, Collapse: 1}},
			[]int{20, 30, Collapsed},
		},
		{
			"collapse drops its gap", 31, 1,
			[]Track{{Basis: 20, Min: 20, Max: 20, Collapse: 1}, {Grow: 1, Min: 30, Shrink: 1}},
			[]int{Collapsed, 31},
		},
		{"empty takes no gap", 10, 1, []Track{Fixed(0), Fill(1), Auto(0)}, []int{0, 10, 0}},
		{"overflow without collapse", 10, 0, []Track{Fixed(8), Fixed(8)}, []int{8, 8}},
		{"no grow leaves space", 10, 0, []Track{Auto(3)}, []int{3}},
		{"empty", 10, 0, nil, []int{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Split(nil, tt.budget, tt.gap, tt.tracks)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Split(%d, %d) = %v, want %v", tt.budget, tt.gap, got, tt.want)
			}
		})
	}
}

func TestPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sizes   []int
		budget  int
		gap     int
		justify Justify
		want    []int
	}{
		{"start", []int{2, 2}, 10, 1, Start, []int{0, 3}},
		{"center", []int{2, 2}, 10, 0, Center, []int{3, 5}},
		{"end", []int{2, 2}, 10, 0, End, []int{6, 8}},
		{"between", []int{2, 2, 2}, 10, 0, SpaceBetween, []int{0, 4, 8}},
		{"around", []int{2, 2}, 12, 0, SpaceAround, []int{2, 8}},
		{"evenly", []int{2, 2}, 10, 0, SpaceEvenly, []int{2, 6}},
		{"collapsed takes no gap", []int{2, Collapsed, 2}, 10, 1, Start, []int{0, 3, 3}},
		{"empty takes no gap", []int{0, 2, 0, 2, 0}, 10, 1, Start, []int{0, 0, 3, 3, 5}},
		{"overflow packs from start", []int{8, 8}, 10, 0, End, []int{0, 8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Place(nil, tt.sizes, tt.budget, tt.gap, tt.justify)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Place(%v) = %v, want %v", tt.sizes, got, tt.want)
			}
		})
	}
}

func FuzzSplit(f *testing.F) {
	f.Add(80, 1, 20, 0, 10, 2, 30, 1)
	f.Add(10, 0, 8, 8, 0, 0, 8, 0)
	f.Add(3, 2, 0, 1, 5, 3, 0, 1)

	f.Fuzz(func(t *testing.T, budget, gap, basisA, growA, minA, basisB, growB, collapseB int) {
		budget, gap = clampFuzz(budget, 500), clampFuzz(gap, 5)
		tracks := []Track{
			{Basis: clampFuzz(basisA, 200), Grow: clampFuzz(growA, 5), Min: clampFuzz(minA, 100), Shrink: 1},
			{Basis: clampFuzz(basisB, 200), Grow: clampFuzz(growB, 5), Shrink: 1, Collapse: clampFuzz(collapseB, 3)},
			Fill(1),
		}

		sizes := Split(nil, budget, gap, tracks)
		visible, sum, floors := 0, 0, 0
		for i, s := range sizes {
			if s == Collapsed {
				if tracks[i].Collapse == 0 {
					t.Fatalf("track %d collapsed without Collapse: %v", i, sizes)
				}
				continue
			}
			if s < tracks[i].floor() {
				t.Fatalf("track %d = %d below its floor %d: %v", i, s, tracks[i].floor(), sizes)
			}
			if !tracks[i].Empty() {
				visible++
			}
			sum += s
			floors += tracks[i].floor()
		}
		avail := budget - gap*max(visible-1, 0)
		// A Fill track is always present, so space never goes unused; sizes
		// only overflow when even the floors do not fit.
		if floors <= avail && sum != avail {
			t.Fatalf("sizes %v add up to %d, want %d", sizes, sum, avail)
		}

		offsets := Place(nil, sizes, budget, gap, SpaceEvenly)
		prev := 0
		for i, s := range sizes {
			if offsets[i] < prev {
				t.Fatalf("offsets go backwards: %v for %v", offsets, sizes)
			}
			if s != Collapsed {
				prev = offsets[i] + s
			}
		}
	})
}

func clampFuzz(v, hi int) int {
	if v < 0 {
		v = -v
	}
	if v < 0 { // math.MinInt
		return 0
	}
	return v % (hi + 1)
}

func BenchmarkSplit(b *testing.B) {
	tracks := []Track{
		Fixed(20),
		{Grow: 2, Min: 30, Shrink: 1},
		Auto(18),
		{Basis: 24, Min: 24, Max: 24, Collapse: 1},
		Fill(1),
		Auto(6),
		Fill(3),
		Fixed(1),
	}
	sizes := make([]int, len(tracks))
	offsets := make([]int, len(tracks))
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		sizes = Split(sizes, 120+i%80, 1, tracks)
		offsets = Place(offsets, sizes, 120+i%80, 1, SpaceBetween)
	}
}
