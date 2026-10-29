// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"cmp"
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
)

// InputOption configures one call to [Session.Type], [Session.Key],
// [Session.Click], or [Session.Drag].
type InputOption func(*inputConfig)

type inputConfig struct {
	delay  time.Duration
	repeat int
}

// Delay pauses for d after each key press, click, or drag step of the call,
// instead of the [WithTypingDelay] delay (for keys) or none (for the
// mouse). Zero or less sends the input with no pause. The pause is a
// [time.Sleep].
func Delay(d time.Duration) InputOption {
	return func(cfg *inputConfig) { cfg.delay = d }
}

// Repeat sends the call's input n times, such as a key pressed 5 times or a
// double click, each followed by the call's delay. It does not apply to
// [Session.Drag]. Less than 1 sends it once.
func Repeat(n int) InputOption {
	return func(cfg *inputConfig) { cfg.repeat = n }
}

// newInputConfig returns opts applied to a delay of d and one repeat.
func newInputConfig(d time.Duration, opts []InputOption) inputConfig {
	cfg := inputConfig{delay: d, repeat: 1}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.repeat = max(cfg.repeat, 1)
	return cfg
}

// pause sleeps for the call's delay, if any.
func (cfg inputConfig) pause() {
	if cfg.delay > 0 {
		time.Sleep(cfg.delay)
	}
}

// send sends the program encode's bytes and appends them as an [Input]
// event. Nothing happens when encode returns no bytes. With snap, a
// scrolled viewport first goes back to the bottom (unless
// [WithScrollOnInput] is off), appending a [Scroll] event.
func (s *Session) send(encode func() ([]byte, error), snap bool) error {
	return s.change(func() (Event, error) {
		data, err := encode()
		if err != nil || len(data) == 0 {
			return nil, err
		}
		if snap && s.scrollOnInput && s.term.ScrollbackOffset() != 0 {
			err = s.term.ScrollToBottom()
			if err != nil {
				return nil, err
			}
			s.timeline.append(Scroll{})
		}
		return Input{Data: data}, s.term.SendInput(data)
	})
}

// Type sends text as one key press per character. Characters are pressed as
// on a US layout, with shift for upper case and shifted symbols; others,
// including control characters such as "\n", are sent as is. For keys with
// modifiers, use [Session.Key]. A scrolled viewport goes back to the bottom
// first (see [WithScrollOnInput]). [Repeat] types text that many times.
//
// Without a delay ([WithTypingDelay], [Delay]), the text is one [Input]
// event, sent only if every key encodes. With one, each key press is its own
// [Input] event followed by the pause, and the keys before one that fails to
// encode have been sent.
func (s *Session) Type(text string, opts ...InputOption) error {
	cfg := newInputConfig(s.typingDelay, opts)
	text = strings.Repeat(text, cfg.repeat)
	if cfg.delay <= 0 {
		return s.send(func() ([]byte, error) { return s.encodeText(text) }, true)
	}
	for _, r := range text {
		if err := s.send(func() ([]byte, error) { return s.encodeText(string(r)) }, true); err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// encodeText encodes each character of text as the key press that types it.
func (s *Session) encodeText(text string) ([]byte, error) {
	var data []byte
	for _, r := range text {
		b, err := s.term.EncodeKey(vt.RuneKey(r))
		if err != nil {
			return nil, err
		}
		data = append(data, b...)
	}
	return data, nil
}

// Key sends one key press, named as [vt.ParseKey] accepts, such as "enter",
// "ctrl+c", "shift+tab", "f5", or "a". An unknown name is an error.
// [Repeat] presses it that many times, each its own [Input] event.
//
// The key is encoded per the program's keyboard mode, such as the kitty
// keyboard protocol. A combination the mode cannot encode (such as
// ctrl+shift+a without kitty) is an error and sends nothing. A scrolled
// viewport goes back to the bottom first (see [WithScrollOnInput]). Key
// pauses after each press for the [WithTypingDelay] or [Delay] delay.
func (s *Session) Key(name string, opts ...InputOption) error {
	key, err := vt.ParseKey(name)
	if err != nil {
		return err
	}
	cfg := newInputConfig(s.typingDelay, opts)
	for range cfg.repeat {
		err = s.send(func() ([]byte, error) {
			data, eerr := s.term.EncodeKey(key)
			if eerr == nil && len(data) == 0 {
				eerr = fmt.Errorf("reel: key %q has no encoding in the program's keyboard mode", name)
			}
			return data, eerr
		}, true)
		if err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// Paste sends text as a paste, bracketed when the program has enabled
// bracketed paste mode. A scrolled viewport goes back to the bottom first (see
// [WithScrollOnInput]).
func (s *Session) Paste(text string) error {
	return s.send(func() ([]byte, error) { return s.term.EncodePaste(text) }, true)
}

// Mouse sends a mouse event at zero-based cell (x, y). Nothing is sent
// unless the program enabled a mouse tracking mode that reports the event.
// Unlike keys, it leaves a scrolled viewport where it is.
func (s *Session) Mouse(action vt.MouseAction, button vt.MouseButton, x, y int) error {
	return s.send(func() ([]byte, error) {
		return s.term.EncodeMouse(vt.MouseEvent{Action: action, Button: button, X: x, Y: y})
	}, false)
}

// Click sends a press and a release of button at zero-based cell (x, y),
// as [Session.Mouse] does, then pauses for the call's [Delay]. [Repeat]
// clicks that many times, such as 2 for a double click.
func (s *Session) Click(button vt.MouseButton, x, y int, opts ...InputOption) error {
	cfg := newInputConfig(0, opts)
	for range cfg.repeat {
		if err := s.Mouse(vt.MousePress, button, x, y); err != nil {
			return err
		}
		if err := s.Mouse(vt.MouseRelease, button, x, y); err != nil {
			return err
		}
		cfg.pause()
	}
	return nil
}

// Drag presses button at cell from, moves the pointer one cell at a time
// along the straight line to to, and releases it there, as [Session.Mouse]
// does. Each step of the motion is its own [Input] event, followed by the
// call's [Delay]. Cells are not square, so the line is straight in cells,
// not on screen.
func (s *Session) Drag(button vt.MouseButton, from, to image.Point, opts ...InputOption) error {
	cfg := newInputConfig(0, opts)
	if err := s.Mouse(vt.MousePress, button, from.X, from.Y); err != nil {
		return err
	}
	for _, p := range dragPath(from, to) {
		if err := s.Mouse(vt.MouseMotion, button, p.X, p.Y); err != nil {
			return err
		}
		cfg.pause()
	}
	return s.Mouse(vt.MouseRelease, button, to.X, to.Y)
}

// dragPath returns the cells of the line from from to to, after from, by
// Bresenham's algorithm: each step moves to one of the 8 neighboring cells.
func dragPath(from, to image.Point) []image.Point {
	dx, dy := abs(to.X-from.X), -abs(to.Y-from.Y)
	sx, sy := cmp.Compare(to.X, from.X), cmp.Compare(to.Y, from.Y)
	path := make([]image.Point, 0, max(dx, -dy))
	e := dx + dy
	for p := from; p != to; {
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			p.X += sx
		}
		if e2 <= dx {
			e += dx
			p.Y += sy
		}
		path = append(path, p)
	}
	return path
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
