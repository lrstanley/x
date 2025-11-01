module github.com/lrstanley/x/text/lemm

go 1.24.4

replace github.com/lrstanley/x/text/corpse => ../corpse

require (
	github.com/aaaton/golem/v4 v4.0.2
	github.com/aaaton/golem/v4/dicts/en v1.0.1
	github.com/lrstanley/x/text/corpse v0.0.0-20251025115559-2bcc06a1552e
)

require github.com/chewxy/math32 v1.11.1 // indirect
