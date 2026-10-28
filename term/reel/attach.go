// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/internal/asciicast"
	"github.com/lrstanley/x/term/reel/internal/scriptlog"
)

// attachChunk is the most [Attach] reads per write to the session.
const attachChunk = 32 * 1024

// AttachOption configures [Attach].
type AttachOption func(*attachConfig)

type attachConfig struct {
	timing io.Reader
}

// WithTiming times the bytes [Attach] writes by the script(1) timing file
// read from r, rather than by when they arrive.
func WithTiming(r io.Reader) AttachOption {
	return func(cfg *attachConfig) { cfg.timing = r }
}

// Attach writes the bytes read from r to the session as the output of a
// program that ran elsewhere, until r reaches EOF (not an error). Without
// [WithTiming], each read is written and recorded as it arrives. ctx is
// checked between reads; close r (or the timing file) to unblock a read.
//
// With [WithTiming], r is a script(1) typescript. Attach writes as fast as
// it reads, without waiting, and stamps each event at its time in the
// timing file, offset from the session's clock when Attach started. While
// it runs, that file time is the session's clock, so anything else appended
// meanwhile (such as a [Session.Mark]) is stamped at the last entry read;
// afterwards the clock continues from the file's last time. Only one timed
// Attach can run on a session at once.
//
// Both util-linux timing formats are read:
//
//   - Classic ("script -t"): "DELAY COUNT" per run of output.
//   - Advanced ("script -T", e.g. with --log-io): "O DELAY COUNT" output,
//     "I DELAY COUNT" input, "S DELAY SIGNAL INFO" signals, and
//     "H DELAY NAME VALUE" header info.
//
// DELAY is seconds since the previous entry. Input is recorded as [Input]
// events (not sent anywhere) only when the H entries name the same file for
// input and output logs; otherwise it is skipped. SIGWINCH entries resize
// the session and EXIT_CODE is recorded as an [Exit] event; other signals
// and info are ignored.
//
// The leading "Script started on ..." line and any bytes past the last
// counted run (such as "Script done on ...") are skipped. A malformed timing
// line, or a typescript shorter than the timing file counts, is an error
// naming the line.
func Attach(ctx context.Context, s *Session, r io.Reader, opts ...AttachOption) error {
	var cfg attachConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.timing != nil {
		return attachTimed(ctx, s, r, cfg.timing)
	}
	buf := make([]byte, attachChunk)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := s.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return fmt.Errorf("reel: Attach: %w", err)
		}
	}
}

// attachTimed is [Attach] with a timing file.
func attachTimed(ctx context.Context, s *Session, r, timing io.Reader) error {
	tl := s.Timeline()
	base, ok := tl.pace()
	if !ok {
		return errors.New("reel: Attach: a timed Attach is already running on the session")
	}
	defer tl.unpace()

	br := bufio.NewReaderSize(r, attachChunk)
	if err := scriptlog.SkipHeader(br); err != nil {
		return fmt.Errorf("reel: Attach: typescript: %w", unexpectedEOF(err))
	}
	a := &attacher{s: s, r: br, buf: make([]byte, attachChunk)}

	sc := bufio.NewScanner(timing)
	sc.Buffer(nil, 1<<20)
	var at time.Duration
	for n := 1; sc.Scan(); n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		e, err := scriptlog.Parse(line)
		if err == nil && e.Delay > asciicast.MaxTime-at {
			err = errors.New("time is out of range")
		}
		if err != nil {
			return fmt.Errorf("reel: Attach: timing line %d: %w", n, err)
		}
		at += e.Delay
		tl.setPace(base + at)
		if err = a.apply(e); err != nil {
			return fmt.Errorf("reel: Attach: timing line %d: %w", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reel: Attach: reading timing: %w", err)
	}
	return nil
}

// attacher applies the entries of a timing file to a session.
type attacher struct {
	s   *Session
	r   *bufio.Reader
	buf []byte
	// outputLog and inputLog are the files the H entries name; they are
	// equal when the typescript holds the input too.
	outputLog, inputLog string
}

// apply applies e at the session's paced time.
func (a *attacher) apply(e scriptlog.Entry) error {
	switch e.Kind {
	case 'O':
		return a.copy(e.Count, func(p []byte) error {
			_, err := a.s.Write(p)
			return err
		})
	case 'I':
		if a.inputLog == "" || a.inputLog != a.outputLog {
			return nil
		}
		return a.copy(e.Count, func(p []byte) error {
			data := bytes.Clone(p)
			return a.s.change(func() (Event, error) { return Input{Data: data}, nil })
		})
	case 'S':
		if e.Name != "SIGWINCH" {
			return nil
		}
		cols, rows, err := scriptlog.ParseWinch(e.Value)
		if err != nil {
			return err
		}
		return a.s.Resize(cols, rows)
	case 'H':
		switch e.Name {
		case "OUTPUT_LOG":
			a.outputLog = e.Value
		case "INPUT_LOG":
			a.inputLog = e.Value
		case "EXIT_CODE":
			status, err := strconv.Atoi(e.Value)
			if err != nil {
				return fmt.Errorf("exit code %q is not a number", e.Value)
			}
			return a.s.change(func() (Event, error) { return Exit{Status: status}, nil })
		}
	}
	return nil
}

// copy reads n bytes of the typescript and passes them to write in chunks
// of at most attachChunk bytes.
func (a *attacher) copy(n int64, write func([]byte) error) error {
	for n > 0 {
		m, err := io.ReadFull(a.r, a.buf[:min(n, int64(len(a.buf)))])
		if m > 0 {
			if werr := write(a.buf[:m]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return fmt.Errorf("typescript: %w", unexpectedEOF(err))
		}
		n -= int64(m)
	}
	return nil
}

// unexpectedEOF maps [io.EOF] to [io.ErrUnexpectedEOF].
func unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
