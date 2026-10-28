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
		n, ev := decodeEvent(b)
		b = b[n:]
		if ev.kind == eventKey && ev.key.Text != "" && ev.key.Mods&^ModShift == 0 {
			text = append(text, ev.key.Text...)
			continue
		}
		var s Stroke
		switch ev.kind {
		case eventNone:
			continue
		case eventKey:
			s = Stroke{Kind: StrokeKey, Key: ev.key.String()}
		case eventMouse:
			s = Stroke{Kind: StrokeMouse, Mouse: ev.mouse}
		case eventPaste:
			s = Stroke{Kind: StrokePaste, Text: string(ev.paste)}
		}
		flush()
		out = append(out, s)
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

// eventKind is what an [inputEvent] is.
type eventKind uint8

// Event kinds. eventNone is input that is skipped.
const (
	eventNone eventKind = iota
	eventKey
	eventMouse
	eventPaste
)

// inputEvent is one thing [decodeEvent] finds in input: a key press, mouse
// event, or paste, as its kind says.
type inputEvent struct {
	kind  eventKind
	key   Key
	mouse MouseEvent
	paste []byte
}

func keyEvent(k Key) inputEvent          { return inputEvent{kind: eventKey, key: k} }
func mouseInput(m MouseEvent) inputEvent { return inputEvent{kind: eventMouse, mouse: m} }

// decodeEvent decodes the event at the start of b, and returns how many
// bytes it took. The event is nil for input that is skipped.
func decodeEvent(b []byte) (int, inputEvent) {
	switch c := b[0]; {
	case c == ansi.ESC:
		return decodeEsc(b)
	case c == ansi.SS3:
		return decodeSS3(b, 1)
	case c < ansi.SP || c == ansi.DEL:
		return 1, keyEvent(controlKey(c))
	}
	return decodeSeq(b)
}

// decodeEsc decodes input starting with ESC: a sequence, alt with a key, or
// the escape key.
func decodeEsc(b []byte) (int, inputEvent) {
	if len(b) == 1 {
		return 1, keyEvent(Key{Code: "ESCAPE"})
	}
	switch b[1] {
	case '[', 'O', 'P', ']', 'X', '^', '_':
		if len(b) == 2 {
			// A sequence introducer with nothing after it is alt with the
			// introducer's key.
			return 2, keyEvent(runeKey(rune(b[1]), ModAlt))
		}
		if b[1] == 'O' {
			return decodeSS3(b, 2)
		}
		return decodeSeq(b)
	case ansi.ESC:
		// Some terminals send alt with a CSI or SS3 key as ESC before it;
		// otherwise a second ESC is alt+esc.
		if len(b) == 2 || b[2] != '[' && b[2] != 'O' {
			return 2, keyEvent(Key{Code: "ESCAPE", Mods: ModAlt})
		}
	}
	n, ev := decodeEvent(b[1:])
	if ev.kind == eventKey {
		return n + 1, keyEvent(ev.key.with(ModAlt))
	}
	return 1, keyEvent(Key{Code: "ESCAPE"})
}

// decodeSeq decodes the escape sequence or text at the start of b.
func decodeSeq(b []byte) (int, inputEvent) {
	seq, _, n, _ := ansi.DecodeSequence(b, ansi.NormalState, nil)
	switch {
	case ansi.HasCsiPrefix(seq):
		return decodeCSI(b, n)
	case seq[0] == ansi.ESC:
		// An OSC, DCS, APC, PM, or SOS string.
		return n, inputEvent{}
	}
	r, size := utf8.DecodeRune(seq)
	switch {
	case r == utf8.RuneError && size <= 1:
		// A C1 control or string, or a byte that does not start UTF-8.
		return n, inputEvent{}
	case size == len(seq):
		return n, keyEvent(RuneKey(r))
	case utf8.Valid(seq):
		// A grapheme of more than one rune, which no key types.
		return n, keyEvent(Key{Text: string(seq)})
	}
	// A grapheme with bytes that are not UTF-8 in it: its first character,
	// with the rest decoded on its own.
	return size, keyEvent(RuneKey(r))
}

// decodeCSI decodes the CSI sequence of n bytes at the start of b, with the
// bytes after it that X10 mouse reports and pastes take.
func decodeCSI(b []byte, n int) (int, inputEvent) {
	c := parseCSI(b[:n])
	switch {
	case c.inter == '$' && c.prefix == 0 && c.paramCount() == 1:
		// urxvt ends a key with shift held with "$", so what follows is not
		// part of the sequence.
		end := bytes.IndexByte(b[:n], '$') + 1
		if code, ok := tildeKeys[c.param(0, 0, -1)]; ok {
			return end, keyEvent(Key{Code: code, Mods: ModShift})
		}
		return end, inputEvent{}
	case c.final == 'M' && c.prefix == 0 && c.inter == 0 && c.paramCount() == 0:
		// An X10 mouse report: three bytes, each offset by 32.
		if len(b) < n+3 {
			return len(b), inputEvent{}
		}
		v := b[n : n+3]
		return n + 3, mouseInput(mouseEvent(int(v[0])-32, int(v[1])-32, int(v[2])-32, false))
	case c.final == '~' && c.prefix == 0 && c.param(0, 0, 0) == 200:
		body := b[n:]
		end := bytes.Index(body, []byte(ansi.BracketedPasteEnd))
		if end < 0 {
			return len(b), inputEvent{kind: eventPaste, paste: body}
		}
		return n + end + len(ansi.BracketedPasteEnd), inputEvent{kind: eventPaste, paste: body[:end]}
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
		return i, inputEvent{}
	}
	key := ss3Keys[b[i]]
	if key.Code == "" {
		return i + 1, inputEvent{}
	}
	return i + 1, keyEvent(key.with(wireMods(param)))
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
func (c csiSeq) event() inputEvent {
	switch {
	case c.prefix == '<':
		if (c.final == 'M' || c.final == 'm') && c.paramCount() == 3 {
			return mouseInput(mouseEvent(c.param(0, 0, 0), c.param(1, 0, 1), c.param(2, 0, 1), c.final == 'm'))
		}
		return inputEvent{}
	case c.prefix != 0 || c.inter != 0 || c.param(1, 1, 1) == 3:
		// A reply to a query, or a key release in the kitty protocol.
		return inputEvent{}
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
			return mouseInput(mouseEvent(c.param(0, 0, 32)-32, c.param(1, 0, 1), c.param(2, 0, 1), false))
		}
	case 'R':
		if len(c.params) > 0 {
			return inputEvent{}
		}
	}
	key := csiKeys[c.final]
	if key.Code == "" || c.param(0, 0, 1) != 1 {
		return inputEvent{}
	}
	return keyEvent(key.with(mods))
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
		return inputEvent{}
	}
	if p, has := c.subParams(2); has && key.Text != "" {
		var text []byte
		for len(p) > 0 {
			var r int
			r, p = nextSubParam(p, -1)
			if r > 0 && utf8.ValidRune(rune(r)) {
				text = utf8.AppendRune(text, rune(r))
			}
		}
		if len(text) > 0 {
			key.Text = string(text)
		}
	}
	return keyEvent(key)
}

// tildeKey decodes a key ending in "~", or in urxvt's "^" for ctrl or "@"
// for ctrl and shift, including xterm's modifyOtherKeys form.
func (c csiSeq) tildeKey(mods Mods) inputEvent {
	n := c.param(0, 0, -1)
	switch c.final {
	case '~':
		if n == 27 && c.paramCount() == 3 {
			key, ok := codeKey(c.param(2, 0, -1), mods)
			if !ok {
				return inputEvent{}
			}
			return keyEvent(key)
		}
	case '^':
		mods |= ModCtrl
	case '@':
		mods |= ModCtrl | ModShift
	}
	code, ok := tildeKeys[n]
	if !ok {
		return inputEvent{}
	}
	return keyEvent(Key{Code: code}.with(mods))
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

// csiKeys and ss3Keys map the final byte of CSI and SS3 keys to the keys. A
// final byte that is no key has no code.
var csiKeys, ss3Keys = func() (csi, ss3 [utf8.RuneSelf]Key) {
	for final, key := range map[byte]Key{
		'Z': {Code: "TAB", Mods: ModShift},
		// urxvt's arrows with shift held.
		'a': {Code: "ARROW_UP", Mods: ModShift},
		'b': {Code: "ARROW_DOWN", Mods: ModShift},
		'c': {Code: "ARROW_RIGHT", Mods: ModShift},
		'd': {Code: "ARROW_LEFT", Mods: ModShift},
	} {
		csi[final] = key
	}
	for final, key := range map[byte]Key{
		'M': {Code: "ENTER"},
		'X': RuneKey('='),
		// urxvt's arrows with ctrl held.
		'a': {Code: "ARROW_UP", Mods: ModCtrl},
		'b': {Code: "ARROW_DOWN", Mods: ModCtrl},
		'c': {Code: "ARROW_RIGHT", Mods: ModCtrl},
		'd': {Code: "ARROW_LEFT", Mods: ModCtrl},
	} {
		ss3[final] = key
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
