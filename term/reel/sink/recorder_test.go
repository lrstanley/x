// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"image"
	"image/color"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

// collectEncoder records every frame it receives and how it was finished.
// failAt, when positive, makes the failAt-th write (1-based) return
// errEncoderFull.
type collectEncoder struct {
	frames []Frame
	// released counts the Done calls of the frames sent, and releasedAt how
	// many there had been when each frame was written.
	released   int
	releasedAt []int
	failAt     int
	closed     bool
	discarded  bool
}

var errEncoderFull = errors.New("encoder full")

func (e *collectEncoder) WriteFrame(f Frame) error {
	if e.failAt > 0 && len(e.frames)+1 == e.failAt {
		return errEncoderFull
	}
	e.frames = append(e.frames, f)
	e.releasedAt = append(e.releasedAt, e.released)
	return nil
}

func (e *collectEncoder) Close() error   { e.closed = true; return nil }
func (e *collectEncoder) Discard() error { e.discarded = true; return nil }

// done returns a [Frame.Done] that counts releases on e.
func (e *collectEncoder) done() func() { return func() { e.released++ } }

func (e *collectEncoder) delays() []time.Duration {
	out := make([]time.Duration, len(e.frames))
	for i, f := range e.frames {
		out[i] = f.Delay
	}
	return out
}

func TestRecorderResolvesDelays(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ch := make(chan Frame)
		enc := &collectEncoder{}
		rec, err := Record(enc, WithChannel(ch))
		if err != nil {
			t.Fatal(err)
		}

		imgs := []image.Image{solidFrame(1, 4, 4), solidFrame(2, 4, 4), solidFrame(3, 4, 4), solidFrame(4, 8, 2)}
		synctest.Sleep(40 * time.Millisecond)
		ch <- Frame{Image: imgs[0], Done: enc.done()}
		ch <- Frame{Image: imgs[1], Delay: 250 * time.Millisecond, Done: enc.done()}
		synctest.Sleep(70 * time.Millisecond)
		ch <- Frame{Image: imgs[2], Isolated: true, Done: enc.done()}
		ch <- Frame{Image: imgs[3], Delay: -time.Second, Done: enc.done()}
		close(ch)

		if err := rec.Close(); err != nil {
			t.Fatal(err)
		}
		// The negative delay falls back to elapsed time, which is zero here, and
		// rounds up to the one-step minimum.
		want := []time.Duration{40 * time.Millisecond, 250 * time.Millisecond, 70 * time.Millisecond, 10 * time.Millisecond}
		if got := enc.delays(); !slices.Equal(got, want) {
			t.Fatalf("delays = %v, want %v", got, want)
		}
		for i, f := range enc.frames {
			if f.Image != imgs[i] || f.Done != nil {
				t.Fatalf("frame %d is not the image sent at %d, without its Done", i, i)
			}
			if enc.releasedAt[i] != i {
				t.Fatalf("frame %d written after %d releases, want %d", i, enc.releasedAt[i], i)
			}
		}
		if !enc.frames[2].Isolated {
			t.Fatal("Isolated was not passed to the encoder")
		}
		if enc.released != len(imgs) || rec.Frames() != len(imgs) {
			t.Fatalf("released %d and wrote %d frames, want %d", enc.released, rec.Frames(), len(imgs))
		}
		if !enc.closed || enc.discarded {
			t.Fatal("Close did not close the encoder")
		}
	})
}

func TestRecorderDelayGrid(t *testing.T) {
	t.Parallel()

	ms := time.Millisecond
	for _, tc := range []struct {
		fps      int
		step     time.Duration
		in, want []time.Duration
	}{
		// Delays round to whole steps, at least one.
		{0, 10 * ms, []time.Duration{4 * ms, 15 * ms, 70 * ms}, []time.Duration{10 * ms, 20 * ms, 70 * ms}},
		{10, 100 * ms, []time.Duration{47 * ms, 100 * ms, 300 * ms}, []time.Duration{100 * ms, 100 * ms, 300 * ms}},
		{20, 50 * ms, []time.Duration{3 * ms, 50 * ms, 120 * ms}, []time.Duration{50 * ms, 50 * ms, 100 * ms}},
		// 30 FPS is a 30ms step: a frame at the rate plays slightly fast, but a
		// hold keeps its length.
		{30, 30 * ms, []time.Duration{time.Second / 30, 2 * time.Second}, []time.Duration{30 * ms, 2010 * ms}},
	} {
		r := &Recorder{step: delayStep(options{delayGrid: tc.fps, delayGridSet: tc.fps > 0})}
		if r.step != tc.step {
			t.Fatalf("fps %d: step = %v, want %v", tc.fps, r.step, tc.step)
		}
		for i, d := range tc.in {
			if got := r.nextDelay(d); got != tc.want[i] {
				t.Errorf("fps %d: delay %v = %v, want %v", tc.fps, d, got, tc.want[i])
			}
		}
	}
}

func TestRecorderCloseDrainsBuffered(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ch := make(chan Frame, 4)
		enc := &collectEncoder{}
		rec, err := Record(enc, WithChannel(ch))
		if err != nil {
			t.Fatal(err)
		}
		// Hold the reader on the first frame so the rest are still buffered
		// when Close runs.
		gate := make(chan struct{})
		ch <- Frame{Image: solidFrame(1, 4, 4), Delay: time.Millisecond, Done: func() { <-gate }}
		synctest.Wait()
		for i := range 3 {
			ch <- Frame{Image: solidFrame(i+2, 4, 4), Delay: time.Millisecond, Done: enc.done()}
		}

		closed := make(chan error)
		go func() { closed <- rec.Close() }()
		synctest.Wait()
		close(gate)
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		if n := len(enc.frames); n != 4 {
			t.Fatalf("got %d frames, want 4", n)
		}
		if enc.released != 3 {
			t.Fatalf("Done called %d times, want 3", enc.released)
		}
		if err := rec.Close(); err != nil {
			t.Fatalf("second Close = %v, want nil", err)
		}
	})
}

// TestRecorderErrorDiscards checks that after an invalid frame or an encoder
// error the recorder keeps releasing frames, writes no more, and discards the
// output.
func TestRecorderErrorDiscards(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		failAt int
		first  image.Image
		want   error
		kept   int
	}{
		"encoder error": {failAt: 2, first: solidFrame(1, 4, 4), want: errEncoderFull, kept: 1},
		"nil frame":     {want: errNilFrame},
		"empty frame":   {first: image.NewNRGBA(image.Rect(0, 0, 0, 0)), want: errEmptyFrame},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ch := make(chan Frame)
			enc := &collectEncoder{failAt: tc.failAt}
			rec, err := Record(enc, WithChannel(ch))
			if err != nil {
				t.Fatal(err)
			}
			// The channel is unbuffered, so every send returning means the
			// recorder kept reading after the error.
			ch <- Frame{Image: tc.first, Delay: time.Millisecond, Done: enc.done()}
			for i := range 3 {
				ch <- Frame{Image: solidNRGBA(2, 2, color.NRGBA{R: uint8(i), A: 0xff}), Delay: time.Millisecond, Done: enc.done()}
			}
			close(ch)

			if err := rec.Close(); !errors.Is(err, tc.want) {
				t.Fatalf("Close = %v, want %v", err, tc.want)
			}
			if err := rec.Close(); !errors.Is(err, tc.want) {
				t.Fatalf("second Close = %v, want %v", err, tc.want)
			}
			if len(enc.frames) != tc.kept || rec.Frames() != tc.kept {
				t.Fatalf("encoder got %d frames (recorder says %d), want %d before the error", len(enc.frames), rec.Frames(), tc.kept)
			}
			if enc.released != 4 {
				t.Fatalf("Done called %d times, want 4", enc.released)
			}
			if enc.closed || !enc.discarded {
				t.Fatal("output was not discarded")
			}
		})
	}
}

func TestRecorderDiscard(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame, 1)
	enc := &collectEncoder{}
	rec, err := Record(enc, WithChannel(ch))
	if err != nil {
		t.Fatal(err)
	}
	ch <- Frame{Image: solidFrame(1, 4, 4), Delay: time.Millisecond}
	if err := rec.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close after Discard = %v, want nil", err)
	}
	if enc.closed || !enc.discarded {
		t.Fatal("Discard did not discard the output")
	}
}

func TestRecordOptions(t *testing.T) {
	t.Parallel()

	ch := make(chan Frame)

	if _, err := Record(nil, WithChannel(ch)); err == nil {
		t.Fatal("expected error for nil encoder")
	}
	for name, opts := range map[string][]Option{
		"no source":    nil,
		"two sources":  {WithChannel(ch), WithFrameRate(10, false, func() image.Image { return nil })},
		"grid too big": {WithChannel(ch), WithDelayGrid(MaxFrameRate + 1)},
	} {
		enc := &collectEncoder{}
		if _, err := Record(enc, opts...); err == nil {
			t.Fatalf("%s: expected error", name)
		}
		if !enc.discarded {
			t.Fatalf("%s: a failed Record did not discard its encoder", name)
		}
	}

	// Encoder options are ignored, so one list can serve both.
	rec, err := Record(
		&collectEncoder{},
		WithChannel(ch),
		WithDelayGrid(60),
		WithLoopCount(maxGIFLoopCount+1),
		WithOptimize(OptimizeAll),
		WithPalette(color.Palette{color.Black}),
		WithBackground(color.Black),
		WithMaxFrames(10),
		WithMaxBytes(1024),
		WithQuality(MaxQuality+1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
}
