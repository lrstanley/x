// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestChecksumReport(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if err := term.SetChecksumFlags(ChecksumPositive); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Write([]byte("\x1b[1;1;1;1;1;2*y")); err != nil {
		t.Fatal(err)
	}
	if reply := term.replies.pending(); len(reply) != 0 {
		t.Fatalf("checksum replied while disabled: %q", reply)
	}
	if err := term.SetChecksumReport(true); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Write([]byte("\x1b[1;1;1;1;1;2*y")); err != nil {
		t.Fatal(err)
	}
	reply := readReply(t, term, 2*time.Second)
	if !strings.Contains(reply, "00C3") {
		t.Fatalf("checksum reply = %q", reply)
	}
	if err := term.SetChecksumFlags(ChecksumFlags(0x20)); err == nil {
		t.Fatal("invalid checksum flags were accepted")
	}
}

func TestChecksumReportClearedOnCheckout(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	term, err := rt.Open(context.Background(), 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = term.SetChecksumFlags(ChecksumPositive); err != nil {
		t.Fatal(err)
	}
	if err = term.SetChecksumReport(true); err != nil {
		t.Fatal(err)
	}
	if _, err = term.Write([]byte("ab\x1b[1;1;1;1;1;2*y")); err != nil {
		t.Fatal(err)
	}
	if reply := term.replies.pending(); !strings.Contains(string(reply), "00C3") {
		t.Fatalf("checksum reply = %q", reply)
	}
	if err = term.Close(); err != nil {
		t.Fatal(err)
	}

	term, err = rt.Open(context.Background(), 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if _, err = term.Write([]byte("ab\x1b[1;1;1;1;1;2*y")); err != nil {
		t.Fatal(err)
	}
	if reply := term.replies.pending(); len(reply) != 0 {
		t.Fatalf("checksum replied after checkout: %q", reply)
	}
}

func readReply(t *testing.T, term *Terminal, wait time.Duration) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := term.Read(buf)
		if err != nil {
			got <- ""
			return
		}
		got <- string(buf[:n])
	}()
	select {
	case reply := <-got:
		return reply
	case <-time.After(wait):
		return ""
	}
}
