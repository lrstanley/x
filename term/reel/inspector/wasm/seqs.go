// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Token groups, which the inspector draws as timeline lanes.
const (
	groupText   = "text"
	groupCtl    = "control"
	groupSGR    = "sgr"
	groupCursor = "cursor"
	groupErase  = "erase"
	groupMode   = "mode"
	groupQuery  = "query"
	groupCSI    = "csi"
	groupESC    = "esc"
	groupOSC    = "osc"
	groupDCS    = "dcs"
	groupAPC    = "apc"
	groupOther  = "other"
)

// maxRaw caps the escaped bytes kept for one token.
const maxRaw = 2048

// token is one escape sequence, control, or run of text in program output.
type token struct {
	// Offset is the token's first byte in its event's data. A sequence
	// split across events belongs to the event it starts in; its length
	// can extend past that event's data.
	Offset    int    `json:"o"`
	Continues bool   `json:"continues,omitempty"`
	Len       int    `json:"len"`
	Group     string `json:"g"`
	// Tag is the timeline lane the token counts toward, such as "SGR" or
	// "OSC 8". It is empty for text.
	Tag  string `json:"t,omitempty"`
	Name string `json:"n,omitempty"`
	Desc string `json:"d,omitempty"`
	Raw  string `json:"r"`
	// Fields break the sequence out; only an event's detail has them.
	Fields []field `json:"f,omitempty"`
}

// tokenize splits the concatenated output stream into tokens, and hands
// each one to emit with its offset in the stream.
func tokenize(stream []byte, emit func(at int, tok token)) {
	p := ansi.NewParser()
	p.SetDataSize(64 << 10)
	var state byte
	textStart := -1
	flush := func(end int) {
		if textStart >= 0 {
			emit(textStart, token{Len: end - textStart, Group: groupText, Raw: escape(stream[textStart:end])})
			textStart = -1
		}
	}
	for i := 0; i < len(stream); {
		seq, _, n, newState := ansi.DecodeSequence(stream[i:], state, p)
		state = newState
		if n <= 0 {
			n = 1
			seq = stream[i : i+1]
		}
		if tok, ok := classify(seq, p); ok {
			flush(i)
			tok.Len = n
			tok.Raw = escape(seq)
			emit(i, tok)
		} else if textStart < 0 {
			textStart = i
		}
		i += n
	}
	flush(len(stream))
}

// classify names seq, decoded by p. It reports false for printable text
// and the controls that move through it (CR, LF, TAB, BS).
func classify(seq []byte, p *ansi.Parser) (token, bool) {
	var tok token
	cmd := ansi.Cmd(p.Command())
	switch {
	case len(seq) == 0:
		return tok, false
	case ansi.HasCsiPrefix(seq):
		tok.Group, tok.Name, tok.Desc = csiName(cmd, p.Params())
		tok.Tag = strings.ToUpper(tok.Group)
		if tok.Group == groupCSI {
			tok.Tag = "CSI " + tok.Name
		}
	case ansi.HasOscPrefix(seq):
		data := string(p.Data())
		num, _, _ := strings.Cut(data, ";")
		tok.Group, tok.Tag = groupOSC, "OSC "+num
		tok.Name, tok.Desc = "OSC "+num, oscNames[num]
		if num == "9" && strings.HasPrefix(data, "9;4;") {
			tok.Desc = wordProgress
		}
		if strings.HasSuffix(data, ";?") {
			tok.Desc += " (query)"
		}
	case ansi.HasDcsPrefix(seq):
		tok.Group, tok.Tag, tok.Name = groupDCS, "DCS", "DCS"
		switch {
		case cmd.Intermediate() == '+' && cmd.Final() == 'q':
			tok.Group, tok.Tag, tok.Name, tok.Desc = groupQuery, "QUERY", "XTGETTCAP", "query terminfo capability"
		case cmd.Intermediate() == '$' && cmd.Final() == 'q':
			tok.Group, tok.Tag, tok.Name, tok.Desc = groupQuery, "QUERY", "DECRQSS", "request setting"
		}
	case ansi.HasApcPrefix(seq):
		tok.Group, tok.Tag, tok.Name = groupAPC, "APC", "APC"
		if d := p.Data(); len(d) > 0 && d[0] == 'G' {
			tok.Name, tok.Desc = "APC G", "kitty graphics"
		}
	case ansi.HasEscPrefix(seq) && len(seq) > 1:
		tok.Group, tok.Tag = groupESC, "ESC"
		tok.Name, tok.Desc = "ESC "+string(seq[1:]), escNames[string(seq[1:])]
	case len(seq) == 1 && (seq[0] < 0x20 || seq[0] == 0x7f):
		switch seq[0] {
		case '\r', '\n', '\t', '\b':
			return tok, false
		}
		tok.Group, tok.Tag = groupCtl, ctlNames[seq[0]]
		if tok.Tag == "" {
			tok.Tag = fmt.Sprintf("C0 0x%02x", seq[0])
		}
		tok.Name = tok.Tag
	case ansi.HasSosPrefix(seq), ansi.HasPmPrefix(seq):
		tok.Group, tok.Tag, tok.Name = groupOther, "SOS/PM", "SOS/PM"
	default:
		return tok, false
	}
	return tok, true
}

func csiName(cmd ansi.Cmd, params ansi.Params) (group, name, desc string) {
	prefix, inter, final := cmd.Prefix(), cmd.Intermediate(), cmd.Final()
	var ps []string
	for _, prm := range params {
		ps = append(ps, strconv.Itoa(prm.Param(0)))
	}
	switch {
	case final == 'm' && prefix == 0 && inter == 0:
		return groupSGR, "SGR", sgrDesc(ps)
	case (final == 'h' || final == 'l') && inter == 0:
		name, desc = "SM", "set mode"
		if final == 'l' {
			name, desc = "RM", "reset mode"
		}
		if prefix == '?' {
			name, desc = "DECSET", "set private mode"
			if final == 'l' {
				name, desc = "DECRST", "reset private mode"
			}
			var names []string
			for _, m := range ps {
				if n := modeNames[m]; n != "" {
					names = append(names, m+" "+n)
				} else {
					names = append(names, m)
				}
			}
			desc += ": " + strings.Join(names, ", ")
		}
		return groupMode, name, desc
	case final == 'p' && inter == '$':
		return groupQuery, "DECRQM", "request mode " + string(rune(max(prefix, ' '))) + strings.Join(ps, ";")
	case final == 'n':
		return groupQuery, "DSR", "device status report " + strings.Join(ps, ";")
	case final == 'c' && prefix == 0:
		return groupQuery, "DA1", "primary device attributes"
	case final == 'c' && prefix == '>':
		return groupQuery, "DA2", "secondary device attributes"
	case final == 'c' && prefix == '=':
		return groupQuery, "DA3", "tertiary device attributes"
	case final == 'q' && prefix == '>':
		return groupQuery, "XTVERSION", "terminal name and version"
	case final == 'u' && prefix == '?':
		return groupQuery, "kitty kbd?", "query kitty keyboard flags"
	case final == 'u' && (prefix == '>' || prefix == '<' || prefix == '='):
		return groupMode, "kitty kbd " + string(prefix), "kitty keyboard flags " + strings.Join(ps, ";")
	case final == 'q' && inter == ' ':
		return groupCursor, "DECSCUSR", "cursor style " + strings.Join(ps, ";")
	}
	if prefix == 0 && inter == 0 {
		if n, ok := cursorNames[final]; ok {
			return groupCursor, n, strings.Join(ps, ";")
		}
		if n, ok := eraseNames[final]; ok {
			return groupErase, n, strings.Join(ps, ";")
		}
		switch final {
		case 'r':
			return groupCSI, "DECSTBM", "scroll region " + strings.Join(ps, ";")
		case 't':
			return groupCSI, "XTWINOPS", "window op " + strings.Join(ps, ";")
		case 'b':
			return groupCSI, "REP", "repeat char " + strings.Join(ps, ";")
		}
	}
	var b strings.Builder
	if prefix != 0 {
		b.WriteByte(prefix)
	}
	if inter != 0 {
		b.WriteByte(inter)
	}
	b.WriteByte(final)
	return groupCSI, b.String(), strings.Join(ps, ";")
}

func sgrDesc(ps []string) string {
	if len(ps) == 0 || len(ps) == 1 && ps[0] == "0" {
		return wordReset
	}
	return strings.Join(ps, ";")
}

var cursorNames = map[byte]string{
	'A': "CUU", 'B': "CUD", 'C': "CUF", 'D': "CUB", 'E': "CNL", 'F': "CPL",
	'G': "CHA", 'H': "CUP", 'f': "HVP", 'd': "VPA", '`': "HPA", 'I': "CHT",
	'Z': "CBT", 's': "SCOSC", 'u': "SCORC",
}

var eraseNames = map[byte]string{
	'J': "ED", 'K': "EL", 'X': "ECH", 'P': "DCH", '@': "ICH", 'L': "IL",
	'M': "DL", 'S': "SU", 'T': "SD",
}

var modeNames = map[string]string{
	"1": "cursor keys", "5": "reverse video", "6": "origin", "7": "autowrap",
	"12": "cursor blink", "25": "cursor visible", "47": "alt screen",
	"1000": "mouse click", "1002": "mouse drag", "1003": "mouse motion",
	"1004": "focus events", "1006": "mouse sgr", "1016": "mouse sgr pixels",
	"1047": "alt screen", "1048": "save cursor", "1049": "alt screen + save cursor",
	"2004": "bracketed paste", "2026": "synchronized output",
	"2027": "grapheme clusters", "2031": "color scheme reports",
	"9001": "win32 input",
}

var oscNames = map[string]string{
	"0": "icon name + title", "1": "icon name", "2": wordTitle, "4": "palette color",
	"7": "working directory", "8": "hyperlink", "9": "notification",
	"10": "foreground color", "11": "background color", "12": "cursor color",
	"22": "pointer shape", "52": "clipboard", "66": "text sizing",
	"99": "kitty notification", "104": "reset palette color",
	"110": "reset foreground", "111": "reset background", "112": "reset cursor color",
	"133": "semantic prompt", "633": "vscode shell integration",
	"777": "notification", "1337": "iterm2", "5522": "kitty clipboard",
	"7501": "program status",
}

var escNames = map[string]string{
	"7": "DECSC save cursor", "8": "DECRC restore cursor", "c": "RIS full reset",
	"M": "RI reverse index", "D": "IND index", "E": "NEL next line",
	"=": "DECKPAM keypad app mode", ">": "DECKPNM keypad numeric mode",
	"H": "HTS set tab stop", "(B": "G0 charset ASCII", "(0": "G0 charset DEC graphics",
	"\\": "ST string terminator",
}

var ctlNames = map[byte]string{
	0x00: "NUL", 0x05: "ENQ", 0x07: "BEL", 0x0b: "VT", 0x0c: "FF",
	0x0e: "SO", 0x0f: "SI", 0x7f: "DEL",
}

// escape writes b with controls and invalid UTF-8 escaped, cut at maxRaw.
func escape(b []byte) string {
	var sb strings.Builder
	for len(b) > 0 {
		if sb.Len() >= maxRaw {
			fmt.Fprintf(&sb, "…(+%d bytes)", len(b))
			break
		}
		r, size := utf8.DecodeRune(b)
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&sb, `\x%02x`, b[0])
		case r == 0x1b:
			sb.WriteString(`\e`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			fmt.Fprintf(&sb, `\x%02x`, r)
		case r == '\\':
			sb.WriteString(`\\`)
		default:
			sb.Write(b[:size])
		}
		b = b[size:]
	}
	return sb.String()
}
