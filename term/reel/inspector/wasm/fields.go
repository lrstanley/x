// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/base64"
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Words fields and token descriptions share.
const (
	wordData     = "data"
	wordError    = "error"
	wordReset    = "reset"
	wordTitle    = "title"
	wordProgress = "progress"
)

// field is one part of a sequence broken out: a parameter, a key of an OSC
// payload, an attribute an SGR sets.
type field struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

// fields breaks out the parts of seq, decoded by p, or returns nil for a
// sequence with nothing more to say than its token does.
func fields(seq []byte, p *ansi.Parser) []field {
	cmd := ansi.Cmd(p.Command())
	switch {
	case ansi.HasCsiPrefix(seq):
		return csiFields(cmd, p.Params())
	case ansi.HasOscPrefix(seq):
		num, rest, _ := strings.Cut(string(p.Data()), ";")
		return oscFields(num, rest)
	case ansi.HasDcsPrefix(seq):
		if cmd.Intermediate() == '+' && cmd.Final() == 'q' {
			var fs []field
			for name := range strings.SplitSeq(string(p.Data()), ";") {
				fs = append(fs, field{"capability", unhex(name)})
			}
			return fs
		}
		if d := p.Data(); len(d) > 0 {
			return []field{{wordData, clip(string(d))}}
		}
	case ansi.HasApcPrefix(seq):
		d := string(p.Data())
		if !strings.HasPrefix(d, "G") {
			return nil
		}
		control, payload, _ := strings.Cut(d[1:], ";")
		fs := keyValues(control, ",")
		if payload != "" {
			fs = append(fs, field{"payload", fmt.Sprintf("%d bytes of base64", len(payload))})
		}
		return fs
	}
	return nil
}

func csiFields(cmd ansi.Cmd, params ansi.Params) []field {
	prefix, inter, final := cmd.Prefix(), cmd.Intermediate(), cmd.Final()
	switch {
	case final == 'm' && prefix == 0 && inter == 0:
		return sgrFields(params)
	case (final == 'h' || final == 'l') && inter == 0:
		state := map[byte]string{'h': "set", 'l': wordReset}[final]
		var fs []field
		for _, prm := range params {
			m := strconv.Itoa(prm.Param(0))
			name := m
			if prefix == '?' {
				name = "?" + m
				if n := modeNames[m]; n != "" {
					name += " " + n
				}
			}
			fs = append(fs, field{name, state})
		}
		return fs
	}
	return nil
}

// sgrFields names what each SGR parameter sets. Extended colors take the
// parameters after them, separated by ";" or ":".
func sgrFields(params ansi.Params) []field {
	if len(params) == 0 {
		return []field{{"0", wordReset}}
	}
	var fs []field
	for i := 0; i < len(params); i++ {
		n := params[i].Param(0)
		switch {
		case n == 38 || n == 48 || n == 58:
			target := map[int]string{38: "foreground", 48: "background", 58: "underline color"}[n]
			var c color.Color
			count := ansi.ReadStyleColor(params[i:], &c)
			if count == 0 {
				fs = append(fs, field{strconv.Itoa(n), target + " (malformed)"})
				continue
			}
			var key strings.Builder
			for j := range count {
				if j > 0 {
					if params[i+j-1].HasMore() {
						key.WriteByte(':')
					} else {
						key.WriteByte(';')
					}
				}
				if v := params[i+j].Param(-1); v >= 0 {
					key.WriteString(strconv.Itoa(v))
				}
			}
			value := target + " " + hex(c)
			if idx, ok := c.(ansi.IndexedColor); ok {
				value = fmt.Sprintf("%s indexed %d", target, idx)
			}
			fs = append(fs, field{key.String(), value})
			i += count - 1
		case n == 4 && params[i].HasMore() && i+1 < len(params):
			style := params[i+1].Param(0)
			name := "style " + strconv.Itoa(style)
			if style >= 0 && style < len(underlineStyles) {
				name = underlineStyles[style]
			}
			fs = append(fs, field{"4:" + strconv.Itoa(style), "underline " + name})
			i++
		default:
			fs = append(fs, field{strconv.Itoa(n), sgrName(n)})
		}
	}
	return fs
}

var underlineStyles = []string{"none", "single", "double", "curly", "dotted", "dashed"}

var sgrNames = map[int]string{
	0: wordReset, 1: "bold", 2: "faint", 3: "italic", 4: "underline", 5: "blink",
	6: "rapid blink", 7: "reverse", 8: "conceal", 9: "strikethrough",
	21: "double underline", 22: "normal intensity", 23: "not italic",
	24: "not underlined", 25: "not blinking", 27: "not reversed", 28: "reveal",
	29: "not strikethrough", 39: "default foreground", 49: "default background",
	53: "overline", 55: "not overlined", 59: "default underline color",
}

var colorNames = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

func sgrName(n int) string {
	switch {
	case n >= 30 && n <= 37:
		return "foreground " + colorNames[n-30]
	case n >= 40 && n <= 47:
		return "background " + colorNames[n-40]
	case n >= 90 && n <= 97:
		return "foreground bright " + colorNames[n-90]
	case n >= 100 && n <= 107:
		return "background bright " + colorNames[n-100]
	}
	if s, ok := sgrNames[n]; ok {
		return s
	}
	return "unknown"
}

var promptKinds = map[string]string{
	"A": "prompt start", "B": "command start", "C": "command executed",
	"D": "command finished", "L": "fresh line", "N": "new command", "P": "prompt",
}

var progressStates = []string{"remove", "set", wordError, "indeterminate", "paused"}

func oscFields(num, rest string) []field {
	fs := []field{{"command", num}}
	switch num {
	case "0", "1", "2":
		return append(fs, field{wordTitle, rest})
	case "7":
		return append(fs, field{"url", rest})
	case "8":
		params, uri, _ := strings.Cut(rest, ";")
		fs = append(fs, keyValues(params, ":")...)
		return append(fs, field{"uri", uri})
	case "4":
		parts := strings.Split(rest, ";")
		for i := 0; i+1 < len(parts); i += 2 {
			fs = append(fs, field{"color " + parts[i], parts[i+1]})
		}
		return fs
	case "9":
		parts := strings.Split(rest, ";")
		if len(parts) >= 2 && parts[0] == "4" {
			state := parts[1]
			if n, err := strconv.Atoi(state); err == nil && n >= 0 && n < len(progressStates) {
				state += " " + progressStates[n]
			}
			fs = append(fs, field{"state", state})
			if len(parts) >= 3 {
				fs = append(fs, field{wordProgress, parts[2] + "%"})
			}
			return fs
		}
		return append(fs, field{"message", rest})
	case "52":
		sel, data, _ := strings.Cut(rest, ";")
		fs = append(fs, field{"selection", sel})
		if data == "?" {
			return append(fs, field{wordData, "? (query)"})
		}
		return append(fs, field{wordData, unbase64(data)})
	case "133":
		kind, params, _ := strings.Cut(rest, ";")
		name := kind
		if n := promptKinds[kind]; n != "" {
			name += " " + n
		}
		fs = append(fs, field{"kind", name})
		for p := range strings.SplitSeq(params, ";") {
			switch {
			case p == "":
			case kind == "D" && !strings.Contains(p, "="):
				fs = append(fs, field{"exit status", p})
			default:
				fs = append(fs, keyValues(p, "")...)
			}
		}
		return fs
	case "7501":
		// Records are key=value pairs separated by ":", with text in base64.
		for _, f := range keyValues(rest, ":") {
			if f.Key == wordTitle || f.Key == "msg" {
				f.Value = unbase64(f.Value)
			}
			fs = append(fs, f)
		}
		return fs
	}
	for i, p := range strings.Split(rest, ";") {
		if rest != "" {
			fs = append(fs, field{"param " + strconv.Itoa(i+1), clip(p)})
		}
	}
	return fs
}

// keyValues splits s at sep into key=value fields; an empty sep takes s
// as one pair. A part without "=" is a key with no value.
func keyValues(s, sep string) []field {
	parts := []string{s}
	if sep != "" {
		parts = strings.Split(s, sep)
	}
	var fs []field
	for _, p := range parts {
		if p == "" {
			continue
		}
		k, v, _ := strings.Cut(p, "=")
		fs = append(fs, field{k, clip(v)})
	}
	return fs
}

// unbase64 decodes s, quoted so spaces and empty text show, or returns it
// as is with a note when it is not base64.
func unbase64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return clip(s) + " (not base64)"
	}
	return strconv.Quote(clip(string(b)))
}

func unhex(s string) string {
	var b []byte
	_, err := fmt.Sscanf(s, "%x", &b)
	if err != nil {
		return s
	}
	return string(b)
}

func clip(s string) string { return escape([]byte(s)) }
