module github.com/lrstanley/x/term/steep/_examples

go 1.27.2

// Examples live next to the steep module; test/build against the workspace copy.
replace (
	github.com/lrstanley/x/term/reel => ../../reel
	github.com/lrstanley/x/term/steep => ../
)

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.0
	charm.land/lipgloss/v2 v2.0.6
	github.com/charmbracelet/harmonica v0.2.0
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/lrstanley/go-nf v0.0.0-20261018042953-dc908cf1060f
	github.com/lrstanley/x/term/reel v0.0.0
	github.com/lrstanley/x/term/steep v0.0.0-20260510074740-f99adc613ee2
)

require (
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4 // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/charmbracelet/x/xpty v0.1.4 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
