// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// StrokeKind is what a [Stroke] is.
type StrokeKind uint8

// Stroke kinds.
const (
	// StrokeKey is a key press that types no text, such as "enter" or
	// "ctrl+c".
	StrokeKey StrokeKind = iota + 1
	// StrokeText is a run of typed text.
	StrokeText
	// StrokePaste is pasted text.
	StrokePaste
	// StrokeMouse is a mouse event.
	StrokeMouse
)

// Stroke is one thing input to a terminal holds, as a viewer would name it
// (see [DecodeInput]).
type Stroke struct {
	// Kind is what the stroke is.
	Kind StrokeKind
	// Key is the key name for [StrokeKey], with modifiers first, such as
	// "enter", "ctrl+c", "alt+enter", or "shift+tab". Modifiers are ctrl,
	// alt, shift, and super, in that order; a letter is lower case, with
	// shift for upper case. A shifted symbol is named by its key in some
	// keyboard modes, such as "ctrl+shift+2" for ctrl+@.
	Key string
	// Text is the text typed for [StrokeText], or pasted for [StrokePaste].
	Text string
	// Mouse is the event for [StrokeMouse].
	Mouse MouseEvent
}

// MouseEvent is a mouse event decoded from input.
type MouseEvent struct {
	// Action is a press, release, or motion. A wheel turn is a press of a
	// wheel button.
	Action MouseAction
	// Button is the button. It is [MouseUnknown] when the encoding does
	// not say, such as for a release in the X10 encoding or motion with no
	// button held. Buttons 4 through 7 are the wheel: up, down, left, and
	// right.
	Button MouseButton
	// X and Y are the cell, from 0.
	X, Y int
}

// DecodeInput decodes data, input sent to a terminal, such as the bytes the
// Encode methods return, into the keys, text, pastes, and mouse events it
// holds, in order. Consecutive key presses that type text, including space,
// are one [StrokeText]. Input that is none of these, such as a focus report,
// is skipped. Mouse events are decoded from the X10 and SGR encodings, which
// are what programs ask for; the UTF-8, urxvt, and SGR-pixel encodings are not
// decoded correctly.
//
// The bytes alone decide the result, so typing "a" and pressing the key "a"
// decode the same, and a paste the program did not ask to bracket decodes as
// typed text and keys.
func DecodeInput(data []byte) []Stroke {
	var (
		dec uv.EventDecoder
		out []Stroke
	)
	for len(data) > 0 {
		n, ev := dec.Decode(data)
		if n <= 0 {
			break
		}
		switch ev := ev.(type) {
		case uv.PasteStartEvent:
			body := data[n:]
			end := bytes.Index(body, []byte(ansi.BracketedPasteEnd))
			if end < 0 {
				end = len(body)
				n += end
			} else {
				n += end + len(ansi.BracketedPasteEnd)
			}
			out = append(out, Stroke{Kind: StrokePaste, Text: string(body[:end])})
		case uv.KeyPressEvent:
			out = appendKey(out, uv.Key(ev))
		case uv.MouseClickEvent:
			out = appendMouse(out, MousePress, uv.Mouse(ev))
		case uv.MouseWheelEvent:
			out = appendMouse(out, MousePress, uv.Mouse(ev))
		case uv.MouseReleaseEvent:
			out = appendMouse(out, MouseRelease, uv.Mouse(ev))
		case uv.MouseMotionEvent:
			out = appendMouse(out, MouseMotion, uv.Mouse(ev))
		}
		data = data[n:]
	}
	return out
}

// appendKey adds a key press to out: to the text run at its end when the key
// types text with no modifier other than shift, or as a [StrokeKey].
func appendKey(out []Stroke, k uv.Key) []Stroke {
	if k.Text == "" || k.Mod&^(uv.ModShift|uv.ModCapsLock) != 0 {
		if k.Mod.Contains(uv.ModMeta) {
			// The terminal sends super as the xterm meta bit outside the
			// kitty protocol, and has no meta key of its own.
			k.Mod = k.Mod&^uv.ModMeta | uv.ModSuper
		}
		if k.Mod.Contains(uv.ModShift) {
			k.Code = unicode.ToLower(k.Code)
		}
		return append(out, Stroke{Kind: StrokeKey, Key: k.Keystroke()})
	}
	if n := len(out); n > 0 && out[n-1].Kind == StrokeText {
		out[n-1].Text += k.Text
		return out
	}
	return append(out, Stroke{Kind: StrokeText, Text: k.Text})
}

func appendMouse(out []Stroke, action MouseAction, m uv.Mouse) []Stroke {
	return append(out, Stroke{Kind: StrokeMouse, Mouse: MouseEvent{
		Action: action,
		Button: mouseButton(m.Button),
		X:      m.X,
		Y:      m.Y,
	}})
}

// mouseButton maps a decoded button onto the terminal's numbering, which
// swaps right and middle and otherwise matches.
func mouseButton(b ansi.MouseButton) MouseButton {
	if b == ansi.MouseMiddle {
		return MouseMiddle
	}
	if b == ansi.MouseRight {
		return MouseRight
	}
	return MouseButton(b)
}
