// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package anim

import (
	"testing"
	"time"
)

func (s *Sequence) frameMsg() FrameMsg { return FrameMsg{id: s.v.id, gen: s.v.gen} }

// tick advances the clock by d and delivers the sequence's frame, returning
// the command Update asked for.
func tick(c *clock, s *Sequence, d time.Duration) bool {
	c.advance(d)
	return s.Update(s.frameMsg()) != nil
}

const ms = time.Millisecond

func newTestSequence(c *clock, steps ...Step) Sequence {
	return NewSequence(0, steps...).With(Clock(c.Now), Duration(100*ms), Ease(Linear))
}

func TestSequencePlayOnce(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(10), Hold(200*ms), To(0, Duration(50*ms)))
	if s.Animating() || s.Step() != -1 {
		t.Fatal("a new sequence is running")
	}
	if s.Play() == nil || !s.Animating() || s.Step() != 0 {
		t.Fatal("Play did not start the first step")
	}

	tests := []struct {
		advance time.Duration
		value   float64
		step    int
		running bool
	}{
		{50 * ms, 5, 0, true},
		{50 * ms, 10, 1, true},  // the hold starts as the tween ends
		{100 * ms, 10, 1, true}, // half way through the hold
		{100 * ms, 10, 2, true}, // the hold ends, the next tween starts
		{25 * ms, 5, 2, true},   // half way back
		{25 * ms, 0, -1, false}, // done
		{time.Second, 0, -1, false},
	}
	for i, tt := range tests {
		if got := tick(c, &s, tt.advance); got != tt.running {
			t.Fatalf("%d: next frame requested = %v, want %v", i, got, tt.running)
		}
		if !near(s.Float(), tt.value) || s.Step() != tt.step || s.Animating() != tt.running {
			t.Fatalf("%d: value %v step %d animating %v, want %v %d %v",
				i, s.Float(), s.Step(), s.Animating(), tt.value, tt.step, tt.running)
		}
	}
}

func TestSequenceNoDrift(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(1), Hold(100*ms), To(0))
	s.Play()
	// Frames arrive 30ms late every time. The steps still end at 100, 200, and
	// 300ms, so the value is where the schedule says, not where the frames do.
	for range 4 {
		tick(c, &s, 75*ms)
	}
	if s.Animating() || s.Float() != 0 {
		t.Fatalf("after 300ms: value %v animating %v", s.Float(), s.Animating())
	}

	c = &clock{now: time.Unix(0, 0)}
	s = newTestSequence(c, To(1), Hold(100*ms), To(0))
	s.Play()
	tick(c, &s, 250*ms) // 50ms into the last tween, though the frame is late
	if !near(s.Float(), 0.5) || s.Step() != 2 {
		t.Fatalf("a late frame: value %v step %d, want 0.5 in step 2", s.Float(), s.Step())
	}
}

func TestSequenceLoop(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, Jump(0), To(1), Hold(100*ms)) // a 200ms cycle
	if s.Loop() == nil {
		t.Fatal("Loop returned no command")
	}
	tests := []struct {
		advance time.Duration
		value   float64
		step    int
	}{
		{50 * ms, 0.5, 1},
		{50 * ms, 1, 2},
		{150 * ms, 0.5, 1}, // t=250ms, 50ms into the second cycle
		{50 * ms, 1, 2},
		{150 * ms, 0.5, 1},
		{200 * ms, 0.5, 1},
	}
	for i, tt := range tests {
		if !tick(c, &s, tt.advance) {
			t.Fatalf("%d: a loop stopped asking for frames", i)
		}
		if !near(s.Float(), tt.value) || s.Step() != tt.step {
			t.Fatalf("%d: %v in step %d, want %v in step %d", i, s.Float(), s.Step(), tt.value, tt.step)
		}
	}
	s.Stop()
	if s.Animating() {
		t.Fatal("still running after Stop")
	}
}

func TestSequenceLoopCatchesUp(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, Jump(0), To(1), Hold(100*ms)) // a 200ms cycle
	s.Loop()
	tick(c, &s, 10*time.Second+50*ms) // the app was suspended for 10 whole cycles
	if !near(s.Float(), 0.5) || s.Step() != 1 {
		t.Fatalf("after a long pause: %v in step %d, want 0.5 in step 1", s.Float(), s.Step())
	}
}

func TestSequenceShortLoopSkipsMissedRuns(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, Jump(0), To(1, Duration(time.Microsecond)), Hold(time.Microsecond))
	s.Loop()
	start := time.Now()
	tick(c, &s, time.Hour)
	if elapsed := time.Since(start); elapsed > 50*ms {
		t.Fatalf("catching up on an hour of a 2µs loop took %v", elapsed)
	}
	if !s.Animating() {
		t.Fatal("the loop stopped")
	}
}

func TestSequenceEmptyLoopEnds(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, Jump(3), Hold(0))
	if cmd := s.Loop(); cmd != nil || s.Animating() {
		t.Fatal("a loop that takes no time kept running")
	}
	if s.Float() != 3 {
		t.Fatalf("value %v, want the jump's 3", s.Float())
	}

	empty := NewSequence(2)
	if empty.Play() != nil || empty.Loop() != nil || empty.Animating() || empty.Float() != 2 {
		t.Fatal("a sequence without steps ran")
	}
	var zero Sequence
	if zero.Play() != nil || zero.Update(FrameMsg{}) != nil {
		t.Fatal("the zero sequence ran")
	}
}

func TestSequenceStop(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(10))
	s.Play()
	stale := s.frameMsg()
	tick(c, &s, 50*ms)
	s.Stop()
	c.advance(time.Second)
	if s.Update(stale) != nil || s.Update(s.frameMsg()) != nil {
		t.Fatal("a stopped sequence asked for a frame")
	}
	if !near(s.Float(), 5) || s.Animating() || s.Step() != -1 {
		t.Fatalf("stopped at %v, animating %v, step %d", s.Float(), s.Animating(), s.Step())
	}
}

func TestSequenceReplayMidRun(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(10), Hold(100*ms))
	s.Play()
	old := s.frameMsg()
	tick(c, &s, 150*ms) // in the hold
	if s.Step() != 1 {
		t.Fatalf("step %d, want 1", s.Step())
	}

	if s.Play() == nil || s.Step() != 0 {
		t.Fatal("Play did not restart")
	}
	if s.Update(old) != nil {
		t.Fatal("a frame from before the replay was accepted")
	}
	// The replay tweens from where the value was: 10 to 10 ends at once,
	// but over its full duration.
	tick(c, &s, 50*ms)
	if s.Step() != 0 {
		t.Fatalf("step %d half way through the replayed tween, want 0", s.Step())
	}
	tick(c, &s, 50*ms)
	if s.Step() != 1 {
		t.Fatalf("step %d after the replayed tween, want 1", s.Step())
	}
}

func TestSequenceHoldWaitsOnce(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, Hold(time.Second), Jump(1))
	if s.Play() == nil {
		t.Fatal("a hold asked for no frame")
	}
	if cmd := s.Update(s.frameMsg()); cmd == nil || s.Step() != 0 {
		t.Fatal("an early frame should wait for the rest of the hold")
	}
	c.advance(time.Second)
	if s.Update(s.frameMsg()) != nil || s.Float() != 1 || s.Animating() {
		t.Fatalf("after the hold: %v, animating %v", s.Float(), s.Animating())
	}
}

func TestSequenceStepOptions(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(1, Duration(400*ms)), To(0))
	s.Play()
	tick(c, &s, 200*ms)
	if !near(s.Float(), 0.5) {
		t.Fatalf("a step with its own duration: %v, want 0.5", s.Float())
	}
	tick(c, &s, 200*ms) // ends the first, starts the second
	tick(c, &s, 50*ms)  // the second goes by the sequence's 100ms
	if !near(s.Float(), 0.5) {
		t.Fatalf("a step without options: %v, want 0.5", s.Float())
	}
}

func TestSequenceSpring(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(20, Spring(8, 1)), Hold(100*ms), To(0))
	s.Play()
	for range 600 {
		if s.Step() != 0 {
			break
		}
		tick(c, &s, time.Second/60)
	}
	if s.Step() != 1 || s.Float() != 20 {
		t.Fatalf("spring did not settle: %v in step %d", s.Float(), s.Step())
	}
	tick(c, &s, 100*ms)
	if s.Step() != 2 {
		t.Fatalf("step %d after the hold, want 2", s.Step())
	}
	tick(c, &s, 50*ms)
	if !near(s.Float(), 10) {
		t.Fatalf("a tween after a spring: %v, want 10", s.Float())
	}
}

func TestSequenceIgnoresOtherFrames(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	a := newTestSequence(c, To(1))
	b := newTestSequence(c, To(1))
	a.Play()
	b.Play()
	c.advance(50 * ms)
	if a.Update(b.frameMsg()) != nil || near(a.Float(), 0.5) {
		t.Fatal("another sequence's frame advanced the sequence")
	}
	v := NewValue(0, Clock(c.Now))
	if a.Update(v.frameMsg()) != nil || near(a.Float(), 0.5) {
		t.Fatal("a value's frame advanced the sequence")
	}
	if a.Update("other") != nil {
		t.Fatal("an unrelated message produced a command")
	}
}

func TestSequenceJump(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := newTestSequence(c, To(1), Jump(0.25), To(0.5))
	s.Play()
	tick(c, &s, 100*ms) // the tween ends, the jump happens, and the next starts
	if !near(s.Float(), 0.25) || s.Step() != 2 {
		t.Fatalf("after the jump: %v in step %d, want 0.25 in step 2", s.Float(), s.Step())
	}
	tick(c, &s, 50*ms)
	if !near(s.Float(), 0.375) {
		t.Fatalf("tween from the jump: %v, want 0.375", s.Float())
	}
}

func TestNewLoop(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	s := NewLoop(time.Second, Clock(c.Now))
	s.Loop()
	tick(c, &s, 250*ms)
	if !near(s.Float(), 0.25) {
		t.Fatalf("a quarter of the way: %v", s.Float())
	}
	tick(c, &s, time.Second)
	if !near(s.Float(), 0.25) || !s.Animating() {
		t.Fatalf("a period later: %v, animating %v", s.Float(), s.Animating())
	}
}
