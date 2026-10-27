// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"fmt"
	"image"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// cellsOf returns a cell frame of a cols by rows screen with title, at at.
func cellsOf(cols, rows int, title string, at time.Duration) CellFrame {
	return CellFrame{Screen: vt.NewBuffer(cols, rows), State: vt.State{Title: title}, At: at}
}

func TestPipelineStages(t *testing.T) {
	t.Parallel()

	// trace records the position flags each call saw, then passes frames on.
	trace := func(log *[]string) Stage {
		return func(f Frame, emit func(Frame)) {
			*log = append(*log, fmt.Sprintf("%d:%t:%t:%t", imageLabel(f.Image), f.First, f.Last, f.Cells != nil))
			emit(f)
		}
	}
	insert := func(label uint8, first bool) Stage {
		return func(f Frame, emit func(Frame)) {
			card := Frame{Image: labelImage(label), Isolated: true}
			if first && f.First {
				emit(card)
			}
			emit(f)
			if !first && f.Last {
				emit(card)
			}
		}
	}
	keepFirst := func(f Frame, emit func(Frame)) {
		if f.First {
			emit(f)
		}
	}

	cases := []struct {
		name      string
		frames    int
		stages    func(log *[]string) []Stage
		wantLog   []string
		wantOut   []uint8
		wantDelay []time.Duration
	}{
		{
			name:    "no stages passes through",
			frames:  2,
			stages:  func(*[]string) []Stage { return nil },
			wantOut: []uint8{1, 2},
		},
		{
			name:    "single frame is first and last",
			frames:  1,
			stages:  func(log *[]string) []Stage { return []Stage{trace(log)} },
			wantLog: []string{"1:true:true:true"},
			wantOut: []uint8{1},
		},
		{
			name:   "later stages see inserted cards",
			frames: 2,
			stages: func(log *[]string) []Stage {
				return []Stage{insert(10, true), insert(20, false), trace(log)}
			},
			wantLog: []string{"10:true:false:false", "1:false:false:true", "2:false:false:true", "20:false:true:false"},
			wantOut: []uint8{10, 1, 2, 20},
		},
		{
			name:   "earlier stages do not see later cards",
			frames: 2,
			stages: func(log *[]string) []Stage {
				return []Stage{trace(log), insert(10, true)}
			},
			wantLog: []string{"1:true:false:true", "2:false:true:true"},
			wantOut: []uint8{10, 1, 2},
		},
		{
			name:   "last kept frame is last downstream",
			frames: 3,
			stages: func(log *[]string) []Stage {
				return []Stage{keepFirst, trace(log)}
			},
			wantLog: []string{"1:true:true:true"},
			wantOut: []uint8{1},
		},
		{
			name:      "short delays are floored to one step",
			frames:    1,
			stages:    func(*[]string) []Stage { return []Stage{insert(10, true)} },
			wantOut:   []uint8{10, 1},
			wantDelay: []time.Duration{50 * ms, time.Second},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			out := &collectSink{}
			p := newPipeline(testutil.Rasterizer{}, settings{step: 50 * ms, speed: 1}, tc.stages(&log), out)
			for i := range tc.frames {
				cf := cellsOf(1, 1, "", 0)
				p.push(0, Frame{Image: labelImage(uint8(i + 1)), Cells: &cf, Delay: time.Second})
			}
			if err := p.finish(0); err != nil {
				t.Fatal(err)
			}

			var got []uint8
			for _, f := range out.frames {
				got = append(got, imageLabel(f.Image))
			}
			if !slices.Equal(log, tc.wantLog) {
				t.Errorf("stage calls = %q, want %q", log, tc.wantLog)
			}
			if !slices.Equal(got, tc.wantOut) {
				t.Errorf("recorded frames = %v, want %v", got, tc.wantOut)
			}
			if tc.wantDelay != nil && !slices.Equal(out.delays(), tc.wantDelay) {
				t.Errorf("recorded delays = %v, want %v", out.delays(), tc.wantDelay)
			}
			for i, f := range out.frames {
				if f.Isolated != (imageLabel(f.Image) >= 10) {
					t.Errorf("frame %d Isolated = %v, want it on the cards only", i, f.Isolated)
				}
			}
		})
	}
}

// TestPipelineDelays checks that each frame ends at its offset from the first, scaled by the
// speed and rounded to a whole step of the delay grid, so the rounding error
// carries forward rather than adding up. A frame that ends on the step it
// starts on is dropped for the newer one. The last frame plays at least one
// step, extended by the hold, which the speed does not scale.
func TestPipelineDelays(t *testing.T) {
	t.Parallel()
	// every returns n offsets gap apart, from 0.
	every := func(n int, gap time.Duration) []time.Duration {
		out := make([]time.Duration, n)
		for i := range out {
			out[i] = time.Duration(i) * gap
		}
		return out
	}
	for _, tc := range []struct {
		fps   int
		speed float64
		hold  time.Duration
		at    []time.Duration
		end   time.Duration
		want  []time.Duration
	}{
		// Frames that end within half a step of their start are dropped.
		{fps: 0, at: []time.Duration{0, 4 * ms, 19 * ms}, end: 89 * ms, want: []time.Duration{100 * ms}},
		{fps: 100, at: []time.Duration{0, 4 * ms, 19 * ms}, end: 89 * ms, want: []time.Duration{20 * ms, 70 * ms}},
		{fps: 10, at: []time.Duration{0, 47 * ms, 147 * ms}, end: 447 * ms, want: []time.Duration{100 * ms, 300 * ms}},
		// 70ms gaps on a 50ms grid alternate between one and two steps, so
		// the recording plays as long as the timeline.
		{fps: 20, at: every(5, 70*ms), end: 350 * ms, want: []time.Duration{50 * ms, 100 * ms, 50 * ms, 100 * ms, 50 * ms}},
		// 10ms gaps on a 50ms grid keep about one frame in five, and the
		// last, which plays at least a step.
		{fps: 20, at: every(11, 10*ms), end: 110 * ms, want: []time.Duration{50 * ms, 50 * ms, 50 * ms}},
		// 30 FPS is a 30ms step: a frame at the rate plays slightly fast, but
		// a hold keeps its length.
		{fps: 30, at: []time.Duration{0, time.Second / 30}, end: time.Second/30 + 2*time.Second, want: []time.Duration{30 * ms, 2010 * ms}},
		// Speed scales the timeline, but not the hold.
		{fps: 100, speed: 2, hold: 500 * ms, at: []time.Duration{0, 400 * ms}, end: 1000 * ms, want: []time.Duration{200 * ms, 800 * ms}},
		// The last frame plays at least one step.
		{fps: 100, speed: 0.5, at: []time.Duration{0, 100 * ms}, end: 100 * ms, want: []time.Duration{200 * ms, 10 * ms}},
		// The grid counts from the first frame, wherever it is.
		{fps: 20, at: []time.Duration{1030 * ms, 1100 * ms}, end: 1200 * ms, want: []time.Duration{50 * ms, 100 * ms}},
	} {
		s, err := Profile{Outputs: Files("x.gif"), FPS: tc.fps, Speed: tc.speed, Hold: -1}.settings()
		if err != nil {
			t.Fatal(err)
		}
		out := &collectSink{}
		p := newPipeline(testutil.Rasterizer{}, settings{step: s.step, speed: s.speed, hold: tc.hold}, nil, out)
		for _, at := range tc.at {
			if err = p.record(cellsOf(1, 1, "", at)); err != nil {
				t.Fatal(err)
			}
		}
		if err = p.finish(tc.end); err != nil {
			t.Fatal(err)
		}
		if got := out.delays(); !slices.Equal(got, tc.want) {
			t.Errorf("fps %d speed %v: delays = %v, want %v", tc.fps, tc.speed, got, tc.want)
		}
	}
}

// TestPipelineSweep checks that a frame held on screen with indicators that
// move plays as frames sweepStep apart, even on a finer step grid, each
// repainted as of the time it stands for, while a still one stays one frame.
// A step that would look like the one before extends it.
func TestPipelineSweep(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		title  string
		speed  float64
		frames int
	}{
		{"still", 1, 1},
		{"spin", 1, 20},
		{"breathe", 1, 5},
		// At twice the speed, each frame stands for twice the timeline time.
		{"spin", 2, 20},
	} {
		out := &collectSink{}
		p := newPipeline(&fakeAnimator{}, settings{step: 10 * ms, speed: tc.speed}, nil, out)
		start := 1000 * time.Second
		if err := p.record(cellsOf(2, 1, tc.title, start)); err != nil {
			t.Fatal(err)
		}
		hold := time.Duration(float64(time.Second+20*ms) * tc.speed)
		if err := p.finish(start + hold); err != nil {
			t.Fatal(err)
		}
		var total time.Duration
		for i, f := range out.frames {
			// Repaint stores the offset in centiseconds (mod 256) in the
			// alpha of the last pixel; the first frame is the full draw.
			at := start + time.Duration(float64(total)*tc.speed)
			if got, want := out.images[i].Pix[7], uint8(at/(10*ms)); i > 0 && got != want {
				t.Errorf("%s x%v: frame %d repainted as of %d, want %d", tc.title, tc.speed, i, got, want)
			}
			total += f.Delay
		}
		if len(out.frames) != tc.frames || total != time.Second+20*ms {
			t.Errorf("%s x%v: %d frames over %v, want %d over 1.02s", tc.title, tc.speed, len(out.frames), total, tc.frames)
		}
	}
}

// TestPipelineHold checks that with stages the hold after the last frame
// plays as frames of its own, flagged, that sum to the hold, whether the
// last frame holds still or is swept, and that without stages it is not
// split off.
func TestPipelineHold(t *testing.T) {
	t.Parallel()
	const hold = time.Second
	for _, tc := range []struct {
		title  string
		stages bool
		frames int
	}{
		{"still", true, 2},
		{"spin", true, 0},
		{"still", false, 1},
	} {
		var got []Frame
		var stages []Stage
		if tc.stages {
			stages = []Stage{func(f Frame, emit func(Frame)) { got = append(got, f); emit(f) }}
		}
		out := &collectSink{}
		p := newPipeline(&fakeAnimator{}, settings{step: 10 * ms, speed: 1, hold: hold}, stages, out)
		if err := p.record(cellsOf(2, 1, tc.title, 0)); err != nil {
			t.Fatal(err)
		}
		if err := p.finish(500 * ms); err != nil {
			t.Fatal(err)
		}
		if !tc.stages {
			if len(out.frames) != tc.frames {
				t.Errorf("%s without stages: %d frames, want %d", tc.title, len(out.frames), tc.frames)
			}
			continue
		}
		if tc.frames > 0 && len(got) != tc.frames {
			t.Errorf("%s: %d frames, want %d", tc.title, len(got), tc.frames)
		}
		var body, held time.Duration
		for i, f := range got {
			switch {
			case f.Hold:
				held += f.Delay
			case held > 0:
				t.Errorf("%s: frame %d after the hold is not flagged", tc.title, i)
			default:
				body += f.Delay
			}
		}
		if body != 500*ms || held != hold {
			t.Errorf("%s: %v before the hold and %v flagged, want 500ms and %v", tc.title, body, held, hold)
		}
	}
}

// TestPipelineReusesImages checks that without stages the images written are
// reused, and that reuse never changes a frame already written.
func TestPipelineReusesImages(t *testing.T) {
	t.Parallel()
	rast := testutil.Rasterizer{}
	out := &collectSink{}
	p := newPipeline(rast, settings{step: 10 * ms, speed: 1}, nil, out)
	for i := range 10 {
		cf := cellsOf(3, 1, "", time.Duration(i)*100*ms)
		cf.Screen.SetCell(0, 0, &vt.Cell{Content: string(rune('a' + i)), Width: 1})
		if err := p.record(cf); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.finish(time.Second); err != nil {
		t.Fatal(err)
	}
	if len(p.free) == 0 {
		t.Error("no image was kept for reuse")
	}
	for i, img := range out.images {
		if got := img.Pix[0]; got != byte('a'+i) {
			t.Errorf("frame %d shows %q, want %q", i, got, rune('a'+i))
		}
	}
}

// TestPipelineSweepRenderer is [TestPipelineSweep] with the real renderer:
// every sweepStep of a held frame shows the pixels of drawing everything
// again as of that time, whether it got a frame of its own or extends the
// one before.
func TestPipelineSweepRenderer(t *testing.T) {
	t.Parallel()
	rend, err := render.New(render.WithWindowChrome(true))
	if err != nil {
		t.Fatal(err)
	}
	defer rend.Close()
	for _, tc := range []struct {
		seq    string
		frames int
	}{
		{"\x1b]9;4;3\x07", 20},
		{"\x1b]9;4;2\x07", 20},
		{"\x1b]9;4;1;50\x07", 1},
	} {
		var r *replay
		if r, err = newReplay(t.Context(), replayRuntime, Header{Cols: 20, Rows: 3}, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err = r.apply(outAt(0, "busy"+tc.seq)); err != nil {
			t.Fatal(err)
		}
		// A fixed offset into the timeline, so the indicators start at the
		// same phase every run.
		start := 1000 * time.Second
		cf := r.current(start)
		_ = r.close()

		out := &collectSink{}
		p := newPipeline(rend, settings{step: 10 * ms, speed: 1}, nil, out)
		if err = p.record(cf); err != nil {
			t.Fatal(err)
		}
		if err = p.finish(start + time.Second + 20*ms); err != nil {
			t.Fatal(err)
		}
		var total time.Duration
		var ends []time.Duration
		for _, f := range out.frames {
			total += f.Delay
			ends = append(ends, total)
		}
		if len(out.frames) != tc.frames || total != time.Second+20*ms {
			t.Fatalf("%q: %d frames over %v, want %d over 1.02s", tc.seq, len(out.frames), total, tc.frames)
		}
		moved := false
		for k, step := 0, time.Duration(0); step < time.Second; step += sweepStep {
			for ends[k] <= step {
				k++
			}
			full := image.NewNRGBA(rend.Layout(20, 3).Image)
			rend.Draw(full, cf.Screen, cf.State, start+step)
			got := out.images[k]
			if !bytes.Equal(got.Pix, full.Pix) {
				t.Fatalf("%q: frame %d differs from a full redraw at %v", tc.seq, k, step)
			}
			moved = moved || !bytes.Equal(got.Pix, out.images[0].Pix)
		}
		if tc.frames > 1 && !moved {
			t.Fatalf("%q: the redrawn frames do not move", tc.seq)
		}
	}
}
