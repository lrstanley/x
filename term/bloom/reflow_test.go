// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image"
	"testing"
	"time"

	"github.com/lrstanley/x/term/bloom/anim"
)

func requireRect(t *testing.T, r *Root, id string, want image.Rectangle) {
	t.Helper()
	got, ok := r.Frame().Rect(id)
	if !ok || got != want {
		t.Fatalf("%s rect = %v, %v; want %v", id, got, ok, want)
	}
}

func TestLayoutAnimationTween(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	opts := []anim.Option{anim.Duration(time.Second), anim.Ease(anim.Linear)}
	tree := func(offset, width int) Child {
		return Row(Fixed(offset, nil), Fixed(width, Row(Fill(Text("a")).ID("a"), Fill(Text("b")).ID("b"))).ID("pane").Animate(opts...))
	}
	r.View(100, 10, tree(0, 20))
	requireRect(t, r, "pane", image.Rect(0, 0, 20, 10))
	if r.Update("change") == nil {
		t.Fatal("animated tree did not request a discovery tick")
	}
	r.View(100, 10, tree(20, 40))
	requireRect(t, r, "pane", image.Rect(0, 0, 20, 10))
	now = now.Add(500 * time.Millisecond)
	r.View(100, 10, tree(20, 40))
	requireRect(t, r, "pane", image.Rect(10, 0, 40, 10))
	requireRect(t, r, "a", image.Rect(10, 0, 25, 10))
	requireRect(t, r, "b", image.Rect(25, 0, 40, 10))
	i, _ := r.Frame().hit(image.Pt(11, 1))
	if i < 0 || r.Frame().entries[i].id != "a" {
		t.Fatal("hit test did not use visual position")
	}
	if old, _ := r.Frame().hit(image.Pt(1, 1)); old >= 0 {
		t.Fatal("hit test used old position")
	}
	// Retarget from the last drawn rectangle without a discontinuity.
	r.View(100, 10, tree(0, 10))
	requireRect(t, r, "pane", image.Rect(10, 0, 40, 10))
	now = now.Add(500 * time.Millisecond)
	r.View(100, 10, tree(0, 10))
	requireRect(t, r, "pane", image.Rect(5, 0, 25, 10))
	now = now.Add(500 * time.Millisecond)
	r.View(100, 10, tree(0, 10))
	requireRect(t, r, "pane", image.Rect(0, 0, 10, 10))
	if r.r.motion.running {
		t.Fatal("tween never settled")
	}
}

func TestLayoutAnimationLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tree  Child
		count int
	}{
		{"plain", Fixed(10, Text("plain")).ID("plain"), 0},
		{"no id", Fixed(10, Text("anonymous")).Animate(), 0},
		{"animated", Fixed(10, Text("animated")).ID("pane").Animate(), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := New()
			r.View(80, 10, tc.tree)
			if len(r.r.motion.states) != tc.count {
				t.Fatalf("states = %d", len(r.r.motion.states))
			}
			if (r.Update("change") != nil) != (tc.count > 0) {
				t.Fatal("unexpected discovery tick")
			}
			r.View(80, 10, nil)
			if len(r.r.motion.states) != 0 {
				t.Fatal("removed node retained")
			}
		})
	}
}

func TestLayoutAnimationResizeAndZero(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	tree := func(w int) Child {
		return Row(Fixed(w, Text("pane")).ID("pane").Animate(anim.Duration(time.Second), anim.Ease(anim.Linear)), Space())
	}
	r.View(80, 10, tree(20))
	r.View(80, 10, tree(0))
	now = now.Add(500 * time.Millisecond)
	r.View(80, 10, tree(0))
	requireRect(t, r, "pane", image.Rect(0, 0, 10, 10))
	now = now.Add(500 * time.Millisecond)
	r.View(80, 10, tree(0))
	if _, ok := r.Frame().Rect("pane"); ok {
		t.Fatal("zero size still visible")
	}
	if len(r.r.motion.states) != 1 {
		t.Fatal("zero size lost animation state")
	}
	r.View(80, 10, tree(30))
	r.View(90, 10, tree(30))
	requireRect(t, r, "pane", image.Rect(0, 0, 30, 10))
	if r.r.motion.running {
		t.Fatal("resize did not stop animation")
	}
	r.View(90, 10, Row(Fixed(30, Text("pane")).ID("pane").Animate().Min(100).Collapsible(1), Fill(Text("other")).Min(80)))
	if len(r.r.motion.states) != 0 {
		t.Fatal("collapsed slot retained")
	}
	r.View(90, 10, tree(15))
	requireRect(t, r, "pane", image.Rect(0, 0, 15, 10))
}

func TestLayoutAnimationSpring(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	tree := func(x int) Child {
		return Overlay(Text("base"), Layer(Text("spring")).Size(10, 2).Anchor(Start, Start).Offset(x, 0).ID("spring").Animate(anim.Spring(15, 0.8)))
	}
	r.View(100, 10, tree(0))
	r.View(100, 10, tree(40))
	now = now.Add(100 * time.Millisecond)
	r.View(100, 10, tree(40))
	rect, _ := r.Frame().Rect("spring")
	if rect.Min.X <= 0 || rect.Min.X >= 40 {
		t.Fatalf("spring did not advance: %v", rect)
	}
	velocity := r.r.motion.states["spring"].values[0].Float()
	r.View(100, 10, tree(20))
	if got := r.r.motion.states["spring"].values[0].Float(); got != velocity {
		t.Fatal("retarget jumped")
	}
	for range 200 {
		now = now.Add(16 * time.Millisecond)
		r.View(100, 10, tree(20))
	}
	requireRect(t, r, "spring", image.Rect(20, 0, 30, 2))
	if r.r.motion.running {
		t.Fatal("spring never settled")
	}
}

func TestLayoutAnimationTicks(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }), WithFPS(1000))
	tree := func(x int) Child {
		return Row(Fixed(x, nil), Fixed(5, Text("x")).ID("x").Animate(anim.Duration(time.Second)))
	}
	r.View(80, 10, tree(0))
	cmd := r.Update("change")
	if cmd == nil {
		t.Fatal("missing discovery tick")
	}
	if r.Update("duplicate") != nil {
		t.Fatal("duplicate pending tick")
	}
	r.View(80, 10, tree(20))
	msg := cmd()
	cmd = r.Update(msg)
	if cmd == nil {
		t.Fatal("running animation stopped ticking")
	}
	now = now.Add(time.Second)
	r.View(80, 10, tree(20))
	if r.Update(cmd()) != nil {
		t.Fatal("settled animation kept ticking")
	}
	if r.Update(layoutFrameMsg{root: New()}) != nil {
		t.Fatal("accepted foreign root tick")
	}
}

func TestLayoutAnimationStateless(t *testing.T) {
	tree := Row(Fixed(20, nil), Fixed(10, Text("target")).ID("x").Animate())
	got, ok := Solve(tree, 80, 10).Rect("x")
	if !ok || got != image.Rect(20, 0, 30, 10) {
		t.Fatalf("Solve = %v, %v", got, ok)
	}
	if Render(tree, 80, 10) != Render(Row(Fixed(20, nil), Fixed(10, Text("target"))), 80, 10) {
		t.Fatal("stateless render animated")
	}
}

func TestLayoutAnimationAnchoredLayer(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	tree := func(x int) Child {
		return Overlay(Row(Fixed(x, nil), Fixed(10, Text("anchor")).ID("anchor")),
			Layer(Text("follow")).Size(6, 2).AnchorTo("anchor", Bottom).ID("follower").Animate(anim.Duration(time.Second), anim.Ease(anim.Linear)))
	}
	r.View(100, 10, tree(0))
	r.View(100, 10, tree(40))
	now = now.Add(500 * time.Millisecond)
	r.View(100, 10, tree(40))
	requireRect(t, r, "follower", image.Rect(20, 8, 26, 10))
}

func TestLayoutAnimationMovingTarget(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	tree := func(x int) Child {
		return Row(Fixed(x, nil), Fixed(5, Text("moving")).ID("moving").Animate(anim.Duration(time.Second), anim.Ease(anim.Linear)))
	}
	r.View(100, 10, tree(0))
	r.View(100, 10, tree(20))
	for i := 1; i <= 5; i++ {
		now = now.Add(100 * time.Millisecond)
		r.View(100, 10, tree(20+i))
		got, _ := r.Frame().Rect("moving")
		if got.Min.X < i {
			t.Fatalf("changing target stalled: %v", got)
		}
	}
}

func TestLayoutAnimationHeight(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	tree := func(h int) Child {
		return Column(
			Fixed(h, Column(Fill(Text("top")).ID("top"), Fill(Text("bottom")).ID("bottom"))).
				ID("pane").Animate(anim.Duration(time.Second), anim.Ease(anim.Linear)),
			Space(),
		)
	}
	r.View(40, 20, tree(4))
	r.View(40, 20, tree(12))
	now = now.Add(500 * time.Millisecond)
	r.View(40, 20, tree(12))
	requireRect(t, r, "pane", image.Rect(0, 0, 40, 8))
	requireRect(t, r, "top", image.Rect(0, 0, 40, 4))
	requireRect(t, r, "bottom", image.Rect(0, 4, 40, 8))
}

func TestLayoutAnimationIdleClock(t *testing.T) {
	r := New(WithClock(func() time.Time { t.Fatal("clock called for plain layout"); return time.Time{} }))
	for range 3 {
		r.View(40, 10, Row(Fill(Text("plain")).ID("plain"), Space()))
		if r.Update("message") != nil {
			t.Fatal("plain layout scheduled a tick")
		}
	}
}

func TestLayoutAnimationNestedMovesWithParent(t *testing.T) {
	now := time.Unix(1, 0)
	r := New(WithClock(func() time.Time { return now }))
	opts := []anim.Option{anim.Duration(time.Second), anim.Ease(anim.Linear)}
	tree := func(offset int) Child {
		return Row(Fixed(offset, nil), Fixed(20, Column(
			Fixed(1, Text("x")),
			Fixed(1, Text("item")).ID("item").Animate(anim.Duration(4*time.Second)),
		)).ID("pane").Animate(opts...))
	}
	r.View(100, 10, tree(0))
	r.View(100, 10, tree(40))
	now = now.Add(500 * time.Millisecond)
	r.View(100, 10, tree(40))
	// Halfway, the item is where it always is in the pane, rather than an
	// eighth of the way, by its own slower animation, to where it is going.
	requireRect(t, r, "pane", image.Rect(20, 0, 40, 10))
	requireRect(t, r, "item", image.Rect(20, 1, 40, 2))
}
