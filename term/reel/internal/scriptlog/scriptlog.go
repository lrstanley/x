// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package scriptlog parses what util-linux script(1) logs: the line it
// writes at the top of a typescript, and the lines of a timing file, in
// both its classic and its advanced format. Times and terminal sizes are
// bounded as [asciicast] bounds them, so a recording made from a log can be
// written as an asciicast file.
package scriptlog

import (
	"bufio"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/internal/asciicast"
)

// header starts the line script(1) writes at the top of a typescript.
const header = "Script started on "

// SkipHeader skips the line script(1) writes at the top of a typescript,
// when br starts with it. It reads no more than it needs to tell, so it
// does not wait on a typescript without the line that starts with a few
// bytes and then blocks. The error is br's, which is [io.EOF] when the
// typescript ends within the line.
func SkipHeader(br *bufio.Reader) error {
	for n := 1; n <= len(header); n++ {
		if head, _ := br.Peek(n); string(head) != header[:n] {
			return nil
		}
	}
	_, err := br.ReadString('\n')
	return err
}

// Entry is a line of a timing file.
type Entry struct {
	// Kind is 'O', 'I', 'S', or 'H': a run of output or of input, a
	// signal, or a piece of information about the recording. A classic
	// line is 'O'.
	Kind byte
	// Delay is the time since the entry before.
	Delay time.Duration
	// Count is the number of bytes of an 'O' or 'I' entry.
	Count int64
	// Name and Value are the signal and its information of an 'S' entry,
	// and the name and value of an 'H' entry.
	Name, Value string
}

// Parse parses a timing line in either format: "DELAY COUNT", or
// "O DELAY COUNT", "I DELAY COUNT", "S DELAY SIGNAL INFO", and
// "H DELAY NAME VALUE". A delay is at most [asciicast.MaxTime].
func Parse(line string) (Entry, error) {
	e := Entry{Kind: 'O'}
	fields := strings.SplitN(line, " ", 4)
	switch fields[0] {
	case "O", "I", "S", "H":
		e.Kind, fields = fields[0][0], fields[1:]
	}
	if len(fields) < 2 || (e.Kind == 'O' || e.Kind == 'I') && len(fields) != 2 {
		return e, fmt.Errorf("malformed entry %q", line)
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(secs) || secs < 0 || secs > asciicast.MaxTime.Seconds() {
		return e, fmt.Errorf("delay %q is not a number of seconds", fields[0])
	}
	e.Delay = time.Duration(math.Round(secs * float64(time.Second)))
	switch e.Kind {
	case 'O', 'I':
		if e.Count, err = strconv.ParseInt(fields[1], 10, 64); err != nil || e.Count < 0 {
			return e, fmt.Errorf("count %q is not a number of bytes", fields[1])
		}
	default:
		e.Name = fields[1]
		if len(fields) == 3 {
			e.Value = fields[2]
		}
	}
	return e, nil
}

// ParseWinch parses the "ROWS=R COLS=C" information of a SIGWINCH entry.
func ParseWinch(info string) (cols, rows int, err error) {
	var cok, rok bool
	for field := range strings.FieldsSeq(info) {
		key, value, _ := strings.Cut(field, "=")
		switch key {
		case "ROWS":
			rows, rok = asciicast.ParseSize(value)
		case "COLS":
			cols, cok = asciicast.ParseSize(value)
		}
	}
	if !cok || !rok {
		return 0, 0, fmt.Errorf("SIGWINCH %q is not ROWS=R COLS=C, each from 1 to %d", info, asciicast.MaxSize)
	}
	return cols, rows, nil
}
