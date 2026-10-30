// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package kittycmd builds Kitty graphics protocol commands.
package kittycmd

import (
	"bytes"
	"encoding/base64"
	"slices"

	"github.com/charmbracelet/x/ansi"
)

// chunk is how many base64 bytes one command of a transmission carries, the
// most the protocol allows.
const chunk = 4096

// AppendTransmit appends to b the commands that send data with control keys
// keys: data base64-encoded, in chunks, with keys on the first.
func AppendTransmit(b *bytes.Buffer, keys []string, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for first := true; first || enc != ""; first = false {
		part := enc[:min(len(enc), chunk)]
		enc = enc[len(part):]
		more := "m=0"
		if enc != "" {
			more = "m=1"
		}
		opts := []string{more}
		if first {
			opts = append(slices.Clip(keys), more)
		}
		b.WriteString(ansi.KittyGraphics([]byte(part), opts...))
	}
}
