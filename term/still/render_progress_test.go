// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lrstanley/x/term/vt/screen"
)

func TestProgressBounds(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 4)
	for name, chrome := range map[string]bool{"chrome": true, "no chrome": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := newRenderer(t, WithWindowChrome(chrome), WithPadding(3), WithMargin(5, nil), WithProgressBarHeight(3))
			f := d.layoutSnapshot(t, image.Point{}, scr)
			bar := f.progressBounds
			if bar.Dx() != f.windowBounds.Dx() || bar.Dy() != 3 {
				t.Fatalf("progress bounds = %v, want the window width by 3px", bar)
			}
			if chrome {
				if bar.Max.Y != f.chromeBounds.Max.Y || bar.Max.Y > f.gridBounds.Min.Y {
					t.Errorf("progress bounds = %v, want the bottom of title bar %v, above grid %v", bar, f.chromeBounds, f.gridBounds)
				}
			} else if bar.Min != f.windowBounds.Min {
				t.Errorf("progress bounds = %v, want the top of window %v", bar, f.windowBounds)
			}
		})
	}
}

// TestProgressSetShowsPercent checks that a determinate bar covers exactly
// the reported share of the window, from the left, in the progress color.
func TestProgressSetShowsPercent(t *testing.T) {
	t.Parallel()

	want := color.NRGBA{R: 10, G: 200, B: 30, A: 255}
	for _, chrome := range []bool{false, true} {
		for _, pct := range []int{0, 25, 50, 100} {
			scr := newTestScreen(20, 3)
			d := newRenderer(t, WithWindowChrome(chrome), WithPalette(Palette{Progress: want}))
			d.UpdateEmulatorState(&EmulatorState{Focused: true, Progress: Progress{State: ProgressSet, Percent: pct}})
			img := drawNRGBA(t, d, scr)

			bar := d.layoutSnapshot(t, image.Point{}, scr).progressBounds
			end := bar.Min.X + bar.Dx()*pct/100
			for y := bar.Min.Y; y < bar.Max.Y; y++ {
				for x := bar.Min.X; x < bar.Max.X; x++ {
					if got := img.NRGBAAt(x, y); (got == want) != (x < end) {
						t.Fatalf("chrome=%v %d%%: pixel (%d,%d) = %v, want the bar to end at x=%d", chrome, pct, x, y, got, end)
					}
				}
			}
			if below := img.NRGBAAt(bar.Min.X, bar.Max.Y); below == want {
				t.Errorf("chrome=%v %d%%: bar is taller than %dpx", chrome, pct, bar.Dy())
			}
		}
	}
}

// sleepToPhase sleeps, inside a synctest bubble, until the indeterminate
// sweep is u of the way through.
func sleepToPhase(u float64) {
	now := time.Duration(time.Now().UnixNano() % int64(progressSweep))
	synctest.Sleep((time.Duration(u*float64(progressSweep)) - now + progressSweep) % progressSweep)
}

// TestProgressStatesDiffer checks that each state draws, and differently,
// and that remove and the zero value draw nothing.
func TestProgressStatesDiffer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, testProgressStatesDiffer)
}

func testProgressStatesDiffer(t *testing.T) {
	scr := newTestScreen(20, 3)
	sleepToPhase(0.5)
	draw := func(p Progress) []byte {
		d := newRenderer(t, WithWindowChrome(true))
		d.UpdateEmulatorState(&EmulatorState{Title: "t", Progress: p})
		return bytes.Clone(drawNRGBA(t, d, scr).Pix)
	}

	none := draw(Progress{})
	if !bytes.Equal(none, draw(Progress{State: ProgressRemove, Percent: 50})) {
		t.Error("ProgressRemove drew something")
	}
	seen := map[string]string{}
	for name, p := range map[string]Progress{
		"set":               {State: ProgressSet, Percent: 60},
		"error":             {State: ProgressError, Percent: 60},
		"error, no percent": {State: ProgressError, Percent: -1},
		"pause":             {State: ProgressPause, Percent: 60},
		"pause, no percent": {State: ProgressPause, Percent: -1},
		"indeterminate":     {State: ProgressIndeterminate, Percent: -1},
	} {
		img := draw(p)
		if bytes.Equal(img, none) {
			t.Errorf("%s drew nothing", name)
		}
		if prev, ok := seen[string(img)]; ok {
			t.Errorf("%s draws the same as %s", name, prev)
		}
		seen[string(img)] = name
	}
	if !bytes.Equal(draw(Progress{State: ProgressIndeterminate, Percent: 60}), draw(Progress{State: ProgressIndeterminate, Percent: -1})) {
		t.Error("indeterminate progress depends on its percent")
	}
}

// TestProgressRemoveClearsOnNextDraw checks the cached Draw path: a progress
// change on an unchanged screen repaints, and remove restores the old frame.
func TestProgressRemoveClearsOnNextDraw(t *testing.T) {
	t.Parallel()

	for _, chrome := range []bool{false, true} {
		scr := newTestScreen(20, 3)
		d := newRenderer(t, WithWindowChrome(chrome))
		d.UpdateEmulatorState(&EmulatorState{})
		before := bytes.Clone(drawNRGBA(t, d, scr).Pix)

		d.UpdateEmulatorState(&EmulatorState{Progress: Progress{State: ProgressSet, Percent: 40}})
		if bytes.Equal(before, drawNRGBA(t, d, scr).Pix) {
			t.Fatalf("chrome=%v: ProgressSet did not change the next image", chrome)
		}
		d.UpdateEmulatorState(&EmulatorState{Progress: Progress{State: ProgressRemove}})
		if !bytes.Equal(before, drawNRGBA(t, d, scr).Pix) {
			t.Fatalf("chrome=%v: ProgressRemove did not clear the bar", chrome)
		}
	}
}

// TestProgressIndeterminateAnimates checks that the sweep moves with the
// clock, and that the frame cache does not freeze it.
func TestProgressIndeterminateAnimates(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		scr := newTestScreen(20, 3)
		sleepToPhase(0.25)
		d := newRenderer(t)
		d.UpdateEmulatorState(&EmulatorState{Progress: Progress{State: ProgressIndeterminate, Percent: -1}})
		if !d.IndicatorsAnimate() {
			t.Fatal("IndicatorsAnimate() = false for indeterminate progress")
		}
		first := bytes.Clone(drawNRGBA(t, d, scr).Pix)

		synctest.Sleep(progressSweep / 4)
		second := bytes.Clone(drawNRGBA(t, d, scr).Pix)
		if bytes.Equal(first, second) {
			t.Fatal("indeterminate progress did not move with the clock")
		}

		synctest.Sleep(3 * progressSweep / 4) // a full sweep after the first draw
		if !bytes.Equal(first, drawNRGBA(t, d, scr).Pix) {
			t.Fatal("indeterminate progress is not periodic over one sweep")
		}
	})
}

// TestProgressRepaintsOnlyStrip checks that a progress change on an unchanged
// screen repaints the bar's strip and nothing else.
func TestProgressRepaintsOnlyStrip(t *testing.T) {
	t.Parallel()

	scr := screen.NewBuffer(20, 4)
	d := newRenderer(t, WithWindowChrome(true))
	d.UpdateEmulatorState(&EmulatorState{Focused: true, Progress: Progress{State: ProgressSet, Percent: 30}})
	img := drawNRGBA(t, d, scr)

	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	grid := d.layoutSnapshot(t, image.Point{}, scr).gridBounds
	img.SetNRGBA(grid.Min.X, grid.Min.Y, poison)

	d.UpdateEmulatorState(&EmulatorState{Focused: true, Progress: Progress{State: ProgressSet, Percent: 60}})
	got := drawNRGBA(t, d, scr)
	if got.NRGBAAt(grid.Min.X, grid.Min.Y) != poison {
		t.Fatal("a progress change repainted the grid")
	}
	bar := d.layoutSnapshot(t, image.Point{}, scr).progressBounds
	if x := bar.Min.X + bar.Dx()/2; got.NRGBAAt(x, bar.Min.Y) != defaultProgress {
		t.Fatal("a progress change did not repaint the bar")
	}
}

// TestRepaintIndicatorsProgressMatchesFullDraw checks that repainting the bar's rows of
// a copy of an earlier image gives exactly the pixels of drawing everything
// again at the later time, and that the rest of the image is untouched.
func TestRepaintIndicatorsProgressMatchesFullDraw(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string][]Option{
		"plain":      nil,
		"chrome":     {WithWindowChrome(true)},
		"no chrome":  {WithPadding(4), WithMargin(5, color.NRGBA{R: 40, G: 60, B: 90, A: 255}), WithBorderRadius(12)},
		"everything": {WithWindowChrome(true), WithPadding(3), WithMargin(6, color.NRGBA{R: 30, A: 200}), WithBorderRadius(14), WithBackgroundOpacity(0.5), WithProgressBarHeight(3)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				scr := newTestScreen(24, 4)
				for x, r := range "progress repaint test" {
					scr.SetCell(x, 0, &screen.Cell{Content: string(r), Width: 1})
				}
				d := newRenderer(t, opts...)
				d.UpdateEmulatorState(&EmulatorState{Title: "t", Progress: Progress{State: ProgressIndeterminate, Percent: -1}})

				sleepToPhase(0.2)
				base := drawIntoNRGBA(d, scr)
				synctest.Sleep(progressSweep / 4)
				want := drawIntoNRGBA(d, scr)

				got := image.NewNRGBA(base.Rect)
				copy(got.Pix, base.Pix)
				d.RepaintIndicators(got, scr)
				if bytes.Equal(got.Pix, base.Pix) {
					t.Fatal("repainting at a later time did not move the bar")
				}
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatal("repainted image differs from a full draw at the same time")
				}
			})
		})
	}
}

func TestProgressSweepRow(t *testing.T) {
	t.Parallel()

	const w = 200
	track := uint8(255*progressTrack + 0.5)
	row := make([]uint8, w)
	for i := range 20 {
		at := time.Unix(0, int64(progressSweep)*int64(i)/20)
		progressSweepRow(row, at, 2)
		peak := 0
		for x, a := range row {
			if a < track {
				t.Fatalf("t=%d/20: column %d coverage %d is below the track %d", i, x, a, track)
			}
			if a >= row[peak] {
				peak = x
			}
		}
		if i == 10 && (row[peak] != 255 || peak < w/2) {
			t.Errorf("mid-sweep peak %d at column %d, want a solid head right of center", row[peak], peak)
		}
	}
	// At the wrap the segment is off both edges, leaving only the track.
	progressSweepRow(row, time.Unix(0, 0), 2)
	for x, a := range row {
		if a != track {
			t.Fatalf("at the wrap, column %d coverage = %d, want the track %d", x, a, track)
		}
	}
}

func TestProgressBarDisabled(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(10, 2)
	a, b := newRenderer(t), newRenderer(t, WithProgressBar(false))
	a.UpdateEmulatorState(&EmulatorState{})
	b.UpdateEmulatorState(&EmulatorState{Progress: Progress{State: ProgressPause, Percent: -1}})
	if !bytes.Equal(drawNRGBA(t, a, scr).Pix, drawNRGBA(t, b, scr).Pix) {
		t.Fatal("WithProgressBar(false) still drew the bar")
	}
	if b.IndicatorsAnimate() {
		t.Error("IndicatorsAnimate() = true with the bar disabled")
	}
	if _, err := New(WithProgressBarHeight(0)); err == nil {
		t.Fatal("WithProgressBarHeight(0) did not fail")
	}
}
