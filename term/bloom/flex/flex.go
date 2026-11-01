// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package flex shares a fixed number of terminal cells between tracks, the
// rows or columns of a container, using integer arithmetic only.
//
// [Split] decides sizes and [Place] decides positions. Neither allocates when
// given a destination slice with enough capacity, so both are safe to call on
// every frame.
package flex

import "slices"

// Collapsed is the size [Split] reports for a track it dropped because the
// minimum sizes of all tracks did not fit the budget.
const Collapsed = -1

// Track describes how one child takes part in sharing the main axis.
type Track struct {
	// Basis is the size before growing or shrinking.
	Basis int
	// Min and Max clamp the final size. A Max of zero or less is unbounded.
	Min, Max int
	// Grow is this track's share of leftover space, relative to the Grow of
	// the other tracks.
	Grow int
	// Shrink is this track's share of any deficit, weighted by its basis. A
	// track with zero Shrink never goes below its basis.
	Shrink int
	// Collapse makes the track collapsible. When even the minimum sizes do
	// not fit, collapsible tracks are dropped lowest Collapse first, and the
	// last one when tied.
	Collapse int
}

// Fixed is a track of exactly n cells.
func Fixed(n int) Track { return Track{Basis: n, Min: n, Max: n} }

// Fill is a track that starts empty and takes a weighted share of the
// leftover space.
func Fill(weight int) Track { return Track{Grow: weight, Shrink: 1} }

// Auto is a track that starts at its content size n and can shrink.
func Auto(n int) Track { return Track{Basis: n, Shrink: 1} }

// floor is the smallest size the track can be given.
func (t Track) floor() int {
	if t.Shrink <= 0 {
		return t.clamp(t.Basis)
	}
	return max(t.Min, 0)
}

// Empty reports whether the track can never be given a cell, such as a
// fixed track of zero. Like a collapsed track, it takes no gap.
func (t Track) Empty() bool { return t.Grow <= 0 && t.clamp(t.Basis) == 0 }

func (t Track) clamp(n int) int {
	if t.Max > 0 && n > t.Max {
		n = t.Max
	}
	return max(n, t.Min, 0)
}

// Split writes the size of each track into dst, reusing its storage, and
// returns it. Sizes add up to budget, minus the gaps between tracks that are
// neither collapsed nor empty (see [Place]),
// whenever the tracks allow: leftover space goes to growing tracks and a
// deficit is taken from shrinking ones, both in proportion to their weights,
// with any rounding remainder going to earlier tracks. If the minimum sizes
// still do not fit, collapsible tracks are dropped and reported as
// [Collapsed]; if that is not enough, the sizes overflow the budget.
func Split(dst []int, budget, gap int, tracks []Track) []int {
	n := len(tracks)
	dst = slices.Grow(dst[:0], n)[:n]
	clear(dst)

	visible := 0
	for _, t := range tracks {
		if !t.Empty() {
			visible++
		}
	}
	for {
		need := gap * max(visible-1, 0)
		victim, lowest := -1, 0
		for i, t := range tracks {
			if dst[i] == Collapsed || t.Empty() {
				continue
			}
			need += t.floor()
			if t.Collapse > 0 && (victim < 0 || t.Collapse <= lowest) {
				victim, lowest = i, t.Collapse
			}
		}
		if need <= budget || victim < 0 {
			break
		}
		dst[victim] = Collapsed
		visible--
	}

	resolve(dst, tracks, budget-gap*max(visible-1, 0))
	return dst
}

// resolve sizes the visible tracks to fill avail, the way CSS resolves
// flexible lengths: free space (or a deficit) is shared by weight from each
// track's basis, then sizes are clamped to Min and Max. If clamping moved the
// total, the tracks that were clamped are frozen and the rest share again.
func resolve(dst []int, tracks []Track, avail int) {
	sum := 0
	for i, t := range tracks {
		if dst[i] != Collapsed {
			dst[i] = t.clamp(t.Basis)
			sum += dst[i]
		}
	}
	if sum == avail {
		return
	}
	growing := sum < avail

	// weight is a track's share of the free space; zero means it keeps its
	// clamped basis.
	weight := func(t Track) int {
		if growing {
			return t.Grow
		}
		return t.Shrink * t.Basis
	}

	// state is 0 for a track still sharing, frozen for one that keeps its
	// size, or the direction a clamp moved it in this round.
	const (
		sharing int8 = iota
		frozen
		raised
		lowered
	)
	var buf [64]int8
	state := buf[:0]
	if len(tracks) > len(buf) {
		state = make([]int8, 0, len(tracks))
	}
	for i, t := range tracks {
		if dst[i] == Collapsed || weight(t) <= 0 {
			state = append(state, frozen)
		} else {
			state = append(state, sharing)
		}
	}

	for range len(tracks) + 1 {
		free, weights := avail, 0
		for i, t := range tracks {
			switch {
			case dst[i] == Collapsed:
			case state[i] == frozen:
				free -= dst[i]
			default:
				free -= t.Basis
				weights += weight(t)
			}
		}
		if weights == 0 {
			return
		}

		// Shares come from rounding cumulative edges, so they add up to
		// exactly free without a remainder pass.
		sign := 1
		if free < 0 {
			sign, free = -1, -free
		}
		violation, cum, edge := 0, 0, 0
		for i, t := range tracks {
			if state[i] == frozen {
				continue
			}
			cum += weight(t)
			next := free * cum / weights
			target := t.Basis + sign*(next-edge)
			edge = next
			dst[i] = t.clamp(target)
			violation += dst[i] - target
			switch {
			case dst[i] > target:
				state[i] = raised
			case dst[i] < target:
				state[i] = lowered
			default:
				state[i] = sharing
			}
		}
		if violation == 0 {
			return
		}

		// Freeze the tracks clamped in the direction of the total violation,
		// as CSS does, so the others make up the difference.
		for i := range state {
			if (violation > 0 && state[i] == raised) || (violation < 0 && state[i] == lowered) {
				state[i] = frozen
			}
		}
	}
}

// Justify is where tracks go on the main axis when they do not fill it.
type Justify uint8

const (
	// Start packs tracks at the start.
	Start Justify = iota
	// Center packs tracks in the middle.
	Center
	// End packs tracks at the end.
	End
	// SpaceBetween puts the free space between tracks, none at the edges.
	SpaceBetween
	// SpaceAround puts equal space around each track, so the edges get half
	// as much as the space between tracks.
	SpaceAround
	// SpaceEvenly puts equal space between tracks and at both edges.
	SpaceEvenly
)

// Place writes the offset of each track into dst, reusing its storage, and
// returns it. sizes are the result of [Split]; tracks that are collapsed or
// have no cells get the offset of the track after them and take no gap, so
// a track animating to zero leaves no gap behind.
func Place(dst, sizes []int, budget, gap int, justify Justify) []int {
	n := len(sizes)
	dst = slices.Grow(dst[:0], n)[:n]

	visible, used := 0, 0
	for _, s := range sizes {
		if s > 0 {
			visible++
			used += s
		}
	}
	if visible == 0 {
		clear(dst)
		return dst
	}
	used += gap * (visible - 1)
	free := max(budget-used, 0)

	// lead is the space before the first track, and extra the space between
	// tracks on top of gap, handed out over the gaps by cumulative edges.
	lead, spaces, between := 0, 0, 0
	switch justify {
	case Start:
	case Center:
		lead = free / 2
	case End:
		lead = free
	case SpaceBetween:
		spaces, between = visible-1, free
	case SpaceAround:
		lead = free / (2 * visible)
		spaces, between = visible-1, free-2*lead
	case SpaceEvenly:
		lead = free / (visible + 1)
		spaces, between = visible-1, free-2*lead
	}

	pos, seen, edge := lead, 0, 0
	for i, s := range sizes {
		dst[i] = pos
		if s <= 0 {
			continue
		}
		seen++
		pos += s
		if seen < visible {
			pos += gap
			if spaces > 0 {
				next := between * seen / spaces
				pos += next - edge
				edge = next
			}
		}
	}
	return dst
}
