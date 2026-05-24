// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"sync"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
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
	assertPanic(t, "Apply after close", func() {
		_ = d.Apply(WithScrollbar(true))
	})
}

func TestDrawIntoAreaValidation(t *testing.T) {
	t.Parallel()

	margin := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(2, 1)
	d := MustNew(WithMargin(1, margin))
	size := d.Size(scr)

	assertPanic(t, "too small area", func() {
		dst := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
		d.DrawInto(dst, image.Rect(0, 0, size.X-1, size.Y), scr)
	})

	dst := image.NewNRGBA(image.Rect(0, 0, size.X+8, size.Y+8))
	area := image.Rect(2, 3, 2+size.X+4, 3+size.Y+4)
	d.DrawInto(dst, area, scr)

	if got := dst.NRGBAAt(2, 3); got != margin {
		t.Fatalf("margin pixel = %#v, want %#v", got, margin)
	}
	if got := dst.NRGBAAt(area.Max.X-1, area.Max.Y-1); got != (color.NRGBA{}) {
		t.Fatalf("unused larger area pixel = %#v, want transparent zero", got)
	}
}

func TestDrawIntoEmptyAreaUsesScreenBounds(t *testing.T) {
	t.Parallel()

	margin := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(2, 1)
	d := MustNew(WithMargin(1, margin))
	size := d.Size(scr)

	assertPanic(t, "too small destination", func() {
		dst := image.NewNRGBA(image.Rect(0, 0, size.X-1, size.Y))
		d.DrawInto(dst, image.Rectangle{}, scr)
	})

	dst := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
	d.DrawInto(dst, image.Rectangle{}, scr)

	if got := dst.NRGBAAt(0, 0); got != margin {
		t.Fatalf("margin pixel = %#v, want %#v", got, margin)
	}
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

func TestEmulatorStateReplacementDoesNotRebuildMetrics(t *testing.T) {
	t.Parallel()

	first := EmulatorState{Title: "first", CursorVisible: true, CursorX: 1}
	second := EmulatorState{Title: "second", CursorVisible: false, CursorX: 2}
	d := MustNew(WithEmulatorState(first), WithEmulatorState(second))

	if got := d.EmulatorState(); got.Title != second.Title || got.CursorX != second.CursorX || got.CursorVisible != second.CursorVisible {
		t.Fatalf("EmulatorState() = %#v, want replacement %#v", got, second)
	}
	if !d.HasEmulatorState() {
		t.Fatal("HasEmulatorState() = false, want true")
	}

	metricsVersion := d.metricsVersion
	fontVersion := d.fontVersion
	if err := d.Apply(WithEmulatorState(first)); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if d.metricsVersion != metricsVersion {
		t.Fatalf("metricsVersion changed on state-only Apply: %d -> %d", metricsVersion, d.metricsVersion)
	}
	if d.fontVersion != fontVersion {
		t.Fatalf("fontVersion changed on state-only Apply: %d -> %d", fontVersion, d.fontVersion)
	}
	if d.dirty != dirtyNone {
		t.Fatalf("dirty = %08b, want dirtyNone", d.dirty)
	}
}

func TestPaletteOnlyApplyDoesNotRebuildFontsOrMetrics(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{B: 0xff, A: 0xff}
	d := MustNew()

	metricsVersion := d.metricsVersion
	fontVersion := d.fontVersion
	if err := d.Apply(WithPalette(Palette{DefaultBackground: blue})); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if d.metricsVersion != metricsVersion {
		t.Fatalf("metricsVersion changed on palette-only Apply: %d -> %d", metricsVersion, d.metricsVersion)
	}
	if d.fontVersion != fontVersion {
		t.Fatalf("fontVersion changed on palette-only Apply: %d -> %d", fontVersion, d.fontVersion)
	}
	if d.dirty != dirtyNone {
		t.Fatalf("dirty = %08b, want dirtyNone", d.dirty)
	}
}

func TestIndependentRenderersConcurrentDraw(t *testing.T) {
	t.Parallel()

	const workers = 32
	d := MustNew()
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			_ = d.Draw(newTestScreen(2, 1))
		}()
	}
	wg.Wait()
}

func TestRendererConcurrentMethodsSerialize(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(4, 2)
	d := MustNew(WithScrollbar(true))
	size := d.Size(scr)

	var wg sync.WaitGroup
	for i := range 25 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			_ = d.Draw(scr)
		}()
		go func(i int) {
			defer wg.Done()
			if err := d.Apply(WithEmulatorState(EmulatorState{
				CursorVisible:   i%2 == 0,
				CursorX:         i % 4,
				CursorY:         i % 2,
				ScrollbackCount: i,
			})); err != nil {
				t.Errorf("Apply() error = %v", err)
			}
		}(i)
		go func() {
			defer wg.Done()
			_ = d.EmulatorState()
		}()
		go func() {
			defer wg.Done()
			dst := image.NewNRGBA(image.Rectangle{Max: size})
			d.DrawInto(dst, dst.Bounds(), scr)
		}()
	}
	wg.Wait()
}

func TestRendererContextPaletteIsImmutable(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{B: 0xff, A: 0xff}
	red := color.NRGBA{R: 0xff, A: 0xff}
	scr := newTestScreen(1, 1)

	d := MustNew(
		WithPalette(Palette{Indexed: map[int]color.Color{1: blue}}),
		WithCellFgDrawer(func(ctx Context, _ draw.Image, _ image.Rectangle, _ *uv.Cell) {
			palette := ctx.Palette()
			palette.Indexed[1] = red
			if got := ctx.Palette().Indexed[1]; got != blue {
				t.Fatalf("mutated context palette = %#v, want original %#v", got, blue)
			}
		}),
	)

	_ = d.Draw(scr)
	if got := d.opts.palette.Indexed[1]; got != blue {
		t.Fatalf("renderer palette = %#v, want original %#v", got, blue)
	}
}

func assertPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", name)
		}
	}()
	fn()
}

type testScreen struct {
	*uv.Buffer
}

func newTestScreen(width, height int) testScreen {
	return testScreen{Buffer: uv.NewBuffer(width, height)}
}

func (s testScreen) WidthMethod() uv.WidthMethod {
	return testWidthMethod{}
}

type testWidthMethod struct{}

func (testWidthMethod) StringWidth(str string) int {
	if str == "" {
		return 0
	}
	return 1
}
