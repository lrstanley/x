// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"strings"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
)

// display is which parts around and over the grid frames are drawn with:
// reel's render options that show what a cast's sequences ask a terminal
// window to show, and the mouse of its input, as [effects.Mouse] draws it.
type display struct {
	Chrome    bool `json:"chrome"`
	Scrollbar bool `json:"scrollbar"`
	Progress  bool `json:"progress"`
	Status    bool `json:"status"`
	Prompts   bool `json:"prompts"`
	Links     bool `json:"links"`
	Bell      bool `json:"bell"`
	Mouse     bool `json:"mouse"`
}

// Window chrome comes with padding and rounded corners, in CSS pixels at
// the page's scale, so the frame looks like a window; without it, the
// grid's cells start at the image's edge.
const (
	chromePadding = 12
	chromeRadius  = 10
)

func (d display) options(scale float64) []render.Option {
	pad, radius := 0, 0
	if d.Chrome {
		pad, radius = int(chromePadding*scale+0.5), int(chromeRadius*scale+0.5)
	}
	return []render.Option{
		render.WithPadding(pad),
		render.WithBorderRadius(radius),
		render.WithWindowChrome(d.Chrome),
		render.WithScrollbar(d.Scrollbar),
		render.WithProgressBar(d.Progress),
		render.WithProgramStatus(d.Status),
		render.WithPromptMarks(d.Prompts),
		render.WithLinkUnderline(d.Links),
		render.WithBell(d.Bell),
	}
}

// setScale draws frames at scale from the next one, such as to match how
// large the page shows them.
func (x *inspector) setScale(scale float64) error {
	return x.configure(scale, x.display)
}

// setDisplay draws frames with d from the next one.
func (x *inspector) setDisplay(d display) error {
	return x.configure(x.scale, d)
}

// configure applies scale and display together so a frame sees one configuration.
func (x *inspector) configure(scale float64, d display) error {
	if err := x.player.SetOptions(append([]render.Option{render.WithScale(scale)}, d.options(scale)...)...); err != nil {
		return err
	}
	// The mouse is sized by the cells, which the options size.
	if err := x.startMouse(d.Mouse); err != nil {
		return err
	}
	x.scale, x.display = scale, d
	return nil
}

// detectDisplay turns on the parts the cast's sequences call for, and says
// why for each: a title or directory for the window chrome, OSC 9;4 for the
// progress bar, mouse input for the mouse, and so on. A program status is only drawn with chrome.
func (x *inspector) detectDisplay() (d display, why map[string]string) {
	why = map[string]string{}
	set := func(on *bool, key, reason string) {
		if !*on {
			*on = true
			why[key] = reason
		}
	}
	alt := false
	for _, toks := range x.tokens {
		for _, t := range toks {
			switch {
			case t.Tag == "OSC 0" || t.Tag == "OSC 2":
				set(&d.Chrome, "chrome", "sets a title ("+t.Tag+")")
			case t.Tag == "OSC 7":
				set(&d.Chrome, "chrome", "reports a working directory (OSC 7)")
			case t.Tag == "OSC 7501":
				set(&d.Status, "status", "reports program status (OSC 7501)")
				set(&d.Chrome, "chrome", "reports program status (OSC 7501), drawn in the title bar")
			case t.Tag == "OSC 9" && t.Desc == wordProgress:
				set(&d.Progress, "progress", "reports progress (OSC 9;4)")
			case t.Tag == "OSC 133":
				set(&d.Prompts, "prompts", "marks prompts (OSC 133)")
			case t.Tag == "OSC 8":
				set(&d.Links, "links", "writes hyperlinks (OSC 8)")
			case t.Tag == "BEL":
				set(&d.Bell, "bell", "rings the bell (BEL)")
			case t.Name == "DECSET" && strings.Contains(t.Desc, "alt screen"):
				alt = true
			}
		}
	}
	for _, ev := range x.events {
		switch ev.(type) {
		case reel.Scroll, reel.ScrollbackSize:
			set(&d.Scrollbar, "scrollbar", "scrolls through its scrollback")
		}
	}
	if len(x.mouseSpans) > 0 {
		set(&d.Mouse, "mouse", "sends mouse input")
	}
	// Output on the main screen that runs past it fills the scrollback.
	if !alt && bytes.Count(x.stream, []byte("\n")) > x.tl.Header().Rows {
		set(&d.Scrollbar, "scrollbar", "writes past the bottom of the screen")
	}
	return d, why
}
