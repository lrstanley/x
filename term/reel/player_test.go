// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render"
)

func TestPlayerSeek(t *testing.T) {
	tl := testTimeline(t,
		Output{event: at(0), Data: []byte("one")},
		Mark{event: at(10), Label: "m"},
		Output{event: at(20), Data: []byte("\r\ntwo")},
		Resize{event: at(30), Cols: 40, Rows: 10},
	)
	p, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })

	text := func() string {
		var lines []string
		for _, row := range p.Screen().Cells {
			var b strings.Builder
			for _, c := range row {
				if c != nil {
					b.WriteString(c.Content)
				}
			}
			lines = append(lines, strings.TrimSpace(b.String()))
		}
		return strings.TrimRight(strings.Join(lines, "|"), "|")
	}

	for _, tc := range []struct {
		n          int
		want       string
		cols, rows int
	}{
		{n: 3, want: "one|two", cols: 80, rows: 24},
		{n: 1, want: "one", cols: 80, rows: 24},
		{n: 99, want: "one|two", cols: 40, rows: 10},
		{n: 0, want: "", cols: 80, rows: 24},
	} {
		if err = p.Seek(tc.n); err != nil {
			t.Fatal(err)
		}
		if got := text(); got != tc.want {
			t.Errorf("Seek(%d) text = %q, want %q", tc.n, got, tc.want)
		}
		scr := p.Screen()
		if scr.Cols != tc.cols || scr.Rows != tc.rows {
			t.Errorf("Seek(%d) size = %dx%d, want %dx%d", tc.n, scr.Cols, scr.Rows, tc.cols, tc.rows)
		}
	}
	if p.Pos() != 0 || p.Len() != 4 {
		t.Errorf("Pos, Len = %d, %d, want 0, 4", p.Pos(), p.Len())
	}

	// An image is kept until a Seek, so it is drawn again at another event
	// of the same size.
	if err = p.Seek(3); err != nil {
		t.Fatal(err)
	}
	img, err := p.Image()
	if err != nil {
		t.Fatal(err)
	}
	two := bytes.Clone(img.Pix)
	if again, _ := p.Image(); again != img || !bytes.Equal(again.Pix, two) {
		t.Error("Image changed without a Seek")
	}
	if err = p.Seek(1); err != nil {
		t.Fatal(err)
	}
	if img, err = p.Image(); err != nil || bytes.Equal(img.Pix, two) {
		t.Errorf("Image after Seek(1) = %v, want it drawn again", err)
	}

	if err = p.Seek(4); err != nil {
		t.Fatal(err)
	}
	img, err = p.Image()
	if err != nil {
		t.Fatal(err)
	}
	if img.Rect.Empty() {
		t.Error("Image is empty")
	}
	l, err := p.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if l.Image != img.Rect || l.Grid.Dx() != 40*l.Cell.X || l.Grid.Dy() != 10*l.Cell.Y {
		t.Errorf("Layout = %+v, want a 40x10 grid in %v", l, img.Rect)
	}

	// Window chrome adds a title bar above the grid.
	if err = p.SetOptions(render.WithWindowChrome(true)); err != nil {
		t.Fatal(err)
	}
	chrome, err := p.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if chrome.Grid.Min.Y <= l.Grid.Min.Y || chrome.Image.Dy() <= l.Image.Dy() {
		t.Errorf("Layout with chrome = %+v, want the grid lower than %+v", chrome, l)
	}
	if img, err = p.Image(); err != nil || img.Rect != chrome.Image {
		t.Errorf("Image with chrome = %v, %v, want %v", img.Rect, err, chrome.Image)
	}
}

// TestPlayerPrepare checks that a Seek back from checkpoints shows the same
// screen as one replayed from the start, and that Prepare rebuilds a
// checkpoint a Seek took over.
func TestPlayerPrepare(t *testing.T) {
	var evs []Event
	for i := range 40 {
		evs = append(evs, Output{event: at(i * 10), Data: []byte(fmt.Sprintf("\r\nline %d", i))})
	}
	tl := testTimeline(t, evs...)
	p, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	plain, err := NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })

	// A zero budget does no work, so it says there is more.
	if more, perr := p.Prepare(3, 0); perr != nil || !more {
		t.Fatalf("Prepare with no budget = %v, %v, want more", more, perr)
	}
	for more := true; more; {
		if more, err = p.Prepare(3, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(p.spares); got != 3 {
		t.Fatalf("%d checkpoints, want 3", got)
	}

	screen := func(p *Player) string {
		var b strings.Builder
		for _, row := range p.Screen().Cells {
			for _, c := range row {
				if c != nil {
					b.WriteString(c.Content)
				}
			}
		}
		return b.String()
	}
	if err = p.Seek(40); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{25, 12, 31, 3} {
		if err = p.Seek(n); err != nil {
			t.Fatal(err)
		}
		if err = plain.Seek(0); err != nil {
			t.Fatal(err)
		}
		if err = plain.Seek(n); err != nil {
			t.Fatal(err)
		}
		if got, want := screen(p), screen(plain); got != want {
			t.Errorf("Seek(%d) from a checkpoint = %q, want %q", n, got, want)
		}
	}
	if len(p.spares) == 3 {
		t.Error("Seek back took over no checkpoint")
	}
	if more, perr := p.Prepare(3, time.Second); perr != nil || more || len(p.spares) != 3 {
		t.Errorf("Prepare after seeking = %v, %v with %d checkpoints, want them rebuilt", more, perr, len(p.spares))
	}
	if _, err = p.Prepare(0, time.Second); err != nil || len(p.spares) != 0 {
		t.Errorf("Prepare(0) left %d checkpoints, %v", len(p.spares), err)
	}
}
