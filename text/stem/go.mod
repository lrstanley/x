module github.com/lrstanley/x/text/stem

go 1.24.4

replace github.com/lrstanley/x/text/corpse => ../corpse

require (
	github.com/kljensen/snowball v0.10.0
	github.com/lrstanley/x/text/corpse v0.0.0-20260117234740-5feb8aa461b0
)

require github.com/chewxy/math32 v1.11.1 // indirect
