// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"image"
	"testing"
	"testing/synctest"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// BenchmarkAdaptivePaletteCold palettizes a frame of antialiased ramps into a
// fresh adaptive palette, so every color is new and median cut runs.
func BenchmarkAdaptivePaletteCold(b *testing.B) {
	src := testutil.TerminalFrame(0)
	for i := 0; i < len(src.Pix); i += 4 {
		px := i / 4
		t := uint8(px % 251) //nolint:gosec // bounded.
		c := (px / 251) % 12
		src.Pix[i] = uint8(int(t) * (c*20 + 15) / 255)    //nolint:gosec // bounded.
		src.Pix[i+1] = uint8(int(t) * (255 - c*20) / 255) //nolint:gosec // bounded.
		src.Pix[i+2] = uint8(int(t)*(c*7+100)/255 + 30)   //nolint:gosec // bounded.
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ap := NewAdaptive(true, false)
		dst := image.NewPaletted(src.Bounds(), ap.pal)
		ap.Palettize(dst, src)
	}
}

// TestPalettizeLeavesNoGoroutines checks that palettizing a frame large
// enough to split across goroutines leaves none running, which a
// [synctest] bubble fails on.
func TestPalettizeLeavesNoGoroutines(t *testing.T) {
	t.Parallel()
	src := testutil.TerminalFrame(0)
	if palettizeSpanCount(src.Bounds()) < 2 {
		t.Skip("one CPU: frames are not split")
	}
	synctest.Test(t, func(*testing.T) {
		ap := NewAdaptive(true, false)
		ap.Palettize(image.NewPaletted(src.Bounds(), ap.pal), src)
	})
}
