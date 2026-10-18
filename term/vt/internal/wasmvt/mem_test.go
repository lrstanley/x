// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wasmvt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
)

func TestGhosttyRevMatchesBuild(t *testing.T) {
	docker, err := os.ReadFile("../../build/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(docker, []byte("GHOSTTY_REV="+GhosttyRev)) {
		t.Fatalf("GHOSTTY_REV is not %s", GhosttyRev)
	}
	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	// The constant plus the header links. A pin bump has to update both.
	if bytes.Count(doc, []byte(GhosttyRev)) < 2 {
		t.Fatal("package docs are missing GhosttyRev")
	}
}

func TestABIHashMatchesModule(t *testing.T) {
	m := New()
	ptr := m.Xghostty_type_json()
	if ptr == 0 {
		t.Fatal("null manifest")
	}
	mem := m.Memory()
	start := int(uint32(ptr))
	end := start
	for end < len(mem) && mem[end] != 0 {
		end++
	}
	sum := sha256.Sum256(mem[start:end])
	if hex.EncodeToString(sum[:]) != ABIHash {
		t.Fatal("ghostty_type_json changed; run go run ./cmd/genabi")
	}
	if SizeString != 8 || StringPtrOff != 0 || StringLenOff != 4 {
		t.Fatalf("GhosttyString layout %d %d %d", SizeString, StringPtrOff, StringLenOff)
	}
	if ResultNoValue != -4 {
		t.Fatalf("NO_VALUE %d", ResultNoValue)
	}
}

func TestMemoryRoundTrip(t *testing.T) {
	m := New()
	ptr, err := m.Alloc(8)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free(ptr, 8)

	m.SetU8(ptr, 0, 0x11)
	m.SetU16(ptr, 1, 0x2211)
	m.SetU32(ptr, 3, 0x66554433)
	m.SetI32(ptr, 3, -2)
	if m.U8(ptr, 0) != 0x11 || m.U16(ptr, 1) != 0x2211 || m.I32(ptr, 3) != -2 {
		t.Fatalf("bytes %x", m.Slice(ptr, 8))
	}
	if m.U32(ptr, 100) != 0 || m.U64(ptr, 100) != 0 {
		t.Fatal("out of range read was not zero")
	}

	m.Put(ptr, []byte("hi"))
	if got := string(m.Slice(ptr, 2)); got != "hi" {
		t.Fatalf("put %q", got)
	}
	m.Zero(ptr, 8)
	if m.U64(ptr, 0) != 0 {
		t.Fatal("zero left data")
	}

	if _, err = m.Alloc(0); err != nil {
		t.Fatal(err)
	}
	if m.Slice(0, 4) != nil {
		t.Fatal("null slice")
	}
}

func TestGhosttyStringRoundTrip(t *testing.T) {
	m := New()
	raw := []byte("hello")
	src, err := m.Alloc(len(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free(src, len(raw))
	m.Put(src, raw)

	n := SizeString
	dst, err := m.Alloc(n)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free(dst, n)
	m.StoreString(dst, 0, src, len(raw))
	if got := m.String(dst, 0); got != "hello" {
		t.Fatalf("string %q", got)
	}
	m.StoreString(dst, 0, 0, 0)
	if got := m.String(dst, 0); got != "" {
		t.Fatalf("empty %q", got)
	}
}

func TestResult(t *testing.T) {
	if err := Result("read", 0); err != nil {
		t.Fatal(err)
	}
	err := Result("read", ResultNoValue)
	if !errors.Is(err, ErrNoValue) {
		t.Fatalf("got %v", err)
	}
	if bad := Result("read", ResultInvalidValue); bad == nil || errors.Is(bad, ErrNoValue) {
		t.Fatalf("got %v", bad)
	}
}
