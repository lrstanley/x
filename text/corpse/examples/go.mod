module github.com/lrstanley/x/text/corpse/examples

go 1.24.4

replace (
	github.com/lrstanley/x/text/corpse => ../
	github.com/lrstanley/x/text/lemm => ../../lemm
	github.com/lrstanley/x/text/stem => ../../stem
)

require (
	github.com/coder/hnsw v0.6.1
	github.com/gkampitakis/go-snaps v0.5.14
	github.com/lrstanley/x/text/corpse v0.0.0-20250929050902-72a64f8d56ef
	github.com/lrstanley/x/text/lemm v0.0.0-20250929050902-72a64f8d56ef
	github.com/lrstanley/x/text/stem v0.0.0-20250929050902-72a64f8d56ef
)

require (
	github.com/aaaton/golem/v4 v4.0.2 // indirect
	github.com/aaaton/golem/v4/dicts/en v1.0.1 // indirect
	github.com/chewxy/math32 v1.11.1 // indirect
	github.com/gkampitakis/ciinfo v0.3.3 // indirect
	github.com/gkampitakis/go-diff v1.3.2 // indirect
	github.com/goccy/go-yaml v1.18.0 // indirect
	github.com/google/renameio v1.0.1 // indirect
	github.com/kljensen/snowball v0.10.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/maruel/natural v1.1.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/viterin/partial v1.1.0 // indirect
	github.com/viterin/vek v0.4.2 // indirect
	golang.org/x/exp v0.0.0-20250819193227-8b4c13bb791b // indirect
	golang.org/x/sys v0.38.0 // indirect
)
