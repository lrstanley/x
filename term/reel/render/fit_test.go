// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"testing"
)

// fitOptions cover everything that adds to the image around the cells.
var fitOptions = []Option{
	WithMargin(3, color.Black),
	WithPadding(5),
	WithWindowChrome(true),
	WithScrollbar(true),
}

// imageAt returns the size of the image of a cols by rows screen drawn at
// scale with fitOptions.
func imageAt(t *testing.T, scale float64, cols, rows int) image.Point {
	t.Helper()
	d := MustNew(append([]Option{WithScale(scale)}, fitOptions...)...)
	defer d.Close()
	return d.Layout(cols, rows).Image.Size()
}

func TestScaleForFontSize(t *testing.T) {
	t.Parallel()

	for _, px := range []float64{8, 14.666666666666666, 22, 40} {
		d := MustNew(WithScale(ScaleForFontSize(px)))
		if got := d.opts.FontSizePx(); got < px-1e-9 || got > px+1e-9 {
			t.Errorf("FontSizePx at ScaleForFontSize(%v) = %v", px, got)
		}
		d.Close()
	}
	if got := ScaleForFontSize(0); got != 0 {
		t.Errorf("ScaleForFontSize(0) = %v, want 0", got)
	}
}

func TestRendererFit(t *testing.T) {
	t.Parallel()

	d := MustNew(fitOptions...)
	t.Cleanup(func() { d.Close() })

	t.Run("fits and cannot grow", func(t *testing.T) {
		t.Parallel()

		for _, size := range [][2]int{{200, 100}, {641, 479}, {1920, 1080}} {
			cols, rows := d.Fit(size[0], size[1])
			if cols == 0 || rows == 0 {
				continue
			}
			img := d.Layout(cols, rows).Image
			if img.Dx() > size[0] || img.Dy() > size[1] {
				t.Errorf("Fit(%d, %d) = %dx%d, which is %v", size[0], size[1], cols, rows, img.Size())
			}
			if more := d.Layout(cols+1, rows).Image; more.Dx() <= size[0] {
				t.Errorf("Fit(%d, %d) cols = %d, but %d fit", size[0], size[1], cols, cols+1)
			}
			if more := d.Layout(cols, rows+1).Image; more.Dy() <= size[1] {
				t.Errorf("Fit(%d, %d) rows = %d, but %d fit", size[0], size[1], rows, rows+1)
			}
		}
	})

	t.Run("none without room for the surroundings", func(t *testing.T) {
		t.Parallel()

		around := d.Layout(0, 0).Image.Size()
		cols, rows := d.Fit(around.X-1, around.Y-1)
		if cols != 0 || rows != 0 {
			t.Errorf("Fit below the surroundings = %dx%d, want 0x0", cols, rows)
		}
	})

	t.Run("monotonic", func(t *testing.T) {
		t.Parallel()

		var prevCols, prevRows int
		for px := range 601 {
			cols, rows := d.Fit(px, px)
			if cols < prevCols || rows < prevRows {
				t.Fatalf("Fit(%d, %[1]d) = %dx%d, was %dx%d", px, cols, rows, prevCols, prevRows)
			}
			prevCols, prevRows = cols, rows
		}
	})
}

func TestFitScale(t *testing.T) {
	t.Parallel()

	const cols, rows = 80, 24

	t.Run("monotonic and round trips", func(t *testing.T) {
		t.Parallel()

		var prev float64
		for width := 700; width <= 2600; width += 150 {
			height := width * 9 / 16
			scale, err := FitScale(cols, rows, width, height, fitOptions...)
			if err != nil {
				t.Fatalf("FitScale(%d, %d) error = %v", width, height, err)
			}
			if scale < prev {
				t.Fatalf("FitScale(%d, %d) = %v, was %v at a smaller size", width, height, scale, prev)
			}
			prev = scale

			size := imageAt(t, scale, cols, rows)
			if size.X > width || size.Y > height {
				t.Errorf("FitScale(%d, %d) = %v draws %v", width, height, scale, size)
			}
			// A little more scale would no longer fit.
			if more := imageAt(t, scale*1.05, cols, rows); more.X <= width && more.Y <= height {
				t.Errorf("FitScale(%d, %d) = %v, but 5%% more draws %v", width, height, scale, more)
			}
		}
	})

	t.Run("fits the grid Fit would", func(t *testing.T) {
		t.Parallel()

		scale, err := FitScale(cols, rows, 1280, 720, fitOptions...)
		if err != nil {
			t.Fatal(err)
		}
		d := MustNew(append([]Option{WithScale(scale)}, fitOptions...)...)
		defer d.Close()
		if gotCols, gotRows := d.Fit(1280, 720); gotCols < cols || gotRows < rows {
			t.Errorf("Fit(1280, 720) at scale %v = %dx%d, want at least %dx%d", scale, gotCols, gotRows, cols, rows)
		}
	})

	t.Run("one direction", func(t *testing.T) {
		t.Parallel()

		both, err := FitScale(cols, rows, 1280, 100000, fitOptions...)
		if err != nil {
			t.Fatal(err)
		}
		width, err := FitScale(cols, rows, 1280, 0, fitOptions...)
		if err != nil {
			t.Fatal(err)
		}
		if width != both {
			t.Errorf("FitScale(1280, 0) = %v, want %v of an effectively unlimited height", width, both)
		}
		tall, err := FitScale(cols, rows, 0, 720, fitOptions...)
		if err != nil {
			t.Fatal(err)
		}
		if h := imageAt(t, tall, cols, rows).Y; h > 720 {
			t.Errorf("FitScale(0, 720) = %v draws %d tall", tall, h)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			cols, rows int
			w, h       int
			opts       []Option
		}{
			{"no cols", 0, 24, 800, 600, nil},
			{"no rows", 80, 0, 800, 600, nil},
			{"no size", 80, 24, 0, 0, nil},
			{"negative", 80, 24, -1, 600, nil},
			{"too small", 80, 24, 20, 20, nil},
			{"surroundings alone do not fit", 80, 24, 10, 10, fitOptions},
			{"larger than any font size", 80, 24, 1 << 30, 0, nil},
			{"scale set", 80, 24, 800, 600, []Option{WithScale(2)}},
			{"bad option", 80, 24, 800, 600, []Option{WithPadding(-1)}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				if scale, err := FitScale(tt.cols, tt.rows, tt.w, tt.h, tt.opts...); err == nil {
					t.Errorf("FitScale = %v, want an error", scale)
				}
			})
		}
	})
}
