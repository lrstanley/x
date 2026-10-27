// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt/screen"
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

// recordFrames is an [ExportMutator] that appends every frame it sees to
// frames and passes it on.
func recordFrames(frames *[]ExportFrame) ExportMutator {
	return func(_ ExportContext, f ExportFrame, emit func(ExportFrame)) {
		*frames = append(*frames, f)
		emit(f)
	}
}

// gifOptions record a GIF of a small terminal into a temporary directory,
// ignoring the environment, followed by opts.
func gifOptions(t *testing.T, opts ...Option) []Option {
	t.Helper()
	return append([]Option{
		WithWindowSize(20, 4),
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithIgnoreEnv(true),
	}, opts...)
}

func TestExportPipeline(t *testing.T) {
	t.Parallel()

	// trace records the position flags each call saw, then passes frames on.
	trace := func(log *[]string) ExportMutator {
		return func(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
			*log = append(*log, fmt.Sprintf("%d:%t:%t:%t", imageLabel(f.Image), ec.First, ec.Last, ec.Failed))
			emit(f)
		}
	}
	insert := func(label uint8, first bool) ExportMutator {
		return func(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
			card := ExportFrame{Image: labelImage(label)}
			if first && ec.First {
				emit(card)
			}
			emit(f)
			if !first && ec.Last {
				emit(card)
			}
		}
	}
	keepFirst := func(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
		if ec.First {
			emit(f)
		}
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
			ch := make(chan sink.Frame, 16)
			p := exportPipeline{base: ExportContext{Step: 50 * time.Millisecond}, out: []chan<- sink.Frame{ch}}
			for _, mutate := range tc.mutators(&log) {
				p.stages = append(p.stages, mutatorStage{mutate: mutate})
			}
			for i := range tc.frames {
				p.push(0, sourcedFrame{ExportFrame: ExportFrame{Image: labelImage(uint8(i + 1)), Delay: time.Second}})
			}
			p.finish(time.Now(), true)
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
	capture := func(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
		switch {
		case ec.First:
			intro = f.Image
			if !f.Isolated {
				t.Error("intro card should be palettized on its own")
			}
		case ec.Last:
			outro, outroFailed = f.Image, ec.Failed
			if !f.Isolated {
				t.Error("outro card should be palettized on its own")
			}
		default:
			if f.Isolated {
				t.Error("terminal frame should use the shared palette")
			}
		}
		emit(f)
	}
	h := NewHarness(t, rootTestModel{}, gifOptions(t,
		WithExportMutators(introCard(time.Second), outroCard(time.Second), capture),
	)...)
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
	var frames []ExportFrame
	h := NewHarness(t, backgroundModel{}, gifOptions(t,
		WithExportMutators(introCard(time.Second), outroCard(time.Second), recordFrames(&frames)),
	)...)
	h.Type("abc")
	finishGIF(t, h, false)

	if len(frames) < 3 {
		t.Fatalf("frames = %d, want an intro, the recording, and an outro", len(frames))
	}
	intro, program, outro := frames[0].Image, frames[len(frames)-2].Image, frames[len(frames)-1].Image
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
	var frames []ExportFrame
	h := NewHarness(t, rootTestModel{}, gifOptions(t,
		WithWindowSize(DefaultTermWidth, DefaultTermHeight),
		WithExportMutators(introCard(1500*time.Millisecond), recordFrames(&frames)),
	)...)
	h.Type("abc")
	want := h.Image().Bounds()
	finishGIF(t, h, false)
	for _, f := range frames {
		if f.At.IsZero() {
			continue
		}
		if f.Isolated {
			t.Error("terminal frame isolated = true, want the shared palette")
		}
	}
	intro := slices.DeleteFunc(frames, func(f ExportFrame) bool { return !f.At.IsZero() })

	// An empty hold, one frame per pixel row of fill, then ten steps of steam.
	if got := len(intro); got != 1+len(logoBitmap)+10 {
		t.Fatalf("intro frames = %d, want %d", got, 1+len(logoBitmap)+10)
	}
	var total time.Duration
	for i, f := range intro {
		if !f.Isolated {
			t.Errorf("intro frame %d isolated = false, want the logo on its own palette", i)
		}
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
	overlay := func(_ ExportContext, f ExportFrame, emit func(ExportFrame)) {
		if f.At.IsZero() {
			t.Error("mutator saw a card; cards should be added after every mutator")
		}
		seen++
		emit(f)
	}
	h := NewHarness(t, rootTestModel{}, gifOptions(t,
		WithExportIntro(time.Second),
		WithExportOutro(time.Second),
		WithExportMutators(overlay),
	)...)
	h.Type("abc")
	g := finishGIF(t, h, false)
	if len(g.Image) != seen+2 {
		t.Errorf("gif frames = %d, want the %d recorded frames plus an intro and an outro", len(g.Image), seen)
	}
}

// TestCardRendererIgnoresWindowChrome checks that cards and key chips, which
// reuse the harness renderer options, do not get a title bar of their own.
func TestCardRendererIgnoresWindowChrome(t *testing.T) {
	t.Parallel()
	scr := screen.NewBuffer(10, 2)
	var plain, chrome cardRenderer
	chrome.opts = []render.Option{render.WithWindowChrome(true)}
	t.Cleanup(func() { _ = plain.close(); _ = chrome.close() })

	want := plain.draw(t, 1, screen.State{Title: "title"}, scr)
	got := chrome.draw(t, 1, screen.State{Title: "title"}, scr)
	if got == nil || want == nil {
		t.Fatalf("draw returned nil: got %v, want %v", got, want)
	}
	if got.Bounds() != want.Bounds() {
		t.Errorf("card bounds with window chrome = %v, want %v", got.Bounds(), want.Bounds())
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
	var frames []ExportFrame
	h := NewHarness(t, rootTestModel{}, gifOptions(t,
		WithRenderer(render.WithBackgroundOpacity(0.6)),
		WithExportMutators(recordFrames(&frames)),
	)...)
	h.Type("abc").Wait("text=abc")
	want, ok := h.Image().(*image.NRGBA)
	if !ok {
		t.Fatalf("renderer image is %T, want *image.NRGBA", h.Image())
	}
	want = cloneNRGBA(want)
	finishGIF(t, h, false)

	last := frames[len(frames)-1].Image
	got, ok := last.(*image.NRGBA)
	if !ok {
		t.Fatalf("recorded frame is %T, want *image.NRGBA", last)
	}
	if a := got.NRGBAAt(2, got.Bounds().Dy()/2).A; a == 0xff || a == 0 {
		t.Errorf("background alpha = %d, want the translucent renderer background", a)
	}
	if !slices.Equal(got.Pix, want.Pix) {
		t.Error("recorded frame differs from the renderer's image; want its exact pixels and alpha")
	}
}

// TestExportRecycledFramesMatch records many different screens through a
// pipeline without mutators, whose frames the encoder recycles, and checks the
// GIF matches a recording of independent copies. Each recycled buffer is
// poisoned as the encoder releases it, so a read after release shows up.
func TestExportRecycledFramesMatch(t *testing.T) {
	h := NewHarness(t, backgroundModel{rootTestModel{}}, WithWindowSize(20, 4), WithIgnoreEnv(true))
	var screens []*screen.Buffer
	var states []screen.State
	text := ""
	for _, c := range "abcdefg" {
		text += string(c)
		h.Type(string(c)).Wait("text=" + text)
		screens = append(screens, h.session.Terminal().Screen())
		states = append(states, h.session.Terminal().State())
	}

	record := func(recycle bool) *gif.GIF {
		in, out, relayed := make(chan sink.Frame), make(chan sink.Frame), make(chan struct{})
		go func() {
			defer close(relayed)
			defer close(out)
			for f := range in {
				if !recycle {
					f.Image, f.Done = cloneNRGBA(f.Image), nil
				} else if done, img := f.Done, f.Image.(*image.NRGBA); done != nil {
					f.Done = func() {
						clear(img.Pix)
						for i := range img.Pix {
							img.Pix[i] = 0xAA
						}
						done()
					}
				}
				out <- f
			}
		}()
		var buf bytes.Buffer
		enc, err := sink.NewGIF(&buf)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := sink.Record(enc, sink.WithChannel(out))
		if err != nil {
			t.Fatal(err)
		}
		p := &exportPipeline{base: ExportContext{Step: 10 * time.Millisecond}, speed: 1, out: []chan<- sink.Frame{in}}
		at := time.Now()
		for i := range 40 {
			j := (i * 3) % len(screens)
			img, _ := copyCommitted(h, screens[j], states[j], h.start)
			p.record(img, at, frameSource{})
			at = at.Add(50 * time.Millisecond)
		}
		p.finish(at, false)
		close(in)
		<-relayed
		if err := rec.Close(); err != nil {
			t.Fatal(err)
		}
		g, err := gif.DecodeAll(&buf)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}

	want, got := record(false), record(true)
	if len(want.Image) < 10 || len(got.Image) != len(want.Image) {
		t.Fatalf("got %d frames, want %d (at least 10)", len(got.Image), len(want.Image))
	}
	for i := range want.Image {
		if got.Delay[i] != want.Delay[i] || !slices.Equal(got.Image[i].Pix, want.Image[i].Pix) ||
			got.Image[i].Bounds() != want.Image[i].Bounds() {
			t.Errorf("frame %d differs from the non-recycled recording", i)
		}
	}
	if len(frameBufs) == 0 {
		t.Error("no buffer was recycled")
	}
}

// TestNewNRGBAIgnoresStalePixels checks a recycled buffer is fully repainted,
// matching a fresh draw, and that a buffer too small to reuse is dropped.
func TestNewNRGBAIgnoresStalePixels(t *testing.T) {
	h := NewHarness(t, backgroundModel{rootTestModel{}}, WithWindowSize(20, 4), WithIgnoreEnv(true))
	h.Type("a").Wait("text=a")
	scr, st := h.session.Terminal().Screen(), h.session.Terminal().State()

	// copyCommitted draws with the harness's focus, as of h.start.
	fresh := st
	fresh.Focused = h.session.Focused()
	h.imageMu.Lock()
	want := image.NewNRGBA(h.imageRenderer.Layout(scr.Width(), scr.Height()).Image)
	h.imageRenderer.Draw(want, scr, fresh, 0)
	h.imageMu.Unlock()
	for _, size := range []int{1, len(want.Pix), 2 * len(want.Pix)} {
		pix := slices.Repeat([]byte{0xAA}, size)
		select {
		case frameBufs <- pix:
		default:
			<-frameBufs
			frameBufs <- pix
		}
		img, _ := copyCommitted(h, scr, st, h.start)
		got, ok := img.(*image.NRGBA)
		if !ok {
			t.Fatalf("copyCommitted image is %T, want *image.NRGBA", img)
		}
		if !slices.Equal(got.Pix, want.Pix) || got.Stride != want.Stride || got.Rect != want.Rect {
			t.Errorf("buffer of %d bytes: recycled draw differs from a fresh one", size)
		}
	}
}

// TestExportAnimatesIndicators checks that a frame held on screen with an
// animated progress bar or program status plays as redrawn frames sweepStep
// apart, even on a finer step grid, so they move in the GIF, while a still
// one stays one frame. A step that would look like the one before extends it,
// so a blocked status plays one frame per breathing step.
func TestExportAnimatesIndicators(t *testing.T) {
	t.Parallel()

	progress := NewHarness(t, rootTestModel{text: "busy"}, WithWindowSize(20, 3), WithRenderer(render.WithWindowChrome(true)))
	progress.Wait("text=busy")
	status := statusHarness(t, "held")
	set := ansi.SetProgramStatus
	for _, tc := range []struct {
		h      *Harness
		seq    string
		frames int
	}{
		{progress, "\x1b]9;4;3\x07", 20},
		{progress, "\x1b]9;4;2\x07", 20},
		{progress, "\x1b]9;4;1;50\x07", 1},
		{status, set(ansi.ProgramStatus{State: ansi.ProgramStateWorking, Title: "build"}), 20},
		{status, set(ansi.ProgramStatus{State: ansi.ProgramStateBlocked, Kind: ansi.ProgramStatusKindQuestion, Title: "which?"}), 7},
		{status, set(ansi.ProgramStatus{State: ansi.ProgramStateWorking, Progress: 40, HasProgress: true, Title: "build"}), 1},
		{status, set(ansi.ProgramStatus{State: ansi.ProgramStateDone, Title: "done"}), 1},
	} {
		assertHeldFrames(t, tc.h, tc.seq, tc.frames)
	}
}

// assertHeldFrames writes seq to h and holds the frame for a little over a
// second, and checks that it plays as frames frames, that every step of the
// hold shows the pixels of a full redraw at its time, and that they move when
// there is more than one frame.
func assertHeldFrames(t *testing.T, h *Harness, seq string, frames int) {
	t.Helper()
	writeVT(t, h, seq)
	scr, st := h.session.Terminal().Screen(), h.session.Terminal().State()

	ch := make(chan sink.Frame, 64)
	p := exportPipeline{base: ExportContext{Step: 10 * time.Millisecond, h: h}, speed: 1, out: []chan<- sink.Frame{ch}}
	// A fixed offset into the timeline, so the indicators start at the same
	// phase every run.
	at := h.start.Add(1000 * time.Second)
	img, held := copyCommitted(h, scr, st, at)
	if (held != nil) != (frames > 1) {
		t.Fatalf("%q: held indicators set = %v, want %v", seq, held != nil, frames > 1)
	}
	if held != nil {
		// A repaint paints the frame as it was captured, not the harness now.
		later := at.Add(100 * time.Millisecond)
		want := cloneNRGBA(img)
		held.repaint(want, later)
		got := cloneNRGBA(img)
		h.Blur()
		held.repaint(got, later)
		h.Focus()
		if !bytes.Equal(want.Pix, got.Pix) {
			t.Fatalf("%q: repaint changed with the harness focus", seq)
		}
	}
	p.record(img, at, frameSource{state: st, held: held})
	p.finish(at.Add(time.Second+20*time.Millisecond), false)
	close(ch)

	var total time.Duration
	var got []*image.NRGBA
	var ends []time.Duration
	for f := range ch {
		img, ok := f.Image.(*image.NRGBA)
		if !ok {
			t.Fatalf("frame image is %T, want *image.NRGBA", f.Image)
		}
		total += f.Delay
		got = append(got, img)
		ends = append(ends, total)
	}
	if len(got) != frames || total != time.Second+20*time.Millisecond {
		t.Fatalf("%q: %d frames over %v, want %d over 1.02s", seq, len(got), total, frames)
	}
	// Every sweepStep of the hold must show the pixels of drawing everything
	// again as of that time, whether it got a frame of its own or extends the
	// one before.
	moved := false
	for k, step := 0, time.Duration(0); step < time.Second; step += sweepStep {
		for ends[k] <= step {
			k++
		}
		full, _ := copyCommitted(h, scr, st, at.Add(step))
		if want, ok := full.(*image.NRGBA); !ok || !bytes.Equal(got[k].Pix, want.Pix) {
			t.Fatalf("%q: frame %d differs from a full redraw at %v", seq, k, step)
		}
		moved = moved || !bytes.Equal(got[k].Pix, got[0].Pix)
	}
	if frames > 1 && !moved {
		t.Fatalf("%q: the redrawn frames do not move", seq)
	}
}
