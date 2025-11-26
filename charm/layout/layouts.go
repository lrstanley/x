// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package layout

// Layout is a generic layout interface. All layouts must implement this interface.
type Layout interface {
	// Render renders the layout into a [lipgloss.Layer]. The child can use the provided
	// availableWidth and availableHeight to calculate the size of the layout it can
	// consume.
	Render(availableWidth, availableHeight int) Layer
}

var (
	_ Layout = (*baseLayout)(nil)
	_ Layout = (*spacer)(nil)
)

// baseLayout is a base layout implementation that has no-op generation methods.
type baseLayout struct{}

func (r *baseLayout) Render(_, _ int) Layer {
	return nil
}

type spacer struct {
	baseLayout
}

// Space creates a new space layout, which will consume all free space that is available.
func Space() Layout {
	return &spacer{}
}

func IsSpace(child any) bool {
	if child == nil {
		return false
	}
	if _, isSpace := child.(*spacer); isSpace {
		return true
	}
	return false
}
