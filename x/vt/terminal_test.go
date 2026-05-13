// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"errors"
	"sync"
	"testing"
)

func TestNativeStructSizes(t *testing.T) {
	if sizeofGhosttyTerminalOptions != 16 {
		t.Fatalf("ghosttyTerminalOptions size got %d want 16", sizeofGhosttyTerminalOptions)
	}
	if sizeofGhosttyPoint != 24 {
		t.Fatalf("ghosttyPoint size got %d want 24", sizeofGhosttyPoint)
	}
	if sizeofGhosttyGridRef != 24 {
		t.Fatalf("ghosttyGridRef size got %d want 24", sizeofGhosttyGridRef)
	}
	if sizeofGhosttyStyle != 72 {
		t.Fatalf("ghosttyStyle size got %d want 72", sizeofGhosttyStyle)
	}
}

func TestAvailable(t *testing.T) {
	err := Available()
	if err == nil {
		return
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStyleLayoutAgainstLibrary(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("libghostty-vt not available: %v", err)
	}
	g, err := ghostLibSingleton()
	if err != nil {
		t.Fatal(err)
	}
	var st ghosttyStyle
	st.Size = uintptr(sizeofGhosttyStyle)
	if err := g.styleDefault(&st); err != nil {
		t.Fatal(err)
	}
	ok, err := g.styleIsDefault(&st)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ghostty_style_is_default returned false for default style; native layout likely wrong")
	}
}

func TestTerminalWriteAndCell(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("libghostty-vt not available: %v", err)
	}
	term, err := Open(Options{Width: 20, Height: 5, MaxScrollback: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	if _, err := term.WriteString("hello"); err != nil {
		t.Fatal(err)
	}
	c := term.CellAt(0, 0)
	if c == nil {
		t.Fatal("expected cell at origin")
	}
	if c.Content != "h" {
		t.Fatalf("content got %q want h", c.Content)
	}
	if c.Width != 1 {
		t.Fatalf("width got %d want 1", c.Width)
	}
}

func TestTerminalConcurrentUse(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("libghostty-vt not available: %v", err)
	}
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			term, err := Open(Options{Width: 12, Height: 4, MaxScrollback: 32})
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = term.Close() }()
			for j := 0; j < 50; j++ {
				if _, err := term.Write([]byte("a")); err != nil {
					errs <- err
					return
				}
				_ = term.Width()
				_ = term.Height()
				_ = term.CellAt(0, 0)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTerminalResize(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("libghostty-vt not available: %v", err)
	}
	term, err := Open(Options{Width: 10, Height: 3, MaxScrollback: 10})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	if err := term.Resize(30, 12); err != nil {
		t.Fatal(err)
	}
	if term.Width() != 30 || term.Height() != 12 {
		t.Fatalf("size got %dx%d want 30x12", term.Width(), term.Height())
	}
}
