// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package anim

import (
	"math"
	"testing"
	"time"
)

// clock is a fake source of time.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }
func (v *Value) frameMsg() FrameMsg      { return FrameMsg{id: v.id, gen: v.gen} }
func near(a, b float64) bool             { return math.Abs(a-b) < 1e-9 }

func TestValueTween(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	v := NewValue(0, Duration(100*time.Millisecond), Ease(Linear), Clock(c.Now))
	if cmd := v.To(10); cmd == nil {
		t.Fatal("To returned no command")
	}
	if !v.Animating() || v.Target() != 10 {
		t.Fatal("not animating toward 10")
	}

	c.advance(50 * time.Millisecond)
	if cmd := v.Update(v.frameMsg()); cmd == nil {
		t.Fatal("no next frame half way")
	}
	if !near(v.Float(), 5) {
		t.Fatalf("half way = %v, want 5", v.Float())
	}

	c.advance(60 * time.Millisecond)
	if cmd := v.Update(v.frameMsg()); cmd != nil {
		t.Fatal("asked for a frame after finishing")
	}
	if v.Float() != 10 || v.Int() != 10 || v.Animating() {
		t.Fatalf("finished at %v, animating %v", v.Float(), v.Animating())
	}
}

func TestValueRetarget(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	v := NewValue(0, Duration(100*time.Millisecond), Ease(Linear), Clock(c.Now))
	v.To(10)
	c.advance(50 * time.Millisecond)
	v.Update(v.frameMsg())
	if cmd := v.To(0); cmd != nil {
		t.Fatal("retargeting a running value started a second chain of frames")
	}
	c.advance(50 * time.Millisecond)
	v.Update(v.frameMsg())
	if !near(v.Float(), 2.5) {
		t.Fatalf("half way back from 5 = %v, want 2.5", v.Float())
	}
}

func TestValueIgnoresOtherFrames(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	a := NewValue(0, Clock(c.Now))
	b := NewValue(0, Clock(c.Now))
	a.To(1)
	b.To(1)
	stale := a.frameMsg()
	a.Set(5)
	c.advance(time.Second)
	if a.Update(stale) != nil || a.Float() != 5 {
		t.Fatal("a frame from before Set moved the value")
	}
	if b.Update(stale) != nil || b.Float() != 0 {
		t.Fatal("another value's frame moved the value")
	}
}

func TestValueSpring(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	v := NewValue(0, Spring(8, 1), Clock(c.Now))
	v.To(20)
	for range 600 {
		c.advance(time.Second / 60)
		if v.Update(v.frameMsg()) == nil {
			break
		}
	}
	if v.Animating() || v.Float() != 20 {
		t.Fatalf("spring did not settle: %v, animating %v", v.Float(), v.Animating())
	}
}

func TestPresenceSpringExit(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	p := NewPresence(Spring(10, 0.2), Clock(c.Now))
	p.Show()
	for p.Animating() {
		c.advance(time.Second / 60)
		p.Update(p.v.frameMsg())
	}
	p.Hide()
	crossed := false
	for p.Animating() {
		c.advance(time.Second / 60)
		p.Update(p.v.frameMsg())
		crossed = crossed || p.v.Float() < 0
		if !p.Visible() && p.Animating() {
			t.Fatalf("not visible at %v while still animating out", p.v.Float())
		}
	}
	if !crossed {
		t.Fatal("the spring never overshot past hidden; the test needs less damping")
	}
	if p.Visible() {
		t.Fatal("still visible once the spring settled")
	}
}

func TestPresence(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	p := NewPresence(Duration(100*time.Millisecond), Clock(c.Now))
	if p.Visible() || p.Shown() {
		t.Fatal("a new presence is visible")
	}
	p.Show()
	if !p.Visible() || !p.Shown() || p.Progress() != 0 {
		t.Fatal("not visible once shown")
	}
	c.advance(200 * time.Millisecond)
	p.Update(p.v.frameMsg())
	if p.Progress() != 1 || p.Animating() {
		t.Fatalf("progress %v after the duration", p.Progress())
	}

	p.Toggle()
	if p.Shown() || !p.Visible() {
		t.Fatal("should stay visible while animating out")
	}
	c.advance(200 * time.Millisecond)
	p.Update(p.v.frameMsg())
	if p.Visible() {
		t.Fatal("still visible once hidden")
	}
}

func TestPresenceShowFor(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	p := NewPresence(Duration(100*time.Millisecond), Clock(c.Now))
	if p.ShowFor(time.Second) == nil || !p.Shown() {
		t.Fatal("ShowFor did not show it")
	}
	expired := hideMsg{id: p.v.id, gen: p.hide}
	if p.Update(hideMsg{id: p.v.id + 1, gen: p.hide}) != nil || !p.Shown() {
		t.Fatal("another presence's timer hid it")
	}
	p.Update(expired)
	if p.Shown() {
		t.Fatal("not hidden once its time was up")
	}

	// Showing it again cancels the timer, and ShowFor restarts it.
	p.ShowFor(time.Second)
	stale := hideMsg{id: p.v.id, gen: p.hide}
	p.Show()
	if p.Update(stale) != nil || !p.Shown() {
		t.Fatal("a cancelled timer hid it")
	}
	p.ShowFor(time.Second)
	if p.Update(stale) != nil || !p.Shown() {
		t.Fatal("an earlier ShowFor's timer hid it")
	}
}

func TestPresenceFloat(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	p := NewPresence(Spring(20, 0.2), Clock(c.Now))
	p.Show()
	peak := 0.0
	for range 30 {
		c.advance(16 * time.Millisecond)
		p.Update(p.v.frameMsg())
		peak = max(peak, p.Float())
	}
	if peak <= 1 || p.Progress() > 1 {
		t.Fatalf("peak %v, progress %v: Float should overshoot while Progress stays at 1", peak, p.Progress())
	}
}

func TestValuePlay(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	v := NewValue(1, Duration(100*time.Millisecond), Ease(Linear), Clock(c.Now))
	if v.Play(0, 1) == nil || v.Float() != 0 || !v.Animating() {
		t.Fatalf("Play(0, 1) from 1: %v, animating %v", v.Float(), v.Animating())
	}
	c.advance(50 * time.Millisecond)
	v.Update(v.frameMsg())
	if !near(v.Float(), 0.5) {
		t.Fatalf("halfway: %v", v.Float())
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	c := &clock{now: time.Unix(0, 0)}
	a := NewValue(0, Clock(c.Now))
	b := NewPresence(Clock(c.Now))
	if Update(a.frameMsg(), &a, &b) != nil {
		t.Fatal("a command with nothing animating")
	}
	a.To(1)
	b.Show()
	c.advance(10 * time.Millisecond)
	if Update(a.frameMsg(), &a, &b) == nil || a.Float() == 0 {
		t.Fatal("the value's frame did not advance it")
	}
}
