// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// QueryKind is the kind of the [Annotation] a [Session] records for each
// query the program writes to its terminal, whose fields are a [Query].
const QueryKind = "reel.query"

// Query is a query the program wrote to its terminal, such as for its device
// attributes or its background color, and the terminal's reply: the fields
// of a [QueryKind] annotation. The program reads the reply as input, which
// the timeline does not record, since replaying the program's output makes
// the terminal reply again. The annotation follows the [Output] event that
// held the query.
type Query struct {
	// Query names what the program asked for: "enq" (ENQ), "da1", "da2",
	// and "da3" (primary, secondary, and tertiary device attributes),
	// "xtversion" (the terminal's name and version), "dsr" (device status),
	// "cpr" (the cursor position), "color_scheme" (dark or light), "decrqm"
	// (a mode), "osc_color" (a dynamic or palette color), "osc52" (the
	// clipboard), "kitty_keyboard" (the kitty keyboard protocol flags),
	// "decrqss" (a setting), "xtgettcap" (terminfo capabilities),
	// "xtwinops" (the window's size or state), "decrqcra" (a checksum of a
	// rectangle of cells), or "unknown", for a reply to no query reel knows.
	Query string `json:"query"`
	// Param is what the query asked about, where it names one: the mode
	// of "decrqm", with "?" before a DEC private mode, such as "?2026"; the
	// OSC number of "osc_color", such as "11" for the background; the
	// selection of "osc52"; the setting of "decrqss"; the capability names
	// of "xtgettcap", joined by ";"; the operation of "xtwinops", such as
	// "14"; and the status asked for by "dsr" and "cpr", such as "5", "6",
	// or "?6".
	Param string `json:"param,omitzero"`
	// Request is the query as the program wrote it. It is empty for a reply
	// whose query was not found in the output that produced it, such as a
	// query split across two writes.
	Request string `json:"request,omitzero"`
	// Reply is what the terminal answered, every reply in the order written
	// when the query asked for more than one thing, such as two palette
	// colors. It is empty when the terminal did not answer.
	Reply string `json:"reply,omitzero"`
	// Truncated is set when Request or Reply was longer than 4 KiB, and cut
	// to it.
	Truncated bool `json:"truncated,omitzero"`
}

// queries returns the queries in out, output the program wrote, matched
// with replies, which the terminal wrote while it parsed out, oldest first.
// Each reply goes to the first query of its kind that has none, else to the
// last query of its kind, else it is a query of its own, without a request.
func queries(out, replies []byte) []Query {
	var qs []Query
	scanSeqs(out, func(s ctlSeq) {
		if q, ok := requestOf(s); ok {
			q.Request = string(s.raw)
			qs = append(qs, q)
		}
	})
	scanSeqs(replies, func(s ctlSeq) {
		r := replyOf(s)
		match := func(q Query) bool {
			return q.Query == r.Query && (r.Param == "" || q.Param == r.Param)
		}
		i := slices.IndexFunc(qs, func(q Query) bool { return q.Reply == "" && match(q) })
		if i < 0 {
			i = lastIndexFunc(qs, match)
		}
		if i < 0 {
			qs = append(qs, r)
			i = len(qs) - 1
		}
		qs[i].Reply += string(s.raw)
	})
	for i := range qs {
		q := &qs[i]
		q.Param = ClipText(q.Param, &q.Truncated)
		q.Request = ClipText(q.Request, &q.Truncated)
		q.Reply = ClipText(q.Reply, &q.Truncated)
	}
	return qs
}

// queryAnnotation returns the [QueryKind] annotation of q, one of those
// queries returns, which [AnnotationSchema] always allows, so it is not
// checked against it. A Query always encodes, so an error is a bug.
func queryAnnotation(q Query) Annotation {
	a, err := newAnnotation(QueryKind, q)
	if err != nil {
		panic(err)
	}
	return a
}

func lastIndexFunc[E any](s []E, f func(E) bool) int {
	for i := len(s) - 1; i >= 0; i-- {
		if f(s[i]) {
			return i
		}
	}
	return -1
}

// ctlSeq is an escape sequence or control character in a stream of terminal
// bytes.
type ctlSeq struct {
	// raw is the whole sequence.
	raw []byte
	// intro is '[' for CSI, ']' for OSC, and 'P' for DCS, or the control
	// character.
	intro byte
	// marker, params, and inter are a CSI or DCS sequence's private marker
	// ("<", "=", ">", or "?"), parameters, and intermediate bytes, and
	// final its final byte.
	marker, params, inter string
	final                 byte
	// data is the text of an OSC sequence, or of a DCS sequence after its
	// final byte. It is a slice of the scanned bytes, since output can hold
	// many OSC sequences, such as hyperlinks, that are no query or reply.
	data []byte
}

// firstParam returns the first of the sequence's parameters.
func (s ctlSeq) firstParam() string {
	p, _, _ := strings.Cut(s.params, ";")
	return p
}

// queryFinals are the final bytes of the CSI sequences [requestOf] and
// [replyOf] classify.
var queryFinals = []byte("Rcnpqtuy")

// scanSeqs calls yield for each CSI sequence in b that ends in one of
// queryFinals, each OSC and DCS sequence, and each ENQ and ACK. A sequence
// cut off by the end of b is skipped.
func scanSeqs(b []byte, yield func(ctlSeq)) {
	for {
		i := bytes.IndexAny(b, "\x1b\x05\x06")
		if i < 0 {
			return
		}
		b = b[i:]
		if b[0] != 0x1b {
			yield(ctlSeq{raw: b[:1], intro: b[0]})
			b = b[1:]
			continue
		}
		if len(b) < 2 {
			return
		}
		switch b[1] {
		case '[':
			head, rest := splitHead(b[2:])
			if len(rest) == 0 {
				return
			}
			if rest[0] < 0x40 || rest[0] > 0x7e {
				b = rest
				continue
			}
			n := len(b) - len(rest) + 1
			// Only these finals end a query or a reply, so the rest, such
			// as the colors and cursor moves of most output, are skipped
			// without parsing them.
			if bytes.IndexByte(queryFinals, rest[0]) < 0 {
				b = b[n:]
				continue
			}
			s := ctlSeq{raw: b[:n], intro: '[', final: rest[0]}
			s.marker, s.params, s.inter = parseHead(head)
			yield(s)
			b = b[n:]
		case ']', 'P':
			body, n, ok := stringBody(b)
			if !ok {
				if n == 0 {
					return
				}
				b = b[n:]
				continue
			}
			s := ctlSeq{raw: b[:n], intro: b[1]}
			if b[1] == ']' {
				s.data = body
			} else {
				head, rest := splitHead(body)
				if len(rest) == 0 {
					b = b[n:]
					continue
				}
				s.marker, s.params, s.inter = parseHead(head)
				s.final, s.data = rest[0], rest[1:]
			}
			yield(s)
			b = b[n:]
		default:
			b = b[1:]
		}
	}
}

// splitHead splits the parameter and intermediate bytes off the start of a
// CSI or DCS sequence, after its introducer.
func splitHead(b []byte) (head, rest []byte) {
	i := 0
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
		i++
	}
	return b[:i], b[i:]
}

// parseHead splits the head of a CSI or DCS sequence into its private
// marker, its parameters, and its intermediate bytes.
func parseHead(head []byte) (marker, params, inter string) {
	s := string(head)
	if s != "" && strings.ContainsRune("<=>?", rune(s[0])) {
		marker, s = s[:1], s[1:]
	}
	i := strings.IndexFunc(s, func(r rune) bool { return r < 0x30 })
	if i < 0 {
		return marker, s, ""
	}
	return marker, s[:i], s[i:]
}

// stringBody returns the text of the OSC or DCS sequence that b starts
// with, between its introducer and its terminator, BEL or ST, and the
// sequence's length. It reports false for a sequence another escape cuts
// short, whose length is then that of what precedes the escape, or for one
// b ends before the terminator of, whose length is then 0.
func stringBody(b []byte) (body []byte, n int, ok bool) {
	for i := 2; i < len(b); i++ {
		switch {
		case b[i] == 0x07 && b[1] == ']':
			return b[2:i], i + 1, true
		case b[i] == 0x1b:
			if i+1 == len(b) {
				return nil, 0, false
			}
			if b[i+1] == '\\' {
				return b[2:i], i + 2, true
			}
			return nil, i, false
		}
	}
	return nil, 0, false
}

// requestOf returns the query s asks, if it is one.
func requestOf(s ctlSeq) (Query, bool) {
	switch s.intro {
	case 0x05:
		return Query{Query: "enq"}, true
	case '[':
		return csiRequest(s)
	case ']':
		num, rest, _ := bytes.Cut(s.data, []byte(";"))
		switch {
		case isColorOSC(num) && bytes.IndexByte(rest, '?') >= 0:
			return Query{Query: "osc_color", Param: string(num)}, true
		case string(num) == "52" && bytes.HasSuffix(rest, []byte(";?")):
			sel, _, _ := bytes.Cut(rest, []byte(";"))
			return Query{Query: "osc52", Param: string(sel)}, true
		}
	case 'P':
		if s.marker != "" || s.params != "" || s.final != 'q' {
			break
		}
		switch s.inter {
		case "$":
			return Query{Query: "decrqss", Param: string(s.data)}, true
		case "+":
			return Query{Query: "xtgettcap", Param: capNames(string(s.data))}, true
		}
	}
	return Query{}, false
}

// csiRequest returns the query the CSI sequence s asks, if it is one.
func csiRequest(s ctlSeq) (Query, bool) {
	switch {
	case s.final == 'c' && s.inter == "" && (s.params == "" || s.params == "0"):
		switch s.marker {
		case "":
			return Query{Query: "da1"}, true
		case ">":
			return Query{Query: "da2"}, true
		case "=":
			return Query{Query: "da3"}, true
		}
	case s.final == 'q' && s.marker == ">" && s.inter == "" && (s.params == "" || s.params == "0"):
		return Query{Query: "xtversion"}, true
	case s.final == 'n' && s.inter == "" && (s.marker == "" || s.marker == "?") && s.params != "":
		switch {
		case s.params == "6":
			return Query{Query: "cpr", Param: s.marker + s.params}, true
		case s.marker == "?" && s.params == "996":
			return Query{Query: "color_scheme"}, true
		default:
			return Query{Query: "dsr", Param: s.marker + s.params}, true
		}
	case s.final == 'p' && s.inter == "$" && (s.marker == "" || s.marker == "?"):
		return Query{Query: "decrqm", Param: s.marker + s.params}, true
	case s.final == 'u' && s.marker == "?" && s.params == "" && s.inter == "":
		return Query{Query: "kitty_keyboard"}, true
	case s.final == 't' && s.marker == "" && s.inter == "":
		switch op := s.firstParam(); op {
		case "11", "13", "14", "15", "16", "18", "19", "20", "21":
			return Query{Query: "xtwinops", Param: op}, true
		}
	case s.final == 'y' && s.inter == "*" && s.marker == "":
		return Query{Query: "decrqcra"}, true
	}
	return Query{}, false
}

// replyOf returns the query s answers, with the parameter the reply names,
// if any.
func replyOf(s ctlSeq) Query {
	switch s.intro {
	case 0x06:
		return Query{Query: "enq"}
	case '[':
		switch {
		case s.final == 'c' && s.marker == "?":
			return Query{Query: "da1"}
		case s.final == 'c' && s.marker == ">":
			return Query{Query: "da2"}
		case s.final == 'R':
			return Query{Query: "cpr"}
		case s.final == 'n' && s.marker == "?" && s.firstParam() == "997":
			return Query{Query: "color_scheme"}
		case s.final == 'n':
			return Query{Query: "dsr"}
		case s.final == 'y' && s.inter == "$":
			return Query{Query: "decrqm", Param: s.marker + s.firstParam()}
		case s.final == 'u' && s.marker == "?":
			return Query{Query: "kitty_keyboard"}
		case s.final == 't':
			// A report's first parameter is its operation's less ten, but
			// for the window state, reported as 1 or 2.
			op, _ := strconv.Atoi(s.firstParam())
			if op == 2 {
				op = 1
			}
			if op > 0 && op < 10 {
				return Query{Query: "xtwinops", Param: strconv.Itoa(op + 10)}
			}
			return Query{Query: "xtwinops"}
		}
	case ']':
		num, rest, _ := bytes.Cut(s.data, []byte(";"))
		switch {
		case isColorOSC(num):
			return Query{Query: "osc_color", Param: string(num)}
		case string(num) == "52":
			sel, _, _ := bytes.Cut(rest, []byte(";"))
			return Query{Query: "osc52", Param: string(sel)}
		}
	case 'P':
		switch {
		case s.marker == ">" && s.final == '|':
			return Query{Query: "xtversion"}
		case s.inter == "!" && s.final == '|':
			return Query{Query: "da3"}
		case s.inter == "!" && s.final == '~':
			return Query{Query: "decrqcra"}
		case s.inter == "$" && s.final == 'r':
			return Query{Query: "decrqss"}
		case s.inter == "+" && s.final == 'r':
			return Query{Query: "xtgettcap"}
		}
	}
	return Query{Query: "unknown"}
}

// isColorOSC reports whether num is the number of an OSC sequence that sets
// or reports a color: 4 for the palette, or 10 to 19 for the dynamic colors.
func isColorOSC(num []byte) bool {
	switch string(num) {
	case "4", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19":
		return true
	}
	return false
}

// capNames returns the hex encoded capability names of an XTGETTCAP query,
// decoded and joined by ";", or as they are when they do not decode.
func capNames(data string) string {
	names := strings.Split(data, ";")
	for i, name := range names {
		b, err := hex.DecodeString(name)
		if err != nil {
			return data
		}
		names[i] = string(b)
	}
	return strings.Join(names, ";")
}
