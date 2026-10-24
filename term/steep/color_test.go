// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestWithColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		opts []Option
		want colorprofile.Profile
	}{
		{nil, colorprofile.ANSI256},
		{[]Option{WithColor(ColorASCII)}, colorprofile.ASCII},
		{[]Option{WithColor(ColorANSI)}, colorprofile.ANSI},
		{[]Option{WithColor(ColorANSI256)}, colorprofile.ANSI256},
		{[]Option{WithColor(ColorTrueColor)}, colorprofile.TrueColor},
		// WithColor applies after WithEnvVars, whatever the order.
		{[]Option{WithColor(ColorTrueColor), WithEnvVars("TERM=dumb", "NO_COLOR=1")}, colorprofile.TrueColor},
		// An explicit program option wins.
		{[]Option{WithColor(ColorTrueColor), WithProgramOptions(tea.WithColorProfile(colorprofile.ANSI))}, colorprofile.ANSI},
	}
	for _, tt := range tests {
		h := NewHarness(t, terminalProbe{}, append(tt.opts, WithIgnoreEnv(true))...)
		got := h.WaitMessage[tea.ColorProfileMsg]()
		if got.Profile != tt.want {
			t.Errorf("WithColor options %d: program detected %v, want %v", len(tt.opts), got.Profile, tt.want)
		}
	}
}

func TestColorEnv(t *testing.T) {
	t.Parallel()

	env := []string{"TTY_FORCE=1", "TERM=xterm-256color", "NO_COLOR=1", "HOME=/tmp"}
	got := ColorTrueColor.env(env)
	want := []string{"TTY_FORCE=1", "HOME=/tmp", "TERM=xterm-256color", "COLORTERM=truecolor"}
	if !slices.Equal(got, want) {
		t.Fatalf("env = %q, want %q", got, want)
	}
}

// The explicit Bubble Tea profile must agree with what detection finds from
// the environment alone, which other programs reading it would see.
func TestColorProfileEnvMatchesDetection(t *testing.T) {
	t.Parallel()

	for _, p := range []ColorProfile{ColorASCII, ColorANSI, ColorANSI256, ColorTrueColor} {
		if got := colorprofile.Env(p.env([]string{"TTY_FORCE=1"})); got != p.teaProfile() {
			t.Errorf("%v: environment detects %v, want %v", p, got, p.teaProfile())
		}
	}
}
