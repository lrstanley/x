// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"
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
		_ = d.Draw(scr)
	})
	assertPanic(t, "DrawInto after close", func() {
		d.DrawInto(image.NewNRGBA(image.Rect(0, 0, 32, 32)), image.Rect(0, 0, 32, 32), scr)
	})
	assertPanic(t, "UpdateEmulatorState after close", func() {
		d.UpdateEmulatorState(&EmulatorState{Focused: true})
	})
}

func TestDrawInto(t *testing.T) {
	t.Parallel()

	margin := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(2, 1)
	d := MustNew(WithMargin(1, margin))
	size := d.Size(scr)

	t.Run("rejects area smaller than render size", func(t *testing.T) {
		t.Parallel()

		assertPanic(t, "too small area", func() {
			dst := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
			d.DrawInto(dst, image.Rect(0, 0, size.X-1, size.Y), scr)
		})
	})

	t.Run("draws into larger area without filling unused pixels", func(t *testing.T) {
		t.Parallel()

		dst := image.NewNRGBA(image.Rect(0, 0, size.X+8, size.Y+8))
		area := image.Rect(2, 3, 2+size.X+4, 3+size.Y+4)
		d.DrawInto(dst, area, scr)

		if got := dst.NRGBAAt(2, 3); got != margin {
			t.Fatalf("margin pixel = %#v, want %#v", got, margin)
		}
		if got := dst.NRGBAAt(area.Max.X-1, area.Max.Y-1); got != (color.NRGBA{}) {
			t.Fatalf("unused larger area pixel = %#v, want transparent zero", got)
		}
	})

	t.Run("empty area uses screen bounds", func(t *testing.T) {
		t.Parallel()

		assertPanic(t, "too small destination", func() {
			dst := image.NewNRGBA(image.Rect(0, 0, size.X-1, size.Y))
			d.DrawInto(dst, image.Rectangle{}, scr)
		})

		dst := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
		d.DrawInto(dst, image.Rectangle{}, scr)

		if got := dst.NRGBAAt(0, 0); got != margin {
			t.Fatalf("margin pixel = %#v, want %#v", got, margin)
		}
	})
}

func TestDrawReusesFrame(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(3, 2)
	d := MustNew()

	img, ok := d.Draw(scr).(*image.NRGBA)
	if !ok {
		t.Fatalf("Draw() image type = %T, want *image.NRGBA", img)
	}
	if got, want := img.Bounds().Size(), d.Size(scr); got != want {
		t.Fatalf("Draw() bounds size = %v, want %v", got, want)
	}

	again := d.Draw(scr)
	if again != img {
		t.Fatal("Draw() did not reuse frame buffer")
	}

	larger := newTestScreen(5, 4)
	resized := d.Draw(larger)
	if resized == img {
		t.Fatal("Draw() reused frame after bounds changed")
	}
	if got, want := resized.Bounds().Size(), d.Size(larger); got != want {
		t.Fatalf("resized Draw() bounds size = %v, want %v", got, want)
	}
}

func TestNewJoinsOptionErrors(t *testing.T) {
	t.Parallel()

	d, err := New(
		WithFontSizePt(0),
		WithBackgroundOpacity(2),
		WithCursorBlinkSpeed(0),
	)
	if err == nil {
		t.Fatal("New() error = nil, want joined validation errors")
	}
	if d != nil {
		t.Fatal("New() renderer = non-nil, want nil on option failure")
	}
	if !errors.Is(err, err) {
		t.Fatalf("New() error = %v", err)
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) || len(joined.Unwrap()) < 3 {
		t.Fatalf("New() error = %v, want errors.Join with multiple failures", err)
	}
}

func TestUpdateEmulatorStateCopiesValue(t *testing.T) {
	t.Parallel()

	first := EmulatorState{Title: "first", CursorVisible: true, CursorX: 1}
	second := EmulatorState{Title: "second", CursorVisible: false, CursorX: 2}
	d := MustNew()
	d.UpdateEmulatorState(&first)

	got := d.GetEmulatorState()
	if got == nil || got.Title != first.Title || got.CursorX != first.CursorX || got.CursorVisible != first.CursorVisible {
		t.Fatalf("GetEmulatorState() = %#v, want %#v", got, first)
	}

	first.Title = "mutated"
	if got := d.GetEmulatorState(); got.Title != "first" {
		t.Fatalf("stored state mutated with caller value: %#v", got)
	}

	d.UpdateEmulatorState(&second)
	got = d.GetEmulatorState()
	if got.Title != second.Title || got.CursorX != second.CursorX || got.CursorVisible != second.CursorVisible {
		t.Fatalf("GetEmulatorState() = %#v, want %#v", got, second)
	}

	d.UpdateEmulatorState(nil)
	if got := d.GetEmulatorState(); got != nil {
		t.Fatalf("GetEmulatorState() = %#v, want nil after clear", got)
	}
}

func TestRendererConcurrentMethodsSerialize(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(4, 2)
	d := MustNew(WithScrollbar(true))
	size := d.Size(scr)

	var wg sync.WaitGroup
	for i := range 25 {
		wg.Add(5)
		go func() {
			defer wg.Done()
			_ = d.Draw(scr)
		}()
		go func(i int) {
			defer wg.Done()
			d.UpdateEmulatorState(&EmulatorState{
				CursorVisible:   i%2 == 0,
				CursorX:         i % 4,
				CursorY:         i % 2,
				ScrollbackCount: i,
			})
		}(i)
		go func() {
			defer wg.Done()
			_ = d.GetEmulatorState()
		}()
		go func() {
			defer wg.Done()
			_ = d.Metrics()
		}()
		go func() {
			defer wg.Done()
			dst := image.NewNRGBA(image.Rectangle{Max: size})
			d.DrawInto(dst, dst.Bounds(), scr)
		}()
	}
	wg.Wait()
}

func TestRendererPaletteFrozenAtNew(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{B: 0xff, A: 0xff}
	red := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(1, 1)

	d := MustNew(WithPalette(Palette{Indexed: map[int]color.Color{1: blue}}))
	_ = d.Draw(scr)

	if got := d.palette.Indexed[1]; got != blue {
		t.Fatalf("renderer palette = %#v, want original %#v", got, blue)
	}
	d.palette.Indexed[1] = red
	if got := d.opts.Palette.Indexed[1]; got != blue {
		t.Fatalf("opts palette = %#v, want original %#v", got, blue)
	}
}
