module github.com/lrstanley/x/term/reel/_examples

go 1.27.2

// Examples live next to the reel module; test/build against the workspace copy.
replace github.com/lrstanley/x/term/reel => ../

require (
	github.com/lrstanley/clix/v2 v2.0.6
	github.com/lrstanley/x/term/reel v0.0.0
)

require (
	github.com/alecthomas/kong v1.16.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/charmbracelet/x/conpty v0.2.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/xpty v0.1.4 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/lmittmann/tint v1.2.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
