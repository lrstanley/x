// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import "testing"

func TestCircleMask(t *testing.T) {
	t.Parallel()

	mask := CircleMask(10)
	if got := mask.Bounds().Dx(); got != 10 {
		t.Fatalf("width = %d, want 10", got)
	}
	if got := mask.AlphaAt(0, 0).A; got != 0 {
		t.Errorf("corner alpha = %d, want 0", got)
	}
	if got := mask.AlphaAt(5, 5).A; got != 255 {
		t.Errorf("center alpha = %d, want 255", got)
	}
	if edge := mask.AlphaAt(5, 0).A; edge == 0 || edge == 255 {
		t.Errorf("edge alpha = %d, want partial coverage", edge)
	}
	if got := CircleMask(-1).Bounds(); !got.Empty() {
		t.Errorf("negative diameter bounds = %v, want empty", got)
	}
}
