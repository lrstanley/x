// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"iter"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	// Key is the key name for [StrokeKey], as [Key.String] names it, such as
	// "enter", "ctrl+c", "alt+enter", or "shift+tab".
	Key string
	// Text is the text typed for [StrokeText], or pasted for [StrokePaste].
	Text string
	// Mouse is the event for [StrokeMouse].
	Mouse MouseEvent
}

// DecodeInput decodes data, input for a program as the Encode methods of
// [Terminal] or a real terminal produce it, into the keys, text, pastes, and
// mouse events it holds, in order. Consecutive key presses that type text
// with no modifier other than shift, including space, are one [StrokeText].
//
// It reads the encodings other terminals send too: legacy and xterm keys,
// with modifyOtherKeys and urxvt's variants; the kitty keyboard protocol;
// X10, urxvt, and SGR mouse reports; and bracketed pastes. A key is decoded
// as the key that types it on a US layout, so the keypad's keys decode as
// the keys they match.
//
// Input that is none of these, such as focus reports, replies to queries,
// key releases, and keys the terminal has no code for, is skipped. The bytes
// alone decide the result, so typing "a" and pressing the key "a" decode the
// same, and a paste the program did not ask to bracket decodes as typed
// text and keys. A cursor position report on the first row looks like F3
// with modifiers, and is skipped. Mouse reports carry no modifiers, and the
// UTF-8 and SGR-pixel mouse encodings are not decoded correctly.
func DecodeInput(data []byte) []Stroke {
	var (
		out  []Stroke
		text strings.Builder
	)
	flush := func() {
		if text.Len() > 0 {
			out = append(out, Stroke{Kind: StrokeText, Text: text.String()})
			text.Reset()
		}
	}
	for ev := range decodeEvents(data) {
		if k, ok := ev.(Key); ok && k.Text != "" && k.Mods&^ModShift == 0 {
			text.WriteString(k.Text)
			continue
		}
		flush()
		switch ev := ev.(type) {
		case Key:
			out = append(out, Stroke{Kind: StrokeKey, Key: ev.String()})
		case MouseEvent:
			out = append(out, Stroke{Kind: StrokeMouse, Mouse: ev})
		case paste:
			out = append(out, Stroke{Kind: StrokePaste, Text: string(ev)})
		}
	}
	flush()
	return out
}

// inputEvent is one thing [decodeEvents] finds in input: a [Key],
// [MouseEvent], or paste.
type inputEvent interface{ isInputEvent() }

// paste is pasted text, from a bracketed paste.
type paste string

func (Key) isInputEvent()        {}
func (MouseEvent) isInputEvent() {}
func (paste) isInputEvent()      {}

// decodeEvents decodes data into the key presses, mouse events, and pastes
// it holds, in order, as [DecodeInput] describes.
func decodeEvents(data []byte) iter.Seq[inputEvent] {
	return func(yield func(inputEvent) bool) {
		for b := data; len(b) > 0; {
			// Every decoder takes at least one byte.
			n, ev := decodeEvent(b)
			b = b[n:]
			if ev != nil && !yield(ev) {
				return
			}
		}
	}
}

// decodeEvent decodes the event at the start of b, and returns how many
// bytes it took. The event is nil for input that is skipped.
func decodeEvent(b []byte) (int, inputEvent) {
	switch c := b[0]; {
	case c == ansi.ESC:
		return decodeEsc(b)
	case c == ansi.SS3:
		return decodeSS3(b, 1)
	case c < ansi.SP || c == ansi.DEL:
		return 1, controlKey(c)
	}
	return decodeSeq(b)
}

// decodeEsc decodes input starting with ESC: a sequence, alt with a key, or
// the escape key.
func decodeEsc(b []byte) (int, inputEvent) {
	if len(b) == 1 {
		return 1, Key{Code: "ESCAPE"}
	}
	switch b[1] {
	case '[', 'O', 'P', ']', 'X', '^', '_':
		if len(b) == 2 {
			// A sequence introducer with nothing after it is alt with the
			// introducer's key.
			return 2, runeKey(rune(b[1]), ModAlt)
		}
		if b[1] == 'O' {
			return decodeSS3(b, 2)
		}
		return decodeSeq(b)
	case ansi.ESC:
		// Some terminals send alt with a CSI or SS3 key as ESC before it;
		// otherwise a second ESC is alt+esc.
		if len(b) == 2 || b[2] != '[' && b[2] != 'O' {
			return 2, Key{Code: "ESCAPE", Mods: ModAlt}
		}
	}
	n, ev := decodeEvent(b[1:])
	if k, ok := ev.(Key); ok {
		return n + 1, k.with(ModAlt)
	}
	return 1, Key{Code: "ESCAPE"}
}

// decodeSeq decodes the escape sequence or text at the start of b.
func decodeSeq(b []byte) (int, inputEvent) {
	seq, _, n, _ := ansi.DecodeSequence(b, ansi.NormalState, nil)
	switch {
	case ansi.HasCsiPrefix(seq):
		return decodeCSI(b, n)
	case seq[0] == ansi.ESC:
		// An OSC, DCS, APC, PM, or SOS string.
		return n, nil
	}
	r, size := utf8.DecodeRune(seq)
	switch {
	case r == utf8.RuneError && size <= 1:
		// A C1 control or string, or a byte that does not start UTF-8.
		return n, nil
	case size < len(seq):
		// A grapheme of more than one rune, which no key types.
		return n, Key{Text: string(seq)}
	}
	return n, RuneKey(r)
}

// decodeCSI decodes the CSI sequence of n bytes at the start of b, with the
// bytes after it that X10 mouse reports and pastes take.
func decodeCSI(b []byte, n int) (int, inputEvent) {
	c := parseCSI(b[:n])
	switch {
	case c.inter == '$' && c.prefix == 0 && len(c.params) == 1:
		// urxvt ends a key with shift held with "$", so what follows is not
		// part of the sequence.
		end := bytes.IndexByte(b[:n], '$') + 1
		if code, ok := tildeKeys[c.param(0, 0, -1)]; ok {
			return end, Key{Code: code, Mods: ModShift}
		}
		return end, nil
	case c.final == 'M' && c.prefix == 0 && c.inter == 0 && len(c.params) == 0:
		// An X10 mouse report: three bytes, each offset by 32.
		if len(b) < n+3 {
			return len(b), nil
		}
		v := b[n : n+3]
		return n + 3, mouseEvent(int(v[0])-32, int(v[1])-32, int(v[2])-32, false)
	case c.final == '~' && c.prefix == 0 && c.param(0, 0, 0) == 200:
		body := b[n:]
		end := bytes.Index(body, []byte(ansi.BracketedPasteEnd))
		if end < 0 {
			return len(b), paste(body)
		}
		return n + end + len(ansi.BracketedPasteEnd), paste(body[:end])
	}
	return n, c.event()
}

// decodeSS3 decodes the SS3 key at the start of b, whose introducer is i
// bytes. Some terminals put a modifier parameter before the key.
func decodeSS3(b []byte, i int) (int, inputEvent) {
	param := 0
	for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
		param = min(param*10+int(b[i]-'0'), 1<<16)
	}
	if i >= len(b) || b[i] < '!' || b[i] > '~' {
		return i, nil
	}
	key, ok := ss3Keys[b[i]]
	if !ok {
		return i + 1, nil
	}
	return i + 1, key.with(wireMods(param))
}

// csiSeq is a CSI sequence split into its parts. Each parameter holds its
// colon-separated sub-parameters, -1 where one is missing. final is 0 for a
// sequence cut off before its final byte.
type csiSeq struct {
	prefix, inter, final byte
	params               [][]int
}

// parseCSI splits seq, a CSI sequence as [ansi.DecodeSequence] finds it,
// into its parts.
func parseCSI(seq []byte) csiSeq {
	var c csiSeq
	if seq[0] == ansi.ESC {
		seq = seq[2:]
	} else {
		seq = seq[1:]
	}
	if len(seq) > 0 && seq[0] >= '<' && seq[0] <= '?' {
		c.prefix, seq = seq[0], seq[1:]
	}
	i := 0
	if len(seq) > 0 && seq[0] >= '0' && seq[0] <= ';' {
		param := []int{-1}
		for ; i < len(seq) && seq[i] >= '0' && seq[i] <= ';'; i++ {
			switch d := seq[i]; d {
			case ':':
				param = append(param, -1)
			case ';':
				c.params = append(c.params, param)
				param = []int{-1}
			default:
				v := &param[len(param)-1]
				*v = min(max(*v, 0)*10+int(d-'0'), 1<<24)
			}
		}
		c.params = append(c.params, param)
	}
	for ; i < len(seq) && seq[i] >= ' ' && seq[i] <= '/'; i++ {
		c.inter = seq[i]
	}
	if i < len(seq) && seq[i] >= '@' && seq[i] <= '~' {
		c.final = seq[i]
	}
	return c
}

// param returns sub-parameter sub of parameter i, or def when it is missing.
func (c csiSeq) param(i, sub, def int) int {
	if i >= len(c.params) || sub >= len(c.params[i]) || c.params[i][sub] < 0 {
		return def
	}
	return c.params[i][sub]
}

// event decodes the sequence, other than an X10 mouse report or a paste.
func (c csiSeq) event() inputEvent {
	switch {
	case c.prefix == '<':
		if (c.final == 'M' || c.final == 'm') && len(c.params) == 3 {
			return mouseEvent(c.param(0, 0, 0), c.param(1, 0, 1), c.param(2, 0, 1), c.final == 'm')
		}
		return nil
	case c.prefix != 0 || c.inter != 0 || c.param(1, 1, 1) == 3:
		// A reply to a query, or a key release in the kitty protocol.
		return nil
	}
	mods := wireMods(c.param(1, 0, 1))
	switch c.final {
	case 'u':
		return c.kittyKey(mods)
	case '~', '^', '@':
		return c.tildeKey(mods)
	case 'M':
		if len(c.params) == 3 {
			// A urxvt mouse report: the button offset by 32, and the cell.
			return mouseEvent(c.param(0, 0, 32)-32, c.param(1, 0, 1), c.param(2, 0, 1), false)
		}
	case 'R':
		if len(c.params) > 0 {
			return nil
		}
	}
	key, ok := csiKeys[c.final]
	if !ok || c.param(0, 0, 1) != 1 {
		return nil
	}
	return key.with(mods)
}

// kittyKey decodes a kitty keyboard protocol key: its code, the shifted
// code when the program asked for it, and the text it types when the
// program asked for that.
func (c csiSeq) kittyKey(mods Mods) inputEvent {
	code := c.param(0, 0, -1)
	if shifted := c.param(0, 1, -1); shifted > 0 && mods&ModShift != 0 {
		code = shifted
	}
	key, ok := codeKey(code, mods)
	if !ok {
		return nil
	}
	if len(c.params) > 2 && key.Text != "" {
		var text []rune
		for _, r := range c.params[2] {
			if r > 0 && utf8.ValidRune(rune(r)) {
				text = append(text, rune(r))
			}
		}
		if len(text) > 0 {
			key.Text = string(text)
		}
	}
	return key
}

// tildeKey decodes a key ending in "~", or in urxvt's "^" for ctrl or "@"
// for ctrl and shift, including xterm's modifyOtherKeys form.
func (c csiSeq) tildeKey(mods Mods) inputEvent {
	n := c.param(0, 0, -1)
	switch c.final {
	case '~':
		if n == 27 && len(c.params) == 3 {
			key, ok := codeKey(c.param(2, 0, -1), mods)
			if !ok {
				return nil
			}
			return key
		}
	case '^':
		mods |= ModCtrl
	case '@':
		mods |= ModCtrl | ModShift
	}
	code, ok := tildeKeys[n]
	if !ok {
		return nil
	}
	return Key{Code: code}.with(mods)
}

// codeKey is the key a kitty or modifyOtherKeys key code stands for, with
// mods held: a key in [kittyCodes], a control character, or the character the
// code is.
func codeKey(code int, mods Mods) (Key, bool) {
	if key, ok := kittyCodes[code]; ok {
		return key.with(mods), true
	}
	r := rune(code)
	switch {
	case code >= 0 && code < ansi.SP:
		return controlKey(byte(code)).with(mods), true
	case utf8.ValidRune(r) && unicode.IsGraphic(r):
		return runeKey(r, mods), true
	}
	return Key{}, false
}

// controlKey is the key a control character stands for: ctrl with a letter
// or symbol, or a key such as enter or tab. DEL is backspace and BS is
// ctrl+h, as terminals send them by default.
func controlKey(c byte) Key {
	switch c {
	case ansi.NUL:
		return Key{Code: "SPACE", Mods: ModCtrl}
	case ansi.HT:
		return Key{Code: "TAB"}
	case ansi.CR:
		return Key{Code: "ENTER"}
	case ansi.ESC:
		return Key{Code: "ESCAPE"}
	case ansi.DEL:
		return Key{Code: "BACKSPACE"}
	case ansi.FS, ansi.GS, ansi.RS, ansi.US:
		return runeKey(rune(c)+'@', ModCtrl)
	}
	return runeKey(rune(c)+'`', ModCtrl)
}

// wireMods returns the modifiers of an xterm or kitty modifier parameter,
// which is one more than a bitmask of shift (1), alt (2), ctrl (4), and
// super (8). xterm calls 8 meta, and the terminal sends super as it outside
// the kitty protocol. Kitty's hyper, meta, and lock bits are dropped.
func wireMods(param int) Mods {
	bits := param - 1
	if bits <= 0 {
		return 0
	}
	var mods Mods
	for i, mod := range [...]Mods{ModShift, ModAlt, ModCtrl, ModSuper} {
		if bits&(1<<i) != 0 {
			mods |= mod
		}
	}
	return mods
}

// mouseEvent decodes the button byte of a mouse report at cell (x, y),
// counted from 1. release is set by an SGR report ending in "m"; the X10 and
// urxvt encodings say a button was released, but not which one.
func mouseEvent(b, x, y int, release bool) MouseEvent {
	const (
		motion = 1 << 5
		wheel  = 1 << 6
		extra  = 1 << 7
	)
	b = max(b, 0)
	m := MouseEvent{Action: MousePress, X: max(x-1, 0), Y: max(y-1, 0)}
	low := b & 0b11
	switch {
	case b&wheel != 0:
		// A wheel turn is a press, never motion or a release.
		m.Button = MouseWheelUp + MouseButton(low)
		return m
	case b&extra != 0:
		m.Button = MouseBackward + MouseButton(low)
	case low == 0b11:
		// A release, or motion with no button held.
		m.Button = MouseUnknown
		release = true
	default:
		m.Button = [...]MouseButton{MouseLeft, MouseMiddle, MouseRight}[low]
	}
	switch {
	case b&motion != 0:
		m.Action = MouseMotion
	case release:
		m.Action = MouseRelease
	}
	return m
}

// csiKeys and ss3Keys map the final byte of CSI and SS3 keys to the keys.
var csiKeys, ss3Keys = func() (csi, ss3 map[byte]Key) {
	csi = map[byte]Key{
		'Z': {Code: "TAB", Mods: ModShift},
		// urxvt's arrows with shift held.
		'a': {Code: "ARROW_UP", Mods: ModShift},
		'b': {Code: "ARROW_DOWN", Mods: ModShift},
		'c': {Code: "ARROW_RIGHT", Mods: ModShift},
		'd': {Code: "ARROW_LEFT", Mods: ModShift},
	}
	ss3 = map[byte]Key{
		'M': {Code: "ENTER"},
		'X': RuneKey('='),
		// urxvt's arrows with ctrl held.
		'a': {Code: "ARROW_UP", Mods: ModCtrl},
		'b': {Code: "ARROW_DOWN", Mods: ModCtrl},
		'c': {Code: "ARROW_RIGHT", Mods: ModCtrl},
		'd': {Code: "ARROW_LEFT", Mods: ModCtrl},
	}
	// The keypad in application mode, "j" through "y".
	for i, r := range "*+,-./0123456789" {
		ss3[byte('j'+i)] = RuneKey(r)
	}
	for final, code := range map[byte]string{
		'A': "ARROW_UP",
		'B': "ARROW_DOWN",
		'C': "ARROW_RIGHT",
		'D': "ARROW_LEFT",
		'E': "NUMPAD_BEGIN",
		'F': "END",
		'H': "HOME",
		'P': "F1",
		'Q': "F2",
		'R': "F3",
		'S': "F4",
	} {
		csi[final] = Key{Code: code}
		ss3[final] = Key{Code: code}
	}
	return csi, ss3
}()

// tildeKeys maps the number of a key ending in "~" to the key.
var tildeKeys = func() map[int]string {
	keys := map[int]string{
		1:     "HOME",
		2:     "INSERT",
		3:     "DELETE",
		4:     "END",
		5:     "PAGE_UP",
		6:     "PAGE_DOWN",
		7:     "HOME",
		8:     "END",
		57427: "NUMPAD_BEGIN",
	}
	for i, n := range []int{11, 12, 13, 14, 15, 17, 18, 19, 20, 21, 23, 24, 25, 26, 28, 29, 31, 32, 33, 34, 42, 43, 44, 45, 46} {
		keys[n] = "F" + strconv.Itoa(i+1)
	}
	return keys
}()

// kittyCodes maps the kitty keyboard protocol's codes for keys that type no
// character, mostly in the private use area, to the keys. The keypad's keys
// map to the keys they match. Codes for keys the terminal has none for,
// such as F26 and hyper, are left out.
var kittyCodes = func() map[int]Key {
	keys := map[int]Key{
		ansi.BS:  {Code: "BACKSPACE"},
		ansi.DEL: {Code: "BACKSPACE"},
		57414:    {Code: "ENTER"},
		57415:    RuneKey('='),
		57416:    RuneKey(','),
	}
	for i, code := range []string{
		"ESCAPE", "ENTER", "TAB", "BACKSPACE", "INSERT", "DELETE",
		"ARROW_LEFT", "ARROW_RIGHT", "ARROW_UP", "ARROW_DOWN",
		"PAGE_UP", "PAGE_DOWN", "HOME", "END",
		"CAPS_LOCK", "SCROLL_LOCK", "NUM_LOCK", "PRINT_SCREEN", "PAUSE", "CONTEXT_MENU",
	} {
		keys[57344+i] = Key{Code: code}
	}
	for i := range 25 {
		keys[57364+i] = Key{Code: "F" + strconv.Itoa(i+1)}
	}
	for i, r := range "0123456789./*-+" {
		keys[57399+i] = RuneKey(r)
	}
	for i, code := range []string{
		"ARROW_LEFT", "ARROW_RIGHT", "ARROW_UP", "ARROW_DOWN",
		"PAGE_UP", "PAGE_DOWN", "HOME", "END", "INSERT", "DELETE", "NUMPAD_BEGIN",
	} {
		keys[57417+i] = Key{Code: code}
	}
	for code, name := range map[int]string{
		57430: "MEDIA_PLAY_PAUSE",
		57432: "MEDIA_STOP",
		57435: "MEDIA_TRACK_NEXT",
		57436: "MEDIA_TRACK_PREVIOUS",
		57438: "AUDIO_VOLUME_DOWN",
		57439: "AUDIO_VOLUME_UP",
		57440: "AUDIO_VOLUME_MUTE",
		57441: "SHIFT_LEFT",
		57442: "CONTROL_LEFT",
		57443: "ALT_LEFT",
		57444: "META_LEFT",
		57447: "SHIFT_RIGHT",
		57448: "CONTROL_RIGHT",
		57449: "ALT_RIGHT",
		57450: "META_RIGHT",
	} {
		keys[code] = Key{Code: name}
	}
	return keys
}()
