module github.com/lrstanley/x/term/still

go 1.27.2

require (
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/lrstanley/x/term/vt v0.0.0
	golang.org/x/image v0.47.0
)

replace github.com/lrstanley/x/term/vt => ../vt

require (
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
