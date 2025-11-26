// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

var (
	_ Layout = (*leftPaddingLayout)(nil)
	_ Layout = (*rightPaddingLayout)(nil)
	_ Layout = (*topPaddingLayout)(nil)
	_ Layout = (*bottomPaddingLayout)(nil)
)

type leftPaddingLayout struct {
	amount int
	child  any
}

// LeftPadding creates a new layout that pads the provided child with the given amount of space on the left.
func LeftPadding(amount int, child any) Layout {
	if child == nil {
		return nil
	}
	return &leftPaddingLayout{amount: max(0, amount), child: child}
}

func (r *leftPaddingLayout) Render(availableWidth, availableHeight int) Layer {
	if IsSpace(r.child) {
		return nil
	}

	layer := resolveLayer(r.child, availableWidth-r.amount, availableHeight)
	if layer == nil {
		return nil
	}

	return layer.X(r.amount)
}

type rightPaddingLayout struct {
	amount int
	child  any
}

// RightPadding creates a new layout that pads the provided child with the given amount of space on the right.
func RightPadding(amount int, child any) Layout {
	if child == nil {
		return nil
	}
	return &rightPaddingLayout{amount: max(0, amount), child: child}
}

func (r *rightPaddingLayout) Render(availableWidth, availableHeight int) Layer {
	if IsSpace(r.child) {
		return nil
	}

	layer := resolveLayer(r.child, availableWidth-r.amount, availableHeight)
	if layer == nil {
		return nil
	}

	return layer
}

type topPaddingLayout struct {
	amount int
	child  any
}

// TopPadding creates a new layout that pads the provided child with the given amount of space on the top.
func TopPadding(amount int, child any) Layout {
	if child == nil {
		return nil
	}
	return &topPaddingLayout{amount: max(0, amount), child: child}
}

func (r *topPaddingLayout) Render(availableWidth, availableHeight int) Layer {
	if IsSpace(r.child) {
		return nil
	}

	layer := resolveLayer(r.child, availableWidth, availableHeight-r.amount)
	if layer == nil {
		return nil
	}

	return layer.Y(r.amount)
}

type bottomPaddingLayout struct {
	amount int
	child  any
}

// BottomPadding creates a new layout that pads the provided child with the given amount of space on the bottom.
func BottomPadding(amount int, child any) Layout {
	if child == nil {
		return nil
	}
	return &bottomPaddingLayout{amount: max(0, amount), child: child}
}

func (r *bottomPaddingLayout) Render(availableWidth, availableHeight int) Layer {
	if IsSpace(r.child) {
		return nil
	}

	layer := resolveLayer(r.child, availableWidth, availableHeight-r.amount)
	if layer == nil {
		return nil
	}
	return layer
}
