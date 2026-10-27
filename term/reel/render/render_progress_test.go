// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
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
			img := drawAt(t, d, scr, vt.State{Focused: true, Progress: vt.Progress{State: vt.ProgressSet, Percent: pct}}, 0)

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

// sweepPhase is the offset at which the indeterminate sweep is u of the way
// through.
func sweepPhase(u float64) time.Duration {
	return time.Duration(u * float64(progressSweep))
}

// TestProgressStatesDiffer checks that each state draws, and differently,
// and that remove and the zero value draw nothing.
func TestProgressStatesDiffer(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 3)
	draw := func(p vt.Progress) []byte {
		d := newRenderer(t, WithWindowChrome(true))
		return drawAt(t, d, scr, vt.State{Title: "t", Progress: p}, sweepPhase(0.5)).Pix
	}

	none := draw(vt.Progress{})
	if !bytes.Equal(none, draw(vt.Progress{State: vt.ProgressRemove, Percent: 50})) {
		t.Error("ProgressRemove drew something")
	}
	seen := map[string]string{}
	for name, p := range map[string]vt.Progress{
		"set":               {State: vt.ProgressSet, Percent: 60},
		"error":             {State: vt.ProgressError, Percent: 60},
		"error, no percent": {State: vt.ProgressError, Percent: -1},
		"pause":             {State: vt.ProgressPause, Percent: 60},
		"pause, no percent": {State: vt.ProgressPause, Percent: -1},
		"indeterminate":     {State: vt.ProgressIndeterminate, Percent: -1},
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
	if !bytes.Equal(draw(vt.Progress{State: vt.ProgressIndeterminate, Percent: 60}), draw(vt.Progress{State: vt.ProgressIndeterminate, Percent: -1})) {
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
		before := drawAt(t, d, scr, vt.State{}, 0).Pix

		set := vt.State{Progress: vt.Progress{Reported: true, State: vt.ProgressSet, Percent: 40}}
		if bytes.Equal(before, drawAt(t, d, scr, set, 0).Pix) {
			t.Fatalf("chrome=%v: ProgressSet did not change the next image", chrome)
		}
		removed := vt.State{Progress: vt.Progress{Reported: true, State: vt.ProgressRemove}}
		if !bytes.Equal(before, drawAt(t, d, scr, removed, 0).Pix) {
			t.Fatalf("chrome=%v: ProgressRemove did not clear the bar", chrome)
		}
		set.ProgramStatusReported = true
		if !bytes.Equal(before, drawAt(t, d, scr, set, 0).Pix) {
			t.Fatalf("chrome=%v: progress drew after an OSC 7501 report", chrome)
		}
	}
}

// TestProgressIndeterminateAnimates checks that the sweep moves with the
// offset, and that the frame cache does not freeze it.
func TestProgressIndeterminateAnimates(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 3)
	d := newRenderer(t)
	st := vt.State{Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}
	if !d.Animates(st) {
		t.Fatal("Animates() = false for indeterminate progress")
	}
	start := sweepPhase(0.25)
	first := drawAt(t, d, scr, st, start).Pix

	second := drawAt(t, d, scr, st, start+progressSweep/4).Pix
	if bytes.Equal(first, second) {
		t.Fatal("indeterminate progress did not move with the offset")
	}

	if !bytes.Equal(first, drawAt(t, d, scr, st, start+progressSweep).Pix) {
		t.Fatal("indeterminate progress is not periodic over one sweep")
	}
	if !bytes.Equal(first, drawAt(t, d, scr, st, start-progressSweep).Pix) {
		t.Fatal("indeterminate progress is not periodic before offset 0")
	}
}

// TestProgressRepaintsOnlyStrip checks that a progress change on an unchanged
// screen repaints the bar's strip and nothing else.
func TestProgressRepaintsOnlyStrip(t *testing.T) {
	t.Parallel()

	scr := vt.NewBuffer(20, 4)
	d := newRenderer(t, WithWindowChrome(true))
	drawAt(t, d, scr, vt.State{Focused: true, Progress: vt.Progress{State: vt.ProgressSet, Percent: 30}}, 0)

	// Poison in the renderer's own frame shows whether Draw painted it again.
	poison := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	grid := d.layoutSnapshot(t, image.Point{}, scr).gridBounds
	d.frame.SetNRGBA(grid.Min.X, grid.Min.Y, poison)

	got := drawAt(t, d, scr, vt.State{Focused: true, Progress: vt.Progress{State: vt.ProgressSet, Percent: 60}}, 0)
	if got.NRGBAAt(grid.Min.X, grid.Min.Y) != poison {
		t.Fatal("a progress change repainted the grid")
	}
	bar := d.layoutSnapshot(t, image.Point{}, scr).progressBounds
	if x := bar.Min.X + bar.Dx()/2; got.NRGBAAt(x, bar.Min.Y) != defaultProgress {
		t.Fatal("a progress change did not repaint the bar")
	}
}

// TestRepaintProgressMatchesFullDraw checks that repainting the bar's rows of
// a copy of an earlier image gives exactly the pixels of drawing everything
// again at the later offset, and that the rest of the image is untouched.
func TestRepaintProgressMatchesFullDraw(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string][]Option{
		"plain":      nil,
		"chrome":     {WithWindowChrome(true)},
		"no chrome":  {WithPadding(4), WithMargin(5, color.NRGBA{R: 40, G: 60, B: 90, A: 255}), WithBorderRadius(12)},
		"everything": {WithWindowChrome(true), WithPadding(3), WithMargin(6, color.NRGBA{R: 30, A: 200}), WithBorderRadius(14), WithBackgroundOpacity(0.5), WithProgressBarHeight(3)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scr := newTestScreen(24, 4)
			for x, r := range "progress repaint test" {
				scr.SetCell(x, 0, &vt.Cell{Content: string(r), Width: 1})
			}
			d := newRenderer(t, opts...)
			st := vt.State{Title: "t", Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}

			at := sweepPhase(0.2)
			base := drawAt(t, d, scr, st, at)
			later := at + progressSweep/4
			want := drawFresh(d, scr, st, later)

			got := image.NewNRGBA(base.Rect)
			copy(got.Pix, base.Pix)
			d.Repaint(got, scr, st, later)
			if bytes.Equal(got.Pix, base.Pix) {
				t.Fatal("repainting at a later offset did not move the bar")
			}
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatal("repainted image differs from a full draw at the same offset")
			}
		})
	}
}

// TestRepaintAtOrigin checks that Repaint lays the indicators out at
// dst.Rect.Min, where Draw drew the image.
func TestRepaintAtOrigin(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(12, 3)
	d := newRenderer(t, WithWindowChrome(true), WithMargin(4, nil))
	st := vt.State{Title: "t", Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}
	size := d.Layout(12, 3).Image.Size()
	dst := image.NewNRGBA(image.Rectangle{Min: image.Pt(7, 5), Max: image.Pt(7+size.X, 5+size.Y)})
	d.Draw(dst, scr, st, sweepPhase(0.2))
	later := sweepPhase(0.5)
	d.Repaint(dst, scr, st, later)

	want := drawFresh(d, scr, st, later)
	for y := range size.Y {
		for x := range size.X {
			if got, w := dst.NRGBAAt(7+x, 5+y), want.NRGBAAt(x, y); got != w {
				t.Fatalf("pixel %d,%d = %v, want %v as a full draw at the origin", x, y, got, w)
			}
		}
	}
}

func TestProgressSweepRow(t *testing.T) {
	t.Parallel()

	const w = 200
	track := uint8(255*progressTrack + 0.5)
	row := make([]uint8, w)
	for i := range 20 {
		at := progressSweep * time.Duration(i) / 20
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
	progressSweepRow(row, 0, 2)
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
	paused := vt.State{Progress: vt.Progress{State: vt.ProgressPause, Percent: -1}}
	if !bytes.Equal(drawAt(t, a, scr, vt.State{}, 0).Pix, drawAt(t, b, scr, paused, 0).Pix) {
		t.Fatal("WithProgressBar(false) still drew the bar")
	}
	indeterminate := vt.State{Progress: vt.Progress{State: vt.ProgressIndeterminate, Percent: -1}}
	if b.Animates(indeterminate) {
		t.Error("Animates() = true with the bar disabled")
	}
	if _, err := New(WithProgressBarHeight(0)); err == nil {
		t.Fatal("WithProgressBarHeight(0) did not fail")
	}
}
