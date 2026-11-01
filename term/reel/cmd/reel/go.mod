module github.com/lrstanley/x/term/reel/cmd/reel

go 1.27.2

// The CLI lives next to reel and tape; build against the workspace copies.
replace (
	github.com/lrstanley/x/term/reel => ../..
	github.com/lrstanley/x/term/tape => ../../../tape
)

require (
	charm.land/lipgloss/v2 v2.0.6
	github.com/alecthomas/kong v1.16.1
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/charmbracelet/x/term v0.2.2
	github.com/lrstanley/bubbletint/v2 v2.0.4
	github.com/lrstanley/clix/v2 v2.0.7
	github.com/lrstanley/x/term/reel v0.0.0
	github.com/lrstanley/x/term/tape v0.0.0
)

require (
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260811164956-006e29f97886 // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/charmbracelet/x/xpty v0.1.4 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/lmittmann/tint v1.2.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
