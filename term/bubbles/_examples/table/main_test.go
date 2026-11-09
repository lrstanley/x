// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/steep"
	"github.com/lrstanley/steep/reel"
	"github.com/lrstanley/steep/reel/effects"
	"github.com/lrstanley/steep/reel/render"
	"github.com/lrstanley/steep/reel/vt"
)

// tour returns the steep options for a w×h true color tour, recorded to
// testdata/table.webp when exporting is turned on:
//
//	STEEP_EXPORT_MODE=on go test -count 1 -run TestTour .
func tour(w, h int) []steep.Option {
	return []steep.Option{
		steep.WithWindowSize(w, h),
		steep.WithArtifacts("testdata"),
		steep.WithTimeout(20 * time.Second),
		steep.WithColor(steep.ColorTrueColor),
		steep.WithRecording(reel.Profile{
			Outputs: reel.MustFiles("table.webp"),
			Render: []render.Option{
				render.WithWindowChrome(true),
				render.WithPadding(10),
				render.WithMargin(24, lipgloss.Color("#16161e")),
				render.WithBorderRadius(10),
			},
			Effects: []reel.Effect{effects.Keys(time.Second), effects.Pause(), effects.Mouse()},
		}),
	}
}

func TestTour(t *testing.T) {
	h := steep.NewHarness(t, newModel(100_000), tour(120, 30)...)
	// The harness has no pseudo-terminal to signal a resize with, so it is
	// reported in band.
	if _, err := h.Session().Write([]byte(ansi.SetModeInBandResize)); err != nil {
		t.Fatal(err)
	}
	h.Wait("100,000 rows").Sleep(800 * time.Millisecond)

	// The list builds only the rows in sight, however far it scrolls.
	for range 4 {
		h.Key("down").Sleep(100 * time.Millisecond)
	}
	h.Key("pgdown").Sleep(400 * time.Millisecond).Key("pgdown").Sleep(400 * time.Millisecond)
	h.Key("pgup").Sleep(400 * time.Millisecond)
	h.Key("end").Wait("100,000/100,000").Sleep(700 * time.Millisecond)
	h.Key("home").Wait("1/100,000").Sleep(400 * time.Millisecond)

	// The cursor moves between cells, and space or a right click marks rows.
	h.Key("right").Sleep(150 * time.Millisecond).Key("right").Wait("job #1 · status")
	h.Key("space").Key("down").Sleep(150 * time.Millisecond)
	h.Key("space").Key("down").Sleep(150 * time.Millisecond).Key("down").Sleep(150 * time.Millisecond)
	h.Key("space").Sleep(300 * time.Millisecond)
	h.RightClick(20, 10).Sleep(250*time.Millisecond).RightClick(20, 12).Wait("5 marked").Sleep(500 * time.Millisecond)

	// Rows light up under the pointer, and a click selects a cell. The header
	// box takes rows 1 to 3, so the body starts on row 4.
	for y := 12; y <= 16; y++ {
		h.Mouse(vt.MouseMotion, vt.MouseUnknown, 20, y).Sleep(150 * time.Millisecond)
	}
	h.LeftClick(20, 16).Wait("job #13 · name").Sleep(400 * time.Millisecond)

	// The wheel moves the cursor.
	for range 3 {
		h.Mouse(vt.MousePress, vt.MouseWheelDown, 20, 10).Sleep(250 * time.Millisecond)
	}
	h.Mouse(vt.MousePress, vt.MouseWheelUp, 20, 10).Sleep(400 * time.Millisecond)

	// A click on a header sorts by it, and again the other way.
	h.Locate(reel.Contains("NAME")).LeftClick().Wait("NAME ▲").Sleep(700 * time.Millisecond)
	h.Locate(reel.Contains("CPU")).LeftClick().Wait("CPU ▲").Sleep(300 * time.Millisecond)
	h.Locate(reel.Contains("CPU ▲")).LeftClick().Wait("CPU ▼").Wait("140%").Sleep(700 * time.Millisecond)

	// The scrollbar on the border drags through all 100,000 rows.
	x := h.Locate(reel.Contains("100,000 rows ─╮")).X + 14 // the corner
	h.LeftDrag(x, 4, x, 16, reel.Delay(40*time.Millisecond)).WaitNot("1/100,000").Sleep(400 * time.Millisecond)
	h.LeftDrag(x, 16, x, 8, reel.Delay(40*time.Millisecond)).Sleep(500 * time.Millisecond)

	// Filtering, and a double click opening the dialog, which cancels the job.
	h.Key("home").Key("/").Type("fail", reel.Delay(80*time.Millisecond)).Key("enter")
	h.Wait("of 100,000 rows").Sleep(500 * time.Millisecond)
	h.LeftClick(20, 4, reel.Repeat(2)).Wait("Cancel job").Sleep(700 * time.Millisecond)
	h.Key("tab").Sleep(500 * time.Millisecond).Key("tab").Sleep(500 * time.Millisecond)
	h.Key("y").WaitNot("Cancel job").Wait("cancelled job").Sleep(500 * time.Millisecond)
	h.Key("esc").Wait("100,000 rows")

	// Every feature turns off: the header, the border, the separators, and
	// the cursor, after which the keys scroll.
	h.Key("6").Wait("header turned off").Sleep(700 * time.Millisecond)
	h.Key("7").Wait("border turned off").Sleep(700 * time.Millisecond)
	h.Key("9").Wait("separators turned off").Sleep(700 * time.Millisecond)
	h.Key("1").Wait("selection turned off").Sleep(500 * time.Millisecond)
	for range 3 {
		h.Key("down").Sleep(150 * time.Millisecond)
	}
	h.Sleep(400 * time.Millisecond)
	for _, k := range []string{"1", "9", "7", "6"} {
		h.Key(k).Sleep(250 * time.Millisecond)
	}
	h.Wait("100,000 rows").Sleep(500 * time.Millisecond)

	// Narrower: columns cut their text off, then collapse, and the details
	// pane goes first.
	h.Resize(90, 30).Sleep(700 * time.Millisecond)
	h.Resize(64, 30).Sleep(700 * time.Millisecond)
	h.Resize(120, 30).Wait("job #").Sleep(600 * time.Millisecond)
	h.AssertSnapshot()

	h.Key("q")
	h.WaitFinished()
}
