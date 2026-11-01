// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// RecCmd records a command in this terminal.
type RecCmd struct {
	RenderFlags `embed:""`

	Command []string `arg:"" optional:"" placeholder:"COMMAND" help:"Program to record, and its arguments (after --); defaults to $SHELL."`
	Prefix  string   `name:"prefix" default:"ctrl+]" placeholder:"KEY" help:"Key that starts a hotkey: then m drops a mark, and the prefix again sends it to the program."`
}

// Run records the command until it exits, then writes the outputs.
func (c *RecCmd) Run(ctx context.Context) error {
	in, out := os.Stdin.Fd(), os.Stdout.Fd()
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return errors.New("rec needs a terminal; to drive a program without one, use \"reel session\"")
	}
	prefix, err := vt.ParseKey(c.Prefix)
	if err != nil {
		return fmt.Errorf("--prefix: %w", err)
	}
	p, err := c.profile()
	if err != nil {
		return err
	}
	argv := c.Command
	if len(argv) == 0 {
		argv = []string{cmp.Or(os.Getenv("SHELL"), "sh")}
	}
	cols, rows, err := term.GetSize(out)
	if err != nil {
		return err
	}
	if cols < 1 || rows < 1 {
		cols, rows = 80, 24 // A terminal that does not know its size.
	}
	s, err := reel.Open(ctx, cols, rows, reel.WithTitle(filepath.Base(argv[0])))
	if err != nil {
		return err
	}
	defer s.Close()
	proc, err := reel.Exec(ctx, s, exec.Command(argv[0], argv[1:]...)) //nolint:gosec,noctx // Recording it is the point; ctx is the session's.
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "reel: recording %s (%dx%d); %s m drops a mark; exit the program to stop\n", argv[0], cols, rows, c.Prefix)

	state, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	stdin, stopInput, err := interruptible()
	if err != nil {
		return errors.Join(err, term.Restore(in, state))
	}
	// Each worker is stopped and joined once the program exits, before the
	// terminal is restored, so none outlives the recording.
	var inputWG sync.WaitGroup
	inputCtx, cancelInput := context.WithCancel(ctx)
	inputWG.Go(func() { forward(inputCtx, stdin, s, prefix) })
	mirrorCtx, stopMirror := context.WithCancel(ctx)
	var mirrorWG sync.WaitGroup
	mirrorWG.Go(func() { mirror(mirrorCtx, s.Timeline(), os.Stdout) })
	stopResize := onResize(func() {
		if w, h, sizeErr := term.GetSize(out); sizeErr == nil && w > 0 && h > 0 {
			_ = s.Resize(w, h)
		}
	})
	status, waitErr := proc.Wait()
	cancelInput()
	stopInput()
	if canInterrupt {
		inputWG.Wait()
	}
	stopResize()
	stopMirror() // Wait returns once the output is in the timeline.
	mirrorWG.Wait()
	_ = s.Close()
	restoreErr := term.Restore(in, state)
	fmt.Fprintf(os.Stderr, "reel: %s exited with status %d\n", argv[0], status)
	if waitErr != nil && ctx.Err() == nil {
		return errors.Join(waitErr, restoreErr)
	}

	err = reel.Export(context.WithoutCancel(ctx), s.Timeline(), p)
	if err != nil {
		// Keep the recording, in a file of its own, so it can be exported
		// again.
		if saved, saveErr := saveRecording(s.Timeline()); saveErr == nil {
			err = fmt.Errorf("%w (the recording is saved as %s)", err, saved)
		}
	}
	return errors.Join(err, restoreErr)
}

// mirror writes the program's output in tl to w as it is recorded, until
// ctx is done, and then what is left, which is all of it once the
// program's [reel.Process.Wait] returns.
func mirror(ctx context.Context, tl *reel.Timeline, w io.Writer) {
	read := 0
	for {
		updated := tl.Updated()
		for ev := range tl.EventsFrom(read) {
			read++
			if out, ok := ev.(reel.Output); ok {
				_, _ = w.Write(out.Data)
			}
		}
		select {
		case <-updated:
		case <-ctx.Done():
			for ev := range tl.EventsFrom(read) {
				if out, ok := ev.(reel.Output); ok {
					_, _ = w.Write(out.Data)
				}
			}
			return
		}
	}
}

// forward sends what is typed in r to s, until ctx is done or r fails, as
// keys, text, pastes, and mouse events, which the session encodes for the
// program as it asked. Replies to queries the program's mirrored output
// made of the host's terminal decode as nothing, so the program only gets
// reel's terminal's replies. The prefix key starts a hotkey.
func forward(ctx context.Context, r io.Reader, s *reel.Session, prefix vt.Key) {
	buf := make([]byte, 64<<10)
	var pending []byte
	marks := 0
	held := false
	for {
		n, err := r.Read(buf)
		if err != nil || ctx.Err() != nil {
			return
		}
		var data []byte
		data, pending = splitInput(append(pending, buf[:n]...))
		for _, st := range vt.DecodeInput(data) {
			if !held && st.Kind == vt.StrokeKey && st.Key.String() == prefix.String() {
				held = true
				continue
			}
			if held {
				held = false
				if rest, ok := strings.CutPrefix(st.Text, "m"); ok && st.Kind == vt.StrokeText {
					marks++
					_ = s.Mark(fmt.Sprintf("mark:%d", marks))
					if rest == "" {
						continue
					}
					st.Text = rest
				}
			}
			send(s, st)
		}
	}
}

// send sends a stroke to s.
func send(s *reel.Session, st vt.Stroke) {
	switch st.Kind {
	case vt.StrokeKey:
		_ = s.Key(st.Key.String())
	case vt.StrokeText:
		_ = s.Type(st.Text)
	case vt.StrokePaste:
		_ = s.Paste(st.Text)
	case vt.StrokeMouse:
		_ = s.Mouse(st.Mouse.Action, st.Mouse.Button, st.Mouse.X, st.Mouse.Y)
	}
}

// splitInput splits data into what decodes now and the tail a later read
// completes: a bracketed paste without its end, a control sequence without
// its final byte, or part of a UTF-8 character. A lone escape at the end is
// the Escape key, as terminals send it.
func splitInput(data []byte) (complete, tail []byte) {
	if i := bytes.LastIndex(data, []byte("\x1b[200~")); i >= 0 && !bytes.Contains(data[i:], []byte("\x1b[201~")) {
		return data[:i], slices.Clone(data[i:])
	}
	if i := bytes.LastIndexByte(data, 0x1b); i >= 0 && i+1 < len(data) && openSequence(data[i+1:]) {
		return data[:i], slices.Clone(data[i:])
	}
	for n := 1; n <= min(3, len(data)); n++ {
		if utf8.RuneStart(data[len(data)-n]) {
			if !utf8.FullRune(data[len(data)-n:]) {
				return data[:len(data)-n], slices.Clone(data[len(data)-n:])
			}
			break
		}
	}
	return data, nil
}

// openSequence reports whether seq, what follows an escape, is a CSI or
// SS3 sequence that its final byte has not ended yet.
func openSequence(seq []byte) bool {
	switch seq[0] {
	case 'O':
		return len(seq) == 1
	case '[':
		for _, b := range seq[1:] {
			if b >= 0x40 && b <= 0x7e {
				return false
			}
		}
		return true
	}
	return false
}

// saveRecording writes tl to a new file in the working directory, and
// returns its name.
func saveRecording(tl *reel.Timeline) (string, error) {
	f, err := os.CreateTemp(".", "reel-rec-*.cast")
	if err != nil {
		return "", err
	}
	_, err = tl.WriteCast(f, reel.CastV2)
	return f.Name(), errors.Join(err, f.Close())
}
