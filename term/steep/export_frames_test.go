// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/still/export"
)

// labelImage is a 1x1 image whose red channel identifies the frame.
func labelImage(label uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: label, A: 255})
	return img
}

func imageLabel(img image.Image) uint8 {
	r, _, _, _ := img.At(0, 0).RGBA()
	return uint8(r >> 8)
}

func TestExportPipeline(t *testing.T) {
	t.Parallel()

	// trace records the position flags each call saw, then passes frames on.
	trace := func(log *[]string) ExportMutator {
		return func(ec ExportContext, f ExportFrame) []ExportFrame {
			*log = append(*log, fmt.Sprintf("%d:%t:%t:%t", imageLabel(f.Image), ec.First, ec.Last, ec.Failed))
			return []ExportFrame{f}
		}
	}
	insert := func(label uint8, first bool) ExportMutator {
		return func(ec ExportContext, f ExportFrame) []ExportFrame {
			card := ExportFrame{Image: labelImage(label)}
			switch {
			case first && ec.First:
				return []ExportFrame{card, f}
			case !first && ec.Last:
				return []ExportFrame{f, card}
			}
			return []ExportFrame{f}
		}
	}
	keepFirst := func(ec ExportContext, f ExportFrame) []ExportFrame {
		if ec.First {
			return []ExportFrame{f}
		}
		return nil
	}

	cases := []struct {
		name      string
		frames    int
		mutators  func(log *[]string) []ExportMutator
		wantLog   []string
		wantOut   []uint8
		wantDelay []time.Duration
	}{
		{
			name:     "no mutators passes through",
			frames:   2,
			mutators: func(*[]string) []ExportMutator { return nil },
			wantOut:  []uint8{1, 2},
		},
		{
			name:     "single frame is first and last",
			frames:   1,
			mutators: func(log *[]string) []ExportMutator { return []ExportMutator{trace(log)} },
			wantLog:  []string{"1:true:true:true"},
			wantOut:  []uint8{1},
		},
		{
			name:   "later mutators see inserted cards",
			frames: 2,
			mutators: func(log *[]string) []ExportMutator {
				return []ExportMutator{insert(10, true), insert(20, false), trace(log)}
			},
			wantLog: []string{"10:true:false:false", "1:false:false:false", "2:false:false:false", "20:false:true:true"},
			wantOut: []uint8{10, 1, 2, 20},
		},
		{
			name:   "earlier mutators do not see later cards",
			frames: 2,
			mutators: func(log *[]string) []ExportMutator {
				return []ExportMutator{trace(log), insert(10, true)}
			},
			wantLog: []string{"1:true:false:false", "2:false:true:true"},
			wantOut: []uint8{10, 1, 2},
		},
		{
			name:   "last kept frame is last downstream",
			frames: 3,
			mutators: func(log *[]string) []ExportMutator {
				return []ExportMutator{keepFirst, trace(log)}
			},
			wantLog: []string{"1:true:true:true"},
			wantOut: []uint8{1},
		},
		{
			name:      "short delays are floored to one step",
			frames:    1,
			mutators:  func(*[]string) []ExportMutator { return []ExportMutator{insert(10, true)} },
			wantOut:   []uint8{10, 1},
			wantDelay: []time.Duration{50 * time.Millisecond, time.Second},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			ch := make(chan export.Frame, 16)
			p := exportPipeline{base: ExportContext{Step: 50 * time.Millisecond}, out: ch}
			for _, mutate := range tc.mutators(&log) {
				p.stages = append(p.stages, mutatorStage{mutate: mutate})
			}
			for i := range tc.frames {
				p.push(0, ExportFrame{Image: labelImage(uint8(i + 1)), Delay: time.Second})
			}
			p.finish(true)
			close(ch)

			var out []uint8
			var delays []time.Duration
			for f := range ch {
				out = append(out, imageLabel(f.Image))
				delays = append(delays, f.Delay)
			}
			if !slices.Equal(log, tc.wantLog) {
				t.Errorf("mutator calls = %q, want %q", log, tc.wantLog)
			}
			if !slices.Equal(out, tc.wantOut) {
				t.Errorf("recorded frames = %v, want %v", out, tc.wantOut)
			}
			if tc.wantDelay != nil && !slices.Equal(delays, tc.wantDelay) {
				t.Errorf("recorded delays = %v, want %v", delays, tc.wantDelay)
			}
		})
	}
}

func TestExportCardsMatchRecording(t *testing.T) {
	t.Parallel()
	var intro, outro image.Image
	var outroFailed bool
	capture := func(ec ExportContext, f ExportFrame) []ExportFrame {
		switch {
		case ec.First:
			intro = f.Image
		case ec.Last:
			outro, outroFailed = f.Image, ec.Failed
		}
		return []ExportFrame{f}
	}
	h := NewHarness(t, rootTestModel{},
		WithWindowSize(20, 4),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithExportMutators(introCard(time.Second), outroCard(time.Second), capture),
		WithIgnoreEnv(true),
	)
	h.Type("abc")
	want := h.Image().Bounds()
	g := finishGIF(t, h, true)

	if intro == nil || outro == nil {
		t.Fatalf("intro = %v, outro = %v; want both cards recorded", intro != nil, outro != nil)
	}
	if !outroFailed {
		t.Error("outro card should see the failed test result")
	}
	for name, img := range map[string]image.Image{"intro": intro, "outro": outro} {
		if img.Bounds() != want {
			t.Errorf("%s card bounds = %v, want the terminal render bounds %v", name, img.Bounds(), want)
		}
	}

	if len(g.Image) < 3 {
		t.Fatalf("gif frames = %d, want an intro, the recording, and an outro", len(g.Image))
	}
	for _, i := range []int{0, len(g.Delay) - 1} {
		if g.Delay[i] != 100 {
			t.Errorf("card frame %d delay = %d cs, want 100 cs", i, g.Delay[i])
		}
	}
}

// backgroundModel is rootTestModel with a program-set terminal background.
type backgroundModel struct{ rootTestModel }

func (m backgroundModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
	next, cmd := m.rootTestModel.Update(msg)
	m.rootTestModel, _ = next.(rootTestModel)
	return m, cmd
}

func (m backgroundModel) View() tea.View {
	v := m.rootTestModel.View()
	v.BackgroundColor = color.RGBA{R: 0x33, G: 0x55, B: 0xaa, A: 0xff}
	return v
}

func TestExportCardsIgnoreProgramColors(t *testing.T) {
	t.Parallel()
	var frames []image.Image
	capture := func(_ ExportContext, f ExportFrame) []ExportFrame {
		frames = append(frames, f.Image)
		return []ExportFrame{f}
	}
	h := NewHarness(t, backgroundModel{},
		WithWindowSize(20, 4),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithExportMutators(introCard(time.Second), outroCard(time.Second), capture),
		WithIgnoreEnv(true),
	)
	h.Type("abc")
	finishGIF(t, h, false)

	if len(frames) < 3 {
		t.Fatalf("frames = %d, want an intro, the recording, and an outro", len(frames))
	}
	intro, program, outro := frames[0], frames[len(frames)-2], frames[len(frames)-1]
	// A point in the left padding, clear of any text.
	x, y := 2, intro.Bounds().Dy()/2
	if intro.At(x, y) != outro.At(x, y) {
		t.Errorf("intro background = %v, outro = %v; want cards to match", intro.At(x, y), outro.At(x, y))
	}
	if intro.At(x, y) == program.At(x, y) {
		t.Errorf("card background = %v, want the renderer default, not the program's %v", intro.At(x, y), program.At(x, y))
	}
}

func TestExportIntroLogo(t *testing.T) {
	t.Parallel()
	var intro []ExportFrame
	capture := func(_ ExportContext, f ExportFrame) []ExportFrame {
		if f.At.IsZero() {
			intro = append(intro, f)
		}
		return []ExportFrame{f}
	}
	h := NewHarness(t, rootTestModel{},
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithExportMutators(introCard(1500*time.Millisecond), capture),
		WithIgnoreEnv(true),
	)
	h.Type("abc")
	want := h.Image().Bounds()
	finishGIF(t, h, false)

	// An empty hold, one frame per pixel row of fill, then ten steps of steam.
	if got := len(intro); got != 1+len(logoBitmap)+10 {
		t.Fatalf("intro frames = %d, want %d", got, 1+len(logoBitmap)+10)
	}
	var total time.Duration
	for i, f := range intro {
		if f.Image.Bounds() != want {
			t.Fatalf("intro frame %d bounds = %v, want %v", i, f.Image.Bounds(), want)
		}
		total += f.Delay
	}
	if want := logoEmptyHold + time.Duration(len(logoBitmap))*logoFillStep + 1500*time.Millisecond; total != want {
		t.Errorf("intro plays for %v, want %v", total, want)
	}
}

func TestExportCardsAfterMutators(t *testing.T) {
	t.Parallel()
	var seen int
	overlay := func(_ ExportContext, f ExportFrame) []ExportFrame {
		if f.At.IsZero() {
			t.Error("mutator saw a card; cards should be added after every mutator")
		}
		seen++
		return []ExportFrame{f}
	}
	h := NewHarness(t, rootTestModel{},
		WithWindowSize(20, 4),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithExportIntro(time.Second),
		WithExportOutro(time.Second),
		WithExportMutators(overlay),
		WithIgnoreEnv(true),
	)
	h.Type("abc")
	g := finishGIF(t, h, false)
	if len(g.Image) != seen+2 {
		t.Errorf("gif frames = %d, want the %d recorded frames plus an intro and an outro", len(g.Image), seen)
	}
}

func TestExportCardScale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cols, rows int
		want       float64
	}{
		{20, 4, 1},
		{80, 24, 1},
		{160, 48, 1.75},
		{240, 72, 2.5},
		{240, 24, 1}, // The smaller dimension wins.
	} {
		ec := ExportContext{src: frameSource{cols: tc.cols, rows: tc.rows}}
		if got := ec.cardScale(); got != tc.want {
			t.Errorf("cardScale(%dx%d) = %v, want %v", tc.cols, tc.rows, got, tc.want)
		}
	}
}

func TestExportFramesKeepTranslucency(t *testing.T) {
	t.Parallel()
	var frames []image.Image
	capture := func(_ ExportContext, f ExportFrame) []ExportFrame {
		frames = append(frames, f.Image)
		return []ExportFrame{f}
	}
	h := NewHarness(t, rootTestModel{},
		WithWindowSize(20, 4),
		WithRenderer(still.WithBackgroundOpacity(0.6)),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithExportMutators(capture),
		WithIgnoreEnv(true),
	)
	h.Type("abc").Wait("text=abc")
	want, ok := h.Image().(*image.NRGBA)
	if !ok {
		t.Fatalf("renderer image is %T, want *image.NRGBA", h.Image())
	}
	want = cloneNRGBA(want)
	finishGIF(t, h, false)

	got, ok := frames[len(frames)-1].(*image.NRGBA)
	if !ok {
		t.Fatalf("recorded frame is %T, want *image.NRGBA", frames[len(frames)-1])
	}
	if a := got.NRGBAAt(2, got.Bounds().Dy()/2).A; a == 0xff || a == 0 {
		t.Errorf("background alpha = %d, want the translucent renderer background", a)
	}
	if !slices.Equal(got.Pix, want.Pix) {
		t.Error("recorded frame differs from the renderer's image; want its exact pixels and alpha")
	}
}
