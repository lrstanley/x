// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"errors"
	"image/color"
	"io"
	"testing"
)

func TestEncodeKeyFollowsKeyboardMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode string
		key  Key
		want string
	}{
		{"legacy text", "", Key{Code: "A", Text: "a"}, "a"},
		{"legacy ctrl", "", Key{Code: "C", Mods: ModCtrl}, "\x03"},
		{"legacy alt", "", Key{Code: "A", Mods: ModAlt}, "\x1ba"},
		{"legacy shift tab", "", Key{Code: "TAB", Mods: ModShift}, "\x1b[Z"},
		{"kitty ctrl", "\x1b[>1u", Key{Code: "C", Mods: ModCtrl}, "\x1b[99;5u"},
		{"kitty alt", "\x1b[>1u", Key{Code: "A", Mods: ModAlt}, "\x1b[97;3u"},
		{"kitty all keys", "\x1b[>31u", Key{Code: "A", Text: "a"}, "\x1b[97;;97u"},
		{"kitty all keys shifted", "\x1b[>31u", Key{Code: "A", Text: "A", Mods: ModShift}, "\x1b[97:65;2;65u"},
		{"kitty all keys digit", "\x1b[>31u", Key{Code: "DIGIT_1", Text: "1"}, "\x1b[49;;49u"},
		{"modify other keys", "\x1b[>4;2m", Key{Code: "A", Text: "A", Mods: ModShift}, "\x1b[27;2;65~"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			term := openTest(t, 8, 2)
			if _, err := term.Write([]byte(tt.mode)); err != nil {
				t.Fatal(err)
			}
			got, err := term.EncodeKey(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("EncodeKey(%+v) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestEncodeDoesNotSend(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("\x1b[?1000h\x1b[?1006h\x1b[?1004h\x1b[?2004h")); err != nil {
		t.Fatal(err)
	}
	encoders := map[string]func() ([]byte, error){
		"key":   func() ([]byte, error) { return term.EncodeKey(Key{Code: "ENTER"}) },
		"mouse": func() ([]byte, error) { return term.EncodeMouse(MousePress, MouseLeft, 3, 1) },
		"paste": func() ([]byte, error) { return term.EncodePaste("hi") },
		"focus": func() ([]byte, error) { return term.EncodeFocus(true) },
	}
	want := map[string]string{
		"key":   "\r",
		"mouse": "\x1b[<0;4;2M",
		"paste": "\x1b[200~hi\x1b[201~",
		"focus": "\x1b[I",
	}
	for name, encode := range encoders {
		got, err := encode()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want[name] {
			t.Fatalf("%s = %q, want %q", name, got, want[name])
		}
	}
	if pending := term.replies.pending(); len(pending) != 0 {
		t.Fatalf("encoders queued %q", pending)
	}

	if err := term.SendInput([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	n, err := term.Read(buf)
	if err != nil || string(buf[:n]) != "ab" {
		t.Fatalf("Read = %q, %v, want \"ab\"", buf[:n], err)
	}
}

func TestEncodeNothingWithoutMode(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if got, err := term.EncodeMouse(MousePress, MouseLeft, 0, 0); err != nil || got != nil {
		t.Fatalf("EncodeMouse without tracking = %q, %v, want nil", got, err)
	}
	if got, err := term.EncodeFocus(true); err != nil || got != nil {
		t.Fatalf("EncodeFocus without mode 1004 = %q, %v, want nil", got, err)
	}
}

func TestSendInputAfterCloseReader(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	term.CloseReader()
	if err := term.SendInput([]byte("x")); err == nil {
		t.Fatal("SendInput after CloseReader succeeded")
	}
}

func TestRuneKey(t *testing.T) {
	t.Parallel()
	tests := map[rune]Key{
		'a':  {Code: "A", Text: "a"},
		'A':  {Code: "A", Text: "A", Mods: ModShift},
		'1':  {Code: "DIGIT_1", Text: "1"},
		'!':  {Code: "DIGIT_1", Text: "!", Mods: ModShift},
		' ':  {Code: "SPACE", Text: " "},
		'?':  {Code: "SLASH", Text: "?", Mods: ModShift},
		'é':  {Text: "é"},
		'\n': {Text: "\n"},
	}
	for r, want := range tests {
		if got := RuneKey(r); got != want {
			t.Errorf("RuneKey(%q) = %+v, want %+v", r, got, want)
		}
	}
}

func TestEncodeAfterClose(t *testing.T) {
	t.Parallel()
	term, err := NewRuntime(1).Open(context.Background(), 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = term.Close()
	if _, err = term.EncodeKey(Key{Code: "ENTER"}); err == nil {
		t.Fatal("EncodeKey after Close succeeded")
	}
	if err = term.SendInput([]byte("x")); err == nil {
		t.Fatal("SendInput after Close succeeded")
	}
	if _, err = term.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("Read after Close = %v, want EOF", err)
	}
}

func TestHostColorsClearedOnCheckout(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	ctx := context.Background()
	red := color.NRGBA{R: 0xff, A: 0xff}
	first, err := rt.Open(ctx, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fg, bg, cur := first.HostColors(); fg != nil || bg != nil || cur != nil {
		t.Fatalf("new terminal HostColors = %v, %v, %v, want nils", fg, bg, cur)
	}
	if err = first.SetDefaultBackground(red); err != nil {
		t.Fatal(err)
	}
	if err = first.SetForeground(color.Gray{Y: 0x80}); err != nil {
		t.Fatal(err)
	}
	// Program-set colors are not the next checkout's either.
	if _, err = first.Write([]byte("\x1b]10;#123456\x07\x1b]11;#654321\x07\x1b]12;#abcdef\x07")); err != nil {
		t.Fatal(err)
	}
	fg, bg, _ := first.HostColors()
	if fg != (color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}) || bg != red {
		t.Fatalf("HostColors = %v, %v, want gray and red", fg, bg)
	}
	_ = first.Close()

	second, err := rt.Open(ctx, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if fg2, bg2, cur := second.HostColors(); fg2 != nil || bg2 != nil || cur != nil {
		t.Fatalf("reused terminal HostColors = %v, %v, %v, want nils", fg2, bg2, cur)
	}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if fg, bg := second.Foreground(), second.Background(); fg != white || bg != (color.NRGBA{A: 0xff}) {
		t.Fatalf("reused terminal colors = %v on %v, want white on black", fg, bg)
	}
	if c, ok := second.CursorColor(); ok {
		t.Fatalf("reused terminal cursor color = %v, want none", c)
	}
}
