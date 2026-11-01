// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package play

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/cmd/reel/internal/look"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/themes"
	syntax "github.com/lrstanley/x/term/tape"
)

// VHS's defaults, which a tape that does not set them gets too, so its
// screen is the same size.
const (
	defaultWidth       = 1200
	defaultHeight      = 600
	defaultFontSize    = 22.0
	defaultPadding     = 60
	defaultTypingSpeed = 50 * time.Millisecond
	defaultWaitTimeout = 15 * time.Second
)

var defaultWaitPattern = regexp.MustCompile(`>$`)

// shell is how [Tape.Run] starts a shell for Set Shell: with no rc files
// and a "> " prompt, so the default Wait pattern finds it.
type shell struct {
	args []string
	env  []string
}

// shells are VHS's shells (MIT license, Copyright (c) 2022 Charmbracelet,
// Inc), with their prompt color.
//
//nolint:goconst // Shell names and commands.
var shells = map[string]shell{
	"bash": {
		args: []string{"bash", "--noprofile", "--norc", "--login", "+o", "history"},
		env:  []string{`PS1=\[\e[38;2;90;86;224m\]> \[\e[0m\]`, "BASH_SILENCE_DEPRECATION_WARNING=1"},
	},
	"zsh": {
		args: []string{"zsh", "--histnostore", "--no-rcs"},
		env:  []string{"PROMPT=%F{#5B56E0}> %F{reset_color}"},
	},
	"fish": {args: []string{
		"fish", "--login", "--no-config", "--private",
		"-C", "function fish_greeting; end",
		"-C", `function fish_prompt; set_color 5B56E0; echo -n "> "; set_color normal; end`,
	}},
	"powershell": {args: []string{
		"powershell", "-NoLogo", "-NoExit", "-NoProfile", "-Command",
		`Set-PSReadLineOption -HistorySaveStyle SaveNothing; function prompt { Write-Host '>' -NoNewLine -ForegroundColor Blue; return ' ' }`,
	}},
	"pwsh": {args: []string{
		"pwsh", "-Login", "-NoLogo", "-NoExit", "-NoProfile", "-Command",
		`Set-PSReadLineOption -HistorySaveStyle SaveNothing; Function prompt { Write-Host -ForegroundColor Blue -NoNewLine '>'; return ' ' }`,
	}},
	"cmd": {args: []string{"cmd.exe", "/k", "prompt=^> "}},
	"nu": {args: []string{
		"nu", "--execute", "$env.PROMPT_COMMAND = {'\x1b[;38;2;91;86;224m>\x1b[m '}; $env.PROMPT_COMMAND_RIGHT = {''}",
	}},
	"osh": {
		args: []string{"osh", "--norc"},
		env:  []string{`PS1=\[\e[38;2;90;86;224m\]> \[\e[0m\]`},
	},
	"xonsh": {args: []string{"xonsh", "--no-rc", "-D", "PROMPT=\x1b[;38;2;91;86;224m>\x1b[m "}},
}

// defaultShell is VHS's.
func defaultShell() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "bash"
}

// settings are a tape's Set values: how it looks, and how it runs. Those
// VHS has defaults for start at them (see [newSettings]).
type settings struct {
	look.Look

	shell                    string
	cols, rows               int
	width, height            int
	typingSpeed, waitTimeout time.Duration
	waitPattern              *regexp.Regexp
	// sizeSet is whether Width or Height was set, which decides how the
	// screen is sized (see [Tape.Run]).
	sizeSet bool
}

func newSettings() settings {
	return settings{
		Padding: new(defaultPadding),
		shell:   defaultShell(), width: defaultWidth, height: defaultHeight,
		typingSpeed: defaultTypingSpeed, waitTimeout: defaultWaitTimeout,
		waitPattern: defaultWaitPattern,
	}
}

// set applies "Set name value", whose value c reads, to s. It returns a
// warning for a value reel ignores or only approximates.
func (c *compiler) set(s *settings, name syntax.Token) (string, error) {
	ints := map[string]struct {
		v      *int
		lowest int
	}{
		"Columns": {&s.cols, 1}, "Rows": {&s.rows, 1}, "Width": {&s.width, 1}, "Height": {&s.height, 1},
	}
	optInts := map[string]struct {
		v      **int
		lowest int
	}{
		"Padding": {&s.Padding, 0}, "Margin": {&s.Margin, 0}, "BorderRadius": {&s.BorderRadius, 0},
		"Framerate": {&s.FPS, 1},
	}
	floats := map[string]**float64{"FontSize": &s.FontSize, "LineHeight": &s.LineHeight, "PlaybackSpeed": &s.Speed}
	durations := map[string]*time.Duration{"TypingSpeed": &s.typingSpeed, "WaitTimeout": &s.waitTimeout}
	strs := map[string]**string{"FontFamily": &s.FontFamily, "Title": &s.Title}
	flags := map[string]**bool{
		"Scrollbar": &s.Scrollbar, "PromptMarks": &s.PromptMarks, "LinkUnderline": &s.LinkUnderline,
		"Bell": &s.Bell, "KeyOverlay": &s.KeyOverlay, "Toasts": &s.Toasts,
	}
	s.sizeSet = s.sizeSet || name.Text == "Width" || name.Text == "Height"
	if n, ok := ints[name.Text]; ok {
		return "", c.intArg(name, n.lowest, n.v)
	}
	if n, ok := optInts[name.Text]; ok {
		return "", c.intArg(name, n.lowest, fresh(n.v))
	}
	if f, ok := floats[name.Text]; ok {
		return "", c.floatArg(name, false, fresh(f))
	}
	if d, ok := durations[name.Text]; ok {
		v, err := c.duration(name)
		*d = v
		return "", err
	}
	if str, ok := strs[name.Text]; ok {
		v, err := c.stringArg(name)
		*fresh(str) = v
		return "", err
	}
	if b, ok := flags[name.Text]; ok {
		v, err := c.boolArg(name)
		*fresh(b) = v
		return "", err
	}
	return c.setOther(s, name)
}

// setOther applies the settings that each read their value their own way.
func (c *compiler) setOther(s *settings, name syntax.Token) (string, error) {
	switch name.Text {
	case "Shell":
		v, err := c.stringArg(name)
		if err != nil {
			return "", err
		}
		if _, ok := shells[v]; !ok {
			return "", fmt.Errorf("unknown shell %q, want one of %s", v, strings.Join(slices.Sorted(maps.Keys(shells)), ", "))
		}
		s.shell = v
	case "LetterSpacing":
		return "", c.floatArg(name, true, fresh(&s.LetterSpacing))
	case "Theme":
		tok, ok := c.take()
		if !ok || tok.Kind != syntax.JSON && !tok.IsString() {
			return "", c.errorf(name.Pos, "Theme expects JSON or a name")
		}
		if _, err := themes.Resolve(tok.Text); err != nil {
			return "", c.errorf(tok.Pos, "%w", err)
		}
		*fresh(&s.Theme) = tok.Text
	case "MarginFill":
		v, err := c.stringArg(name)
		if err != nil {
			return "", err
		}
		if _, err = themes.ParseColor(v); err != nil {
			return "", fmt.Errorf("invalid MarginFill: %w (reel does not fill the margin with an image)", err)
		}
		*fresh(&s.MarginFill) = v
	case "WindowBar":
		v, err := c.stringArg(name)
		if err != nil {
			return "", err
		}
		*fresh(&s.WindowBar) = true
		switch v {
		case "Colorful":
		case "ColorfulRight", "Rings", "RingsRight":
			return fmt.Sprintf("WindowBar %s is drawn as Colorful", v), nil
		default:
			return "", fmt.Errorf("unknown WindowBar %q, want Colorful, ColorfulRight, Rings, or RingsRight", v)
		}
	case "WindowBarSize":
		var n int
		if err := c.intArg(name, 0, &n); err != nil {
			return "", err
		}
		return "WindowBarSize is ignored: reel sizes the window bar from the font", nil
	case "LoopOffset":
		return "", errors.New("LoopOffset is not supported")
	case "CursorBlink":
		on, err := c.boolArg(name)
		if err == nil && !on {
			return "CursorBlink false is ignored: the cursor blinks as the program sets it", nil
		}
		return "", err
	case "WaitPattern":
		tok, _ := c.take()
		if tok.Kind != syntax.Regex && !tok.IsString() {
			return "", c.errorf(name.Pos, "WaitPattern expects a regular expression")
		}
		re, err := regexp.Compile(tok.Text)
		if err != nil {
			return "", c.errorf(tok.Pos, "invalid WaitPattern: %w", err)
		}
		s.waitPattern = re
	default:
		return "", fmt.Errorf("unknown setting %q", name.Text)
	}
	return "", nil
}

// fresh points *p at a new zero value, which a setting is then read into,
// and returns it.
func fresh[T any](p **T) *T {
	*p = new(T)
	return *p
}
