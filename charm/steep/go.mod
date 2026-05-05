module github.com/lrstanley/x/charm/steep

go 1.26.0

// TODO: https://github.com/charmbracelet/bubbletea/pull/1691
replace charm.land/bubbletea/v2 => github.com/lrstanley/bubbletea/v2 v2.0.0-20260504235939-04ceac426d4c

require (
	charm.land/bubbletea/v2 v2.0.6
	github.com/aymanbagabas/go-udiff v0.4.1
	github.com/charmbracelet/ultraviolet v0.0.0-20260422141423-a0f1f21775f7
	github.com/charmbracelet/x/ansi v0.11.7
	github.com/charmbracelet/x/vt v0.0.0-20260427100455-1ea3e7f8134f
	github.com/rivo/uniseg v0.4.7
)

require (
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/exp/golden v0.0.0-20251109135125-8916d276318f // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/exp v0.0.0-20250819193227-8b4c13bb791b // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
)
