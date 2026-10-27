// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import "github.com/lrstanley/x/term/reel/vt/internal/wasmvt"

// ChecksumFlags selects the XTCHECKSUM variant DECRQCRA uses.
// Zero is the DEC checksum: negate the total, add VT100 video
// attributes, trim plain spaces, and skip cells that were never written.
// Bits match xterm's checksumExtension.
type ChecksumFlags uint8

// XTCHECKSUM bits.
const (
	ChecksumPositive ChecksumFlags = 1 << iota
	ChecksumNoAttributes
	ChecksumNoTrim
	ChecksumUndrawn
	ChecksumFull
)

const checksumFlagMask ChecksumFlags = ChecksumPositive | ChecksumNoAttributes | ChecksumNoTrim | ChecksumUndrawn | ChecksumFull

// SetChecksumReport enables replies to DECRQCRA (CSI Pi ; Pg ; Pt ; Pl ; Pb ; Pr * y).
// Replies are off until this is called. The reply is a DCS sequence on
// [Terminal.Read], the same path as other pty responses.
//
// Reports can be used to read the screen one cell at a time. Enable them
// only when every program that can write to the terminal is trusted.
func (t *Terminal) SetChecksumReport(enabled bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	t.checksumReport = enabled
	return t.mod.SetBoolOption(t.term, wasmvt.TerminalOptionXtChecksumReport, enabled)
}

// SetChecksumFlags sets the current checksum variant and the variant
// restored by a full reset. Flags outside the defined bits are rejected.
// A program can still change the variant with XTCHECKSUM (CSI Ps # y)
// until the next reset.
func (t *Terminal) SetChecksumFlags(flags ChecksumFlags) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	if flags&^checksumFlagMask != 0 {
		return errInvalid("checksum flags")
	}
	t.checksumFlags = flags
	return t.mod.SetU8Option(t.term, wasmvt.TerminalOptionXtChecksumExtension, byte(flags))
}
