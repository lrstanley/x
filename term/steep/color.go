// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"slices"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// ColorProfile is the color support the harness terminal advertises to the
// program (see [WithColor]).
type ColorProfile int

const (
	// ColorASCII advertises no color support (NO_COLOR=1). Text attributes
	// such as bold still render.
	ColorASCII ColorProfile = iota + 1
	// ColorANSI advertises the 16 ANSI colors (TERM=xterm).
	ColorANSI
	// ColorANSI256 advertises 256 colors (TERM=xterm-256color). This is what
	// the harness terminal reports without [WithColor].
	ColorANSI256
	// ColorTrueColor advertises 24-bit color (TERM=xterm-256color,
	// COLORTERM=truecolor).
	ColorTrueColor
)

// String returns the profile's name.
func (p ColorProfile) String() string {
	switch p {
	case ColorASCII:
		return "ascii"
	case ColorANSI:
		return "ansi"
	case ColorANSI256:
		return "ansi256"
	case ColorTrueColor:
		return "truecolor"
	}
	return "unset"
}

// term256 is the TERM the harness terminal reports by default.
const term256 = "TERM=xterm-256color"

// colorEnvKeys are the color detection variables [WithColor] replaces.
var colorEnvKeys = []string{"TERM", "COLORTERM", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE"}

// env returns env with the color variables replaced by those that advertise p.
func (p ColorProfile) env(env []string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return slices.Contains(colorEnvKeys, key)
	})
	switch p {
	case ColorASCII:
		return append(out, term256, "NO_COLOR=1")
	case ColorANSI:
		return append(out, "TERM=xterm")
	case ColorTrueColor:
		return append(out, term256, "COLORTERM=truecolor")
	case ColorANSI256:
	}
	return append(out, term256)
}

// teaProfile is p as Bubble Tea's color profile, set explicitly so it does
// not depend on the host's terminfo database.
func (p ColorProfile) teaProfile() colorprofile.Profile {
	switch p {
	case ColorASCII:
		return colorprofile.ASCII
	case ColorANSI:
		return colorprofile.ANSI
	case ColorTrueColor:
		return colorprofile.TrueColor
	case ColorANSI256:
	}
	return colorprofile.ANSI256
}

// WithColor sets the color support the harness terminal advertises to the
// program, through TERM, COLORTERM, and NO_COLOR and [tea.WithColorProfile].
// Without it the terminal reports [ColorANSI256], so Bubble Tea downsamples
// 24-bit colors; use [ColorTrueColor] for gradients and blends.
//
// It overrides the color variables of [WithEnvVars], whatever the order. A
// [tea.WithColorProfile] passed through [WithProgramOptions] wins over it.
func WithColor(p ColorProfile) Option {
	return func(cfg *options) {
		cfg.color = p
	}
}
