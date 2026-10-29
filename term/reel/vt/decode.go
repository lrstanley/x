// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// StrokeKind is what a [Stroke] is.
type StrokeKind uint8

// Stroke kinds.
const (
	// StrokeKey is a key press that types no text, or is held with a
	// modifier other than shift, such as enter or ctrl+c.
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
	// Key is the key pressed for [StrokeKey]. [Key.String] names it, such as
	// "enter", "ctrl+c", "alt+enter", or "shift+tab".
	Key Key
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
	var text []byte
	// Most strokes other than text start with ESC, so their count is a close
	// guess at how many strokes there are.
	out := make([]Stroke, 0, bytes.Count(data, []byte{ansi.ESC})+1)
	flush := func() {
		if len(text) > 0 {
			out = append(out, Stroke{Kind: StrokeText, Text: string(text)})
			text = text[:0]
		}
	}
	for b := data; len(b) > 0; {
		if n := textLen(b); n > 0 {
			text = append(text, b[:n]...)
			b = b[n:]
			continue
		}
		// Every decoder takes at least one byte.
		n, s := decodeEvent(b)
		b = b[n:]
		switch {
		case s.Kind == StrokeKey && s.Key.Text != "" && s.Key.Mods&^ModShift == 0:
			text = append(text, s.Key.Text...)
		case s.Kind != 0:
			flush()
			out = append(out, s)
		}
	}
	flush()
	if len(out) == 0 {
		return nil
	}
	return out
}

// textLen returns how many bytes at the start of b are characters that type
// themselves, which [DecodeInput] takes as text without decoding them as
// keys: UTF-8 other than control characters.
func textLen(b []byte) int {
	i := 0
	for i < len(b) {
		if c := b[i]; c < utf8.RuneSelf {
			if c < ansi.SP || c == ansi.DEL {
				break
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 || r <= 0x9f {
			// A byte that does not start UTF-8, or a C1 control.
			break
		}
		i += size
	}
	return i
}

// keyStroke is the stroke of a key press. The decoders return it for keys
// that type text too, which [DecodeInput] adds to the text around them.
func keyStroke(k Key) Stroke { return Stroke{Kind: StrokeKey, Key: k} }

// decodeEvent decodes the stroke at the start of b, and returns how many
// bytes it took. The stroke has no kind for input that is skipped.
func decodeEvent(b []byte) (int, Stroke) {
	switch c := b[0]; {
	case c == ansi.ESC:
		return decodeEsc(b)
	case c == ansi.SS3:
		return decodeSS3(b, 1)
	case c < ansi.SP || c == ansi.DEL:
		return 1, keyStroke(controlKey(c))
	}
	return decodeSeq(b)
}

// decodeEsc decodes input starting with ESC: a sequence, alt with a key, or
// the escape key.
//
//nolint:goconst // Ghostty key codes in decoding tables.
func decodeEsc(b []byte) (int, Stroke) {
	if len(b) == 1 {
		return 1, keyStroke(Key{Code: "ESCAPE"})
	}
	switch b[1] {
	case '[', 'O', 'P', ']', 'X', '^', '_':
		if len(b) == 2 {
			// A sequence introducer with nothing after it is alt with the
			// introducer's key.
			return 2, keyStroke(runeKey(rune(b[1]), ModAlt))
		}
		if b[1] == 'O' {
			return decodeSS3(b, 2)
		}
		n, s := decodeSeq(b)
		if b[1] != '[' && !stringEnded(b[:n]) {
			// A string cut off before its terminator is alt with the
			// introducer's key: a terminal ends the strings it sends.
			return 2, keyStroke(runeKey(rune(b[1]), ModAlt))
		}
		return n, s
	case ansi.ESC:
		// Some terminals send alt with a CSI or SS3 key as ESC before it;
		// otherwise a second ESC is alt+esc.
		if len(b) == 2 || b[2] != '[' && b[2] != 'O' {
			return 2, keyStroke(Key{Code: "ESCAPE", Mods: ModAlt})
		}
	}
	n, s := decodeEvent(b[1:])
	if s.Kind == StrokeKey {
		return n + 1, keyStroke(s.Key.with(ModAlt))
	}
	return 1, keyStroke(Key{Code: "ESCAPE"})
}

// stringEnded reports whether seq, an OSC, DCS, APC, PM, or SOS string, ends
// in a terminator rather than being cut off.
func stringEnded(seq []byte) bool {
	return bytes.HasSuffix(seq, []byte{ansi.BEL}) || bytes.HasSuffix(seq, []byte{ansi.ESC, '\\'}) ||
		bytes.HasSuffix(seq, []byte{ansi.ST})
}

// decodeSeq decodes the escape sequence or text at the start of b.
func decodeSeq(b []byte) (int, Stroke) {
	seq, _, n, _ := ansi.DecodeSequence(b, ansi.NormalState, nil)
	switch {
	case ansi.HasCsiPrefix(seq):
		return decodeCSI(b, n)
	case seq[0] == ansi.ESC:
		// An OSC, DCS, APC, PM, or SOS string.
		return n, Stroke{}
	}
	r, size := utf8.DecodeRune(seq)
	switch {
	case r == utf8.RuneError && size == 1 && seq[0] >= 0x80 && seq[0] <= 0x9f:
		// A C1 control or string.
		return n, Stroke{}
	case r == utf8.RuneError && size == 1:
		// A byte that does not start UTF-8. Grapheme clustering can take the
		// bytes after it too, so they are decoded on their own.
		return 1, Stroke{}
	case size == len(seq):
		return n, keyStroke(RuneKey(r))
	case utf8.Valid(seq):
		// A grapheme of more than one rune, which no key types.
		return n, keyStroke(Key{Text: string(seq)})
	}
	// A grapheme with bytes that are not UTF-8 in it: its first character,
	// with the rest decoded on its own.
	return size, keyStroke(RuneKey(r))
}

// decodeCSI decodes the CSI sequence of n bytes at the start of b, with the
// bytes after it that X10 mouse reports and pastes take.
func decodeCSI(b []byte, n int) (int, Stroke) {
	c := parseCSI(b[:n])
	switch {
	case c.inter == '$' && c.prefix == 0 && c.paramCount() == 1:
		// urxvt ends a key with shift held with "$", so what follows is not
		// part of the sequence.
		end := bytes.IndexByte(b[:n], '$') + 1
		if code, ok := tildeKeys[c.param(0, 0, -1)]; ok {
			return end, keyStroke(Key{Code: code, Mods: ModShift})
		}
		return end, Stroke{}
	case c.final == 'M' && c.prefix == 0 && c.inter == 0 && c.paramCount() == 0:
		// An X10 mouse report: three bytes, each offset by 32.
		if len(b) < n+3 {
			return len(b), Stroke{}
		}
		v := b[n : n+3]
		return n + 3, mouseStroke(int(v[0])-32, int(v[1])-32, int(v[2])-32, false)
	case c.final == '~' && c.prefix == 0 && c.param(0, 0, 0) == 200:
		body := b[n:]
		end := bytes.Index(body, []byte(ansi.BracketedPasteEnd))
		if end < 0 {
			return len(b), Stroke{Kind: StrokePaste, Text: string(body)}
		}
		return n + end + len(ansi.BracketedPasteEnd), Stroke{Kind: StrokePaste, Text: string(body[:end])}
	}
	return n, c.event()
}

// decodeSS3 decodes the SS3 key at the start of b, whose introducer is i
// bytes. Some terminals put a modifier parameter before the key.
func decodeSS3(b []byte, i int) (int, Stroke) {
	param := 0
	for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
		param = min(param*10+int(b[i]-'0'), 1<<16)
	}
	if i >= len(b) || b[i] < '!' || b[i] > '~' {
		return i, Stroke{}
	}
	key := ss3Keys[b[i]]
	if key.Code == "" {
		return i + 1, Stroke{}
	}
	return i + 1, keyStroke(key.with(wireMods(param)))
}

// csiSeq is a CSI sequence split into its parts. params holds the
// parameter bytes: ";"-separated parameters, each of ":"-separated
// sub-parameters. final is 0 for a sequence cut off before its final byte.
type csiSeq struct {
	prefix, inter, final byte
	params               []byte
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
	for i < len(seq) && seq[i] >= '0' && seq[i] <= ';' {
		i++
	}
	c.params = seq[:i]
	for ; i < len(seq) && seq[i] >= ' ' && seq[i] <= '/'; i++ {
		c.inter = seq[i]
	}
	if i < len(seq) && seq[i] >= '@' && seq[i] <= '~' {
		c.final = seq[i]
	}
	return c
}

// paramCount returns how many parameters the sequence has.
func (c csiSeq) paramCount() int {
	if len(c.params) == 0 {
		return 0
	}
	return bytes.Count(c.params, []byte{';'}) + 1
}

// subParams returns the bytes of parameter i, and whether it exists.
func (c csiSeq) subParams(i int) ([]byte, bool) {
	p := c.params
	if len(p) == 0 {
		return nil, false
	}
	for ; i > 0; i-- {
		j := bytes.IndexByte(p, ';')
		if j < 0 {
			return nil, false
		}
		p = p[j+1:]
	}
	if j := bytes.IndexByte(p, ';'); j >= 0 {
		p = p[:j]
	}
	return p, true
}

// param returns sub-parameter sub of parameter i, or def when it is missing.
func (c csiSeq) param(i, sub, def int) int {
	p, ok := c.subParams(i)
	if !ok {
		return def
	}
	for ; sub > 0; sub-- {
		j := bytes.IndexByte(p, ':')
		if j < 0 {
			return def
		}
		p = p[j+1:]
	}
	v, _ := nextSubParam(p, def)
	return v
}

// nextSubParam returns the first ":"-separated value in p, or def when it is
// empty, and the rest of p after it. Values saturate rather than overflow.
func nextSubParam(p []byte, def int) (v int, rest []byte) {
	end := bytes.IndexByte(p, ':')
	if end < 0 {
		end, rest = len(p), nil
	} else {
		rest = p[end+1:]
	}
	if end == 0 {
		return def, rest
	}
	for _, d := range p[:end] {
		v = min(v*10+int(d-'0'), 1<<24)
	}
	return v, rest
}

// event decodes the sequence, other than an X10 mouse report or a paste.
func (c csiSeq) event() Stroke {
	switch {
	case c.prefix == '<':
		if (c.final == 'M' || c.final == 'm') && c.paramCount() == 3 {
			return mouseStroke(c.param(0, 0, 0), c.param(1, 0, 1), c.param(2, 0, 1), c.final == 'm')
		}
		return Stroke{}
	case c.prefix != 0 || c.inter != 0 || c.param(1, 1, 1) == 3:
		// A reply to a query, or a key release in the kitty protocol.
		return Stroke{}
	}
	mods := wireMods(c.param(1, 0, 1))
	switch c.final {
	case 'u':
		return c.kittyKey(mods)
	case '~', '^', '@':
		return c.tildeKey(mods)
	case 'M':
		if c.paramCount() == 3 {
			// A urxvt mouse report: the button offset by 32, and the cell.
			return mouseStroke(c.param(0, 0, 32)-32, c.param(1, 0, 1), c.param(2, 0, 1), false)
		}
	case 'R':
		if len(c.params) > 0 {
			return Stroke{}
		}
	}
	key := csiKeys[c.final]
	if key.Code == "" || c.param(0, 0, 1) != 1 {
		return Stroke{}
	}
	return keyStroke(key.with(mods))
}

// kittyKey decodes a kitty keyboard protocol key: its code, the shifted
// code when the program asked for it, and the text it types when the
// program asked for that.
func (c csiSeq) kittyKey(mods Mods) Stroke {
	code := c.param(0, 0, -1)
	if shifted := c.param(0, 1, -1); shifted > 0 && mods&ModShift != 0 {
		code = shifted
	}
	key, ok := codeKey(code, mods)
	if !ok {
		return Stroke{}
	}
	if p, has := c.subParams(2); has && key.Text != "" {
		var text []byte
		for len(p) > 0 {
			var r int
			r, p = nextSubParam(p, -1)
			if r > 0 && r <= utf8.MaxRune && utf8.ValidRune(rune(r)) {
				text = utf8.AppendRune(text, rune(r))
			}
		}
		if len(text) > 0 {
			key.Text = string(text)
		}
	}
	return keyStroke(key)
}

// tildeKey decodes a key ending in "~", or in urxvt's "^" for ctrl or "@"
// for ctrl and shift, including xterm's modifyOtherKeys form.
func (c csiSeq) tildeKey(mods Mods) Stroke {
	n := c.param(0, 0, -1)
	switch c.final {
	case '~':
		if n == 27 && c.paramCount() == 3 {
			key, ok := codeKey(c.param(2, 0, -1), mods)
			if !ok {
				return Stroke{}
			}
			return keyStroke(key)
		}
	case '^':
		mods |= ModCtrl
	case '@':
		mods |= ModCtrl | ModShift
	}
	code, ok := tildeKeys[n]
	if !ok {
		return Stroke{}
	}
	return keyStroke(Key{Code: code}.with(mods))
}

// codeKey is the key a kitty or modifyOtherKeys key code stands for, with
// mods held: a key in [kittyCodes], a control character, or the character the
// code is.
func codeKey(code int, mods Mods) (Key, bool) {
	if key, ok := kittyCodes[code]; ok {
		return key.with(mods), true
	}
	switch {
	case code >= 0 && code < ansi.SP:
		return controlKey(byte(code)).with(mods), true
	case code > 0 && code <= utf8.MaxRune && utf8.ValidRune(rune(code)) && unicode.IsGraphic(rune(code)):
		return runeKey(rune(code), mods), true
	}
	return Key{}, false
}

// controlKey is the key a control character stands for: ctrl with a letter
// or symbol, or a key such as enter or tab. DEL is backspace and BS is
// ctrl+h, as terminals send them by default.
//
//nolint:goconst // Ghostty key codes in decoding tables.
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

// mouseStroke decodes the button byte of a mouse report at cell (x, y),
// counted from 1. release is set by an SGR report ending in "m"; the X10 and
// urxvt encodings say a button was released, but not which one.
func mouseStroke(b, x, y int, release bool) Stroke {
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
		return Stroke{Kind: StrokeMouse, Mouse: m}
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
	return Stroke{Kind: StrokeMouse, Mouse: m}
}

// csiKeys maps the final byte of a CSI key to the key. A final byte that is
// no key has no code.
//
//nolint:goconst // Ghostty key codes in decoding tables.
var csiKeys = [utf8.RuneSelf]Key{
	'A': {Code: "ARROW_UP"},
	'B': {Code: "ARROW_DOWN"},
	'C': {Code: "ARROW_RIGHT"},
	'D': {Code: "ARROW_LEFT"},
	'E': {Code: "NUMPAD_BEGIN"},
	'F': {Code: "END"},
	'H': {Code: "HOME"},
	'P': {Code: "F1"},
	'Q': {Code: "F2"},
	'R': {Code: "F3"},
	'S': {Code: "F4"},
	'Z': {Code: "TAB", Mods: ModShift},
	// urxvt's arrows with shift held.
	'a': {Code: "ARROW_UP", Mods: ModShift},
	'b': {Code: "ARROW_DOWN", Mods: ModShift},
	'c': {Code: "ARROW_RIGHT", Mods: ModShift},
	'd': {Code: "ARROW_LEFT", Mods: ModShift},
}

// ss3Keys maps the final byte of an SS3 key to the key, as [csiKeys] does.
var ss3Keys = [utf8.RuneSelf]Key{
	'A': {Code: "ARROW_UP"},
	'B': {Code: "ARROW_DOWN"},
	'C': {Code: "ARROW_RIGHT"},
	'D': {Code: "ARROW_LEFT"},
	'E': {Code: "NUMPAD_BEGIN"},
	'F': {Code: "END"},
	'H': {Code: "HOME"},
	'M': {Code: "ENTER"},
	'P': {Code: "F1"},
	'Q': {Code: "F2"},
	'R': {Code: "F3"},
	'S': {Code: "F4"},
	// urxvt's arrows with ctrl held.
	'a': {Code: "ARROW_UP", Mods: ModCtrl},
	'b': {Code: "ARROW_DOWN", Mods: ModCtrl},
	'c': {Code: "ARROW_RIGHT", Mods: ModCtrl},
	'd': {Code: "ARROW_LEFT", Mods: ModCtrl},
	// The keypad in application mode.
	'X': RuneKey('='),
	'j': RuneKey('*'),
	'k': RuneKey('+'),
	'l': RuneKey(','),
	'm': RuneKey('-'),
	'n': RuneKey('.'),
	'o': RuneKey('/'),
	'p': RuneKey('0'),
	'q': RuneKey('1'),
	'r': RuneKey('2'),
	's': RuneKey('3'),
	't': RuneKey('4'),
	'u': RuneKey('5'),
	'v': RuneKey('6'),
	'w': RuneKey('7'),
	'x': RuneKey('8'),
	'y': RuneKey('9'),
}

// tildeKeys maps the number of a key ending in "~" to the key.
//
//nolint:goconst // Ghostty key codes in decoding tables.
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
//
//nolint:goconst // Ghostty key codes in decoding tables.
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
