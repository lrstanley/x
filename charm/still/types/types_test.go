// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package types_test

import (
	"image"
	"testing"

	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
)

func TestMetricsCellSize(t *testing.T) {
	t.Parallel()

	m := types.Metrics{
		CellWidth:  units.Px(8),
		CellHeight: units.Px(16),
	}
	if got := m.CellSize(); got != image.Pt(8, 16) {
		t.Fatalf("CellSize() = %v, want (8,16)", got)
	}
}
