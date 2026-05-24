// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"testing"
	"time"

	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
)

func TestNewSnapshot(t *testing.T) {
	t.Parallel()

	fixed := time.Unix(100, 0)
	opts := config.Options{
		Now:               func() time.Time { return fixed },
		BoxThickness:      units.Px(2),
		BackgroundOpacity: 0.5,
	}
	snap := config.NewSnapshot(opts, types.Metrics{}, nil)

	if !snap.Now.Equal(fixed) {
		t.Fatalf("Now = %v, want %v", snap.Now, fixed)
	}
	if !snap.BoxThicknessOverride {
		t.Fatal("BoxThicknessOverride = false, want true")
	}
	if snap.BackgroundOpacity != 0.5 {
		t.Fatalf("BackgroundOpacity = %v, want 0.5", snap.BackgroundOpacity)
	}
}
