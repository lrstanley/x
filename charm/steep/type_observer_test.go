// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"strings"
	"testing"
	"time"
)

type (
	typeObsA struct{ n int }
	typeObsB struct{ s string }
)

func TestNewTypeObserver_Empty(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[typeObsA]()
	if len(o.sortedLastReceived()) != 0 {
		t.Fatalf("sortedLastReceived = %d entries, want 0", len(o.sortedLastReceived()))
	}
	if s := o.String(); s != "observed types: <none>" {
		t.Fatalf("String() = %q, want observed types: <none>", s)
	}
}

func TestTypeObserver_Observe_CountsAndLastReceived(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[any]()
	o.observe("a", "b", "c")

	types := o.types
	if len(types) != 1 {
		t.Fatalf("unique types = %d, want 1", len(types))
	}
	obs := types["string"]
	if obs == nil {
		t.Fatal("missing string type entry")
	}
	if obs.count != 3 {
		t.Fatalf("count = %d, want 3", obs.count)
	}
	if obs.rtype.String() != "string" {
		t.Fatalf("rtype = %q, want string", obs.rtype.String())
	}
	if obs.lastReceived.IsZero() {
		t.Fatal("lastReceived should be set")
	}
}

func TestTypeObserver_Observe_Chaining(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[typeObsA]()
	_ = o.observe(typeObsA{n: 1}).observe(typeObsA{n: 2})
	if len(o.types) != 1 {
		t.Fatalf("types = %d, want 1", len(o.types))
	}
	if o.types["steep.typeObsA"].count != 2 {
		t.Fatalf("count = %d, want 2", o.types["steep.typeObsA"].count)
	}
}

func TestTypeObserver_Observe_MultipleConcreteTypes(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[any]()
	o.observe(typeObsA{n: 1}, typeObsB{s: "x"}, typeObsA{n: 2})

	if len(o.types) != 2 {
		t.Fatalf("unique types = %d, want 2", len(o.types))
	}
	if o.types["steep.typeObsA"].count != 2 {
		t.Fatalf("typeObsA count = %d, want 2", o.types["steep.typeObsA"].count)
	}
	if o.types["steep.typeObsB"].count != 1 {
		t.Fatalf("typeObsB count = %d, want 1", o.types["steep.typeObsB"].count)
	}
}

func TestTypeObserver_SortedLastReceived_NewestFirst(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[any]()

	o.observe(typeObsA{n: 1})
	time.Sleep(5 * time.Millisecond)
	o.observe(typeObsB{s: "x"})
	time.Sleep(5 * time.Millisecond)
	o.observe(typeObsA{n: 2})

	got := o.sortedLastReceived()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].rtype.String() != "steep.typeObsA" {
		t.Fatalf("newest type = %s, want steep.typeObsA (touched last)", got[0].rtype.String())
	}
	if got[1].rtype.String() != "steep.typeObsB" {
		t.Fatalf("older type = %s, want steep.typeObsB", got[1].rtype.String())
	}
	if !got[0].lastReceived.After(got[1].lastReceived) {
		t.Fatalf("want typeObsA lastReceived after typeObsB, got %v vs %v", got[0].lastReceived, got[1].lastReceived)
	}
}

func TestTypeObserver_String_IncludesRelativeFirstAndLast(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[any]()
	o.observe(42)

	s := o.String()
	if !strings.HasPrefix(s, "observed types:\n") {
		t.Fatalf("String() missing header: %q", s)
	}
	if !strings.Contains(s, `"int" x1 -- first:`) {
		t.Fatalf("String() missing int line: %q", s)
	}
	if !strings.Contains(s, "ago") {
		t.Fatalf("String() missing relative duration: %q", s)
	}
	if strings.Contains(s, ", last:") {
		t.Fatalf("single observe should omit last when first == last: %q", s)
	}

	o.observe(7)
	s = o.String()
	if !strings.Contains(s, `"int" x2 -- first:`) {
		t.Fatalf("String() missing int line after second observe: %q", s)
	}
	if !strings.Contains(s, ", last:") {
		t.Fatalf("expected ', last:' when first != last: %q", s)
	}
}

func TestTypeObserver_ConcurrentObserve(t *testing.T) {
	t.Parallel()
	o := newTypeObserver[int]()
	const n = 100
	done := make(chan struct{})
	for i := range n {
		go func(v int) {
			defer func() { done <- struct{}{} }()
			o.observe(v)
		}(i)
	}
	for range n {
		<-done
	}
	types := o.types
	if len(types) != 1 {
		t.Fatalf("unique types = %d, want 1 (all int)", len(types))
	}
	if types["int"].count != n {
		t.Fatalf("count = %d, want %d", types["int"].count, n)
	}
}
