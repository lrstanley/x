// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render/fonts"
	"github.com/lrstanley/x/term/reel/vt"
)

func TestRendererCloseSemantics(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(2, 1)
	d := MustNew()

	if err := d.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	assertPanic(t, "Draw after close", func() {
		d.Draw(image.NewNRGBA(image.Rect(0, 0, 32, 32)), scr, focused, 0)
	})
	assertPanic(t, "Repaint after close", func() {
		d.Repaint(image.NewNRGBA(image.Rect(0, 0, 32, 32)), scr, focused, 0)
	})
}

func TestDraw(t *testing.T) {
	t.Parallel()

	margin := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(2, 1)
	d := MustNew(WithMargin(1, margin))
	size := d.Layout(2, 1).Image.Size()

	t.Run("rejects destination smaller than the image", func(t *testing.T) {
		t.Parallel()

		assertPanic(t, "too narrow destination", func() {
			d.Draw(image.NewNRGBA(image.Rect(0, 0, size.X-1, size.Y)), scr, focused, 0)
		})
		assertPanic(t, "too short destination", func() {
			d.Draw(image.NewNRGBA(image.Rect(5, 5, 5+size.X, 5+size.Y-1)), scr, focused, 0)
		})
	})

	t.Run("draws at the destination origin without filling unused pixels", func(t *testing.T) {
		t.Parallel()

		dst := image.NewNRGBA(image.Rect(2, 3, 2+size.X+4, 3+size.Y+4))
		d.Draw(dst, scr, focused, 0)

		if got := dst.NRGBAAt(2, 3); got != margin {
			t.Fatalf("margin pixel = %#v, want %#v", got, margin)
		}
		if got := dst.NRGBAAt(2+size.X, 3); got != (color.NRGBA{}) {
			t.Fatalf("pixel right of the image = %#v, want transparent zero", got)
		}
		if got := dst.NRGBAAt(dst.Rect.Max.X-1, dst.Rect.Max.Y-1); got != (color.NRGBA{}) {
			t.Fatalf("unused pixel = %#v, want transparent zero", got)
		}
		want := drawFresh(d, scr, focused, 0)
		for y := range size.Y {
			for x := range size.X {
				if got, want := dst.NRGBAAt(2+x, 3+y), want.NRGBAAt(x, y); got != want {
					t.Fatalf("pixel %d,%d = %#v, want %#v", x, y, got, want)
				}
			}
		}
	})
}

func TestLayout(t *testing.T) {
	t.Parallel()

	d := MustNew(WithMargin(3, nil), WithPadding(2), WithScrollbar(true), WithScale(scaleFor(14)))
	l := d.Layout(4, 2)
	cell := d.metrics.CellSize()
	if l.Cell != cell || l.Scale != d.metrics.Scale {
		t.Fatalf("Layout cell %v scale %v, want %v %v", l.Cell, l.Scale, cell, d.metrics.Scale)
	}
	if want := image.Rect(5, 5, 5+4*cell.X, 5+2*cell.Y); l.Grid != want {
		t.Fatalf("Layout grid = %v, want %v", l.Grid, want)
	}
	if want := image.Rect(0, 0, l.Grid.Max.X+scrollbarWidthPx+5, l.Grid.Max.Y+5); l.Image != want {
		t.Fatalf("Layout image = %v, want %v", l.Image, want)
	}
	want := image.Rectangle{Min: l.Grid.Min.Add(image.Pt(3*cell.X, cell.Y)), Max: l.Grid.Max}
	if got := l.CellRect(3, 1); got != want {
		t.Fatalf("CellRect(3, 1) = %v, want %v", got, want)
	}
	if got := l.CellRect(4, 0); !got.Empty() {
		t.Fatalf("CellRect(4, 0) = %v, want empty", got)
	}

	scr := newTestScreen(4, 2)
	f := d.layoutSnapshot(t, image.Point{}, scr)
	if f.imageBounds != l.Image || f.gridBounds != l.Grid || f.cellBounds(2, 1) != l.CellRect(2, 1) {
		t.Fatalf("frame layout %v %v differs from Layout %v %v", f.imageBounds, f.gridBounds, l.Image, l.Grid)
	}
}

// TestDrawOverwritesDestination checks that a draw the frame cache answers
// still writes every pixel of dst, which may have been drawn over since.
func TestDrawOverwritesDestination(t *testing.T) {
	t.Parallel()

	buf := vt.NewBuffer(4, 2)
	buf.SetCell(1, 0, &vt.Cell{Content: "x", Width: 1})
	d := MustNew()
	dst := newImage(d, buf)
	d.Draw(dst, buf, focused, 0)
	want := bytes.Clone(dst.Pix)

	for i := range dst.Pix {
		dst.Pix[i] = 0xAA
	}
	buf.ClearDirty()
	d.Draw(dst, buf, focused, 0)
	if !bytes.Equal(dst.Pix, want) {
		t.Fatal("a cached draw left pixels of dst as they were")
	}
}

// TestFrameSnapTracksState checks that every [vt.State] field either
// changes the frame cache key or is listed as one the key leaves out, so a
// field added to State cannot be missed.
func TestFrameSnapTracksState(t *testing.T) {
	t.Parallel()

	// The cells are tracked by the buffer generation, and the records by the
	// indicators they draw, not by these.
	ignored := map[string]bool{"Generation": true, "ProgramStatusGeneration": true}
	red := color.NRGBA{R: 0xff, A: 0xff}
	changes := map[string]func(*vt.State){
		"Title":                 func(s *vt.State) { s.Title = "x" },
		"Focused":               func(s *vt.State) { s.Focused = true },
		"AltScreen":             func(s *vt.State) { s.AltScreen = true },
		"Cursor":                func(s *vt.State) { s.Cursor.X = 1 },
		"CursorColor":           func(s *vt.State) { s.CursorColor, s.CursorColorSet = red, true },
		"CursorColorSet":        func(s *vt.State) { s.CursorColorSet = true },
		"Foreground":            func(s *vt.State) { s.Foreground = red },
		"Background":            func(s *vt.State) { s.Background = red },
		"ScrollbackLen":         func(s *vt.State) { s.ScrollbackLen = 9 },
		"Progress":              func(s *vt.State) { s.Progress.Percent = 6 },
		"ProgramStatuses":       func(s *vt.State) { s.ProgramStatuses = []vt.ProgramStatus{{Progress: -1}} },
		"ProgramStatusReported": func(s *vt.State) { s.ProgramStatusReported = true },
	}
	d := MustNew(WithWindowChrome(true))
	bounds := image.Rect(0, 0, 4, 2)
	// A progress report, so a report that hides it shows.
	base := vt.State{Progress: vt.Progress{State: vt.ProgressSet, Percent: 5}}
	typ := reflect.TypeFor[vt.State]()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		change, ok := changes[name]
		if !ok {
			if !ignored[name] {
				t.Errorf("State.%s is neither tracked by frameSnap nor listed as ignored", name)
			}
			continue
		}
		to := base
		change(&to)
		if d.currentSnap(bounds, base, 0) == d.currentSnap(bounds, to, 0) {
			t.Errorf("frameSnap ignores State.%s", name)
		}
	}
}

func TestDrawReusesFrame(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(3, 2)
	d := MustNew()

	img := drawNRGBA(t, d, scr)
	if got, want := img.Bounds(), d.Layout(3, 2).Image; got != want {
		t.Fatalf("Draw() bounds = %v, want %v", got, want)
	}
	frame := d.frame

	drawNRGBA(t, d, scr)
	if d.frame != frame {
		t.Fatal("Draw() did not reuse frame buffer")
	}

	larger := newTestScreen(5, 4)
	resized := drawNRGBA(t, d, larger)
	if d.frame == frame {
		t.Fatal("Draw() reused frame after bounds changed")
	}
	if got, want := resized.Bounds(), d.Layout(5, 4).Image; got != want {
		t.Fatalf("resized Draw() bounds = %v, want %v", got, want)
	}
}

func TestNewJoinsOptionErrors(t *testing.T) {
	t.Parallel()

	d, err := New(
		WithScale(0),
		WithBackgroundOpacity(2),
		WithCursorBlinkSpeed(0),
	)
	if err == nil {
		t.Fatal("New() error = nil, want joined validation errors")
	}
	if d != nil {
		t.Fatal("New() renderer = non-nil, want nil on option failure")
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) || len(joined.Unwrap()) < 3 {
		t.Fatalf("New() error = %v, want errors.Join with multiple failures", err)
	}
}

func TestRendererConcurrentMethodsSerialize(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(4, 2)
	d := MustNew(WithScrollbar(true))

	var wg sync.WaitGroup
	for i := range 25 {
		st := vt.State{
			Cursor:        vt.Cursor{Visible: i%2 == 0, X: i % 4, Y: i % 2},
			ScrollbackLen: i,
			Progress:      vt.Progress{State: vt.ProgressIndeterminate, Percent: -1},
		}
		at := time.Duration(i) * 10 * time.Millisecond
		wg.Go(func() {
			dst := newImage(d, scr)
			d.Draw(dst, scr, st, at)
			d.Repaint(dst, scr, st, at+time.Millisecond)
		})
		wg.Go(func() { _ = d.Animates(st) })
		wg.Go(func() { _ = d.Changes(st, at, at+time.Second) })
		wg.Go(func() { _ = d.Metrics() })
		wg.Go(func() { _ = d.Layout(4, 2) })
	}
	wg.Wait()
}

func TestRendererPaletteFrozenAtNew(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{B: 0xff, A: 0xff}
	red := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(1, 1)

	d := MustNew(WithPalette(Palette{Indexed: map[int]color.Color{1: blue}}))
	drawNRGBA(t, d, scr)

	if got := d.palette.Indexed[1]; got != blue {
		t.Fatalf("renderer palette = %#v, want original %#v", got, blue)
	}
	d.palette.Indexed[1] = red
	if got := d.opts.Palette.Indexed[1]; got != blue {
		t.Fatalf("opts palette = %#v, want original %#v", got, blue)
	}
}

func TestPaletteOf(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{B: 0xff, A: 0xff}
	red := color.NRGBA{R: 0xff, A: 0xff}
	if p, err := PaletteOf(WithScale(2)); err != nil || p.Indexed != nil {
		t.Fatalf("PaletteOf without a palette = %+v, %v", p, err)
	}
	p, err := PaletteOf(
		WithPalette(Palette{Indexed: map[int]color.Color{1: blue}}),
		WithPalette(Palette{Indexed: map[int]color.Color{2: red}}),
	)
	if err != nil || p.Indexed[1] != nil || p.Indexed[2] != red {
		t.Fatalf("PaletteOf = %+v, %v; want the last palette", p, err)
	}
	if _, err = PaletteOf(WithScale(-1)); err == nil {
		t.Fatal("PaletteOf with an invalid option succeeded")
	}
}

func TestWithFontFamilyName(t *testing.T) {
	t.Parallel()

	if _, err := New(WithFontFamilyName("JetBrains Mono")); err != nil {
		t.Fatalf("embedded family by name: %v", err)
	}

	_, err := New(WithFontFamilyName("Definitely Not A Font"))
	if !errors.Is(err, fonts.ErrFamilyNotFound) {
		t.Fatalf("err = %v, want fonts.ErrFamilyNotFound", err)
	}
}
