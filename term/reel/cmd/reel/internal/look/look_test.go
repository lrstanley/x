// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package look

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
)

func TestMerge(t *testing.T) {
	tape := Look{Title: new("tape"), FontSize: new(22.0), Bell: new(true)}
	flags := Look{Title: new("flag"), Bell: new(false)}
	got := tape.Merge(flags)
	if *got.Title != "flag" || *got.FontSize != 22 || *got.Bell {
		t.Errorf("merged = title %q, font size %v, bell %v; want the flags' over the tape's", *got.Title, *got.FontSize, *got.Bell)
	}
}

func TestValidate(t *testing.T) {
	for l, want := range map[*Look]string{
		{FontSize: new(0.0)}:      "font size must be above 0",
		{Speed: new(-1.0)}:        "speed must be above 0",
		{FPS: new(0)}:             "fps must be at least 1",
		{Padding: new(-1)}:        "padding must not be negative",
		{MarginFill: new("red")}:  "margin fill",
		{Theme: new("no-such-x")}: "unknown theme",
		{LineHeight: new(0.0)}:    "line height must be above 0",
		{Theme: new("Dracula")}:   "",
		{MarginFill: new("#fff")}: "",
	} {
		err := l.Validate()
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("Validate(%+v) = %v, want %q", *l, err, want)
		}
	}
}

func TestRender(t *testing.T) {
	l := Look{Theme: new("Dracula"), Padding: new(10), Margin: new(4), WindowBar: new(true), LetterSpacing: new(1.0), FontFamily: new("No Such Font")}
	opts, warnings, err := l.Render(18)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) == 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "No Such Font") {
		t.Errorf("Render = %d options, warnings %q", len(opts), warnings)
	}
}

func TestArrange(t *testing.T) {
	keys := effects.Keys(time.Second)
	redact := effects.Redact(effects.Text("x"))
	hide := effects.Hide(effects.HideBetween("a", "b"))
	zoom := effects.Zoom(effects.Row(0))
	got := Arrange([]reel.Effect{keys, zoom, hide, redact})
	want := []reel.Effect{redact, hide, keys, zoom}
	// Effects are not comparable, but these are each of their own type.
	if !slices.EqualFunc(got, want, func(a, b reel.Effect) bool { return fmt.Sprintf("%T", a) == fmt.Sprintf("%T", b) }) {
		t.Errorf("Arrange = %v, want concealers, then cutters, then the rest in order", got)
	}
}
