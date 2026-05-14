// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package vtpipe bridges a program's sequential stdin/stdout to blocking reader and
// writer halves (such as vt.Emulator) using background pumps and buffered I/O.
package vtpipe

import (
	"bufio"
	"errors"
	"io"
	"sync"
)

// DefaultBufferSize is the default size for buffered program I/O and pump copies.
const DefaultBufferSize = 4096

var (
	_ io.Reader       = (*Pipe)(nil)
	_ io.Writer       = (*Pipe)(nil)
	_ io.ReadWriter   = (*Pipe)(nil)
	_ io.ReaderFrom   = (*Pipe)(nil)
	_ io.StringWriter = (*Pipe)(nil)
)

// Pipe is the program-facing end of the bridge ([io.ReadWriter]): reads come from src
// (pumped asynchronously), writes go to sink (flushed asynchronously).
type Pipe struct {
	shutdownOnce sync.Once // runs Close teardown once

	wg    sync.WaitGroup // pumpOut and pumpIn
	outMu sync.Mutex     // serializes Write/Flush/writeString on progOut

	progIn      *bufio.Reader // reads bytes produced by pumping src
	progOut     *bufio.Writer // writes flushed to sinkPipeW
	sinkPipeR   *io.PipeReader
	sinkPipeW   *io.PipeWriter // writer consumed by pumpOut
	srcPipeR    *io.PipeReader // reader end closed on shutdown to unblock pumpIn if progIn fills
	midShutdown func()         // optional, runs after closing sinkPipeW, before closing srcPipeR
	bufferSize  int
}

type config struct {
	bufferSize  int
	midShutdown func()
}

// Option configures [New].
type Option func(*config)

// WithBufferSize sets bufio sizing and pump copy buffer size (must be positive).
func WithBufferSize(size int) Option {
	return func(c *config) {
		if size > 0 {
			c.bufferSize = size
		}
	}
}

// WithAfterOutgoingClosed registers fn to run synchronously during [Pipe.Close]. It is
// invoked after flushing buffered program writes and closing the outbound pipe writer
// (so pumpOut observes EOF), but before closing the inbound pipe reader. Use it to tear
// down the far side so src can unblock (for example closing a VT input pipe so a
// [tea.Program] shutdown does not deadlock a full reverse pipe).
func WithAfterOutgoingClosed(fn func()) Option {
	return func(c *config) {
		c.midShutdown = fn
	}
}

// New returns a Pipe that exposes program stdin/stdout and pumps to sink/src.
//
// Writes on the Pipe are flushed to sink; reads receive bytes copied from src.
//
// Closing the Pipe shuts down pumps and waits until both exit; it must be invoked to
// avoid leaking goroutines. Close is safe to call more than once.
func New(sink io.Writer, src io.Reader, opts ...Option) *Pipe {
	cfg := config{bufferSize: DefaultBufferSize}
	for _, opt := range opts {
		opt(&cfg)
	}

	outR, outW := io.Pipe()
	inR, inW := io.Pipe()

	p := &Pipe{
		bufferSize:  cfg.bufferSize,
		midShutdown: cfg.midShutdown,
		progIn:      bufio.NewReaderSize(inR, cfg.bufferSize),
		progOut:     bufio.NewWriterSize(outW, cfg.bufferSize),
		sinkPipeR:   outR,
		sinkPipeW:   outW,
		srcPipeR:    inR,
	}

	p.wg.Add(2)
	go p.pumpSink(sink)
	go p.pumpSrc(src, inW)
	return p
}

func (p *Pipe) pumpSink(sink io.Writer) {
	defer p.wg.Done()
	buf := make([]byte, p.bufferSize)
	for {
		n, err := p.sinkPipeR.Read(buf)
		if n > 0 {
			_, werr := sink.Write(buf[:n])
			if werr != nil {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				_ = err // sink write path failed or pipe error; teardown path handles cleanup
			}
			return
		}
	}
}

func (p *Pipe) pumpSrc(src io.Reader, dst io.WriteCloser) {
	defer p.wg.Done()
	defer dst.Close()

	buf := make([]byte, p.bufferSize)
	_, err := io.CopyBuffer(dst, src, buf)
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		_ = err
	}
}

// Read exposes program-facing stdin backed by pumped src bytes.
func (p *Pipe) Read(b []byte) (int, error) {
	return p.progIn.Read(b)
}

// Write exposes program-facing stdout flushed through to sink via pumpSink.
func (p *Pipe) Write(b []byte) (int, error) {
	p.outMu.Lock()
	defer p.outMu.Unlock()

	n, err := p.progOut.Write(b)
	if err != nil {
		return n, err
	}
	err = p.progOut.Flush()
	return n, err
}

// WriteString writes s and flushes buffered program bytes to sink.
func (p *Pipe) WriteString(s string) (int, error) {
	p.outMu.Lock()
	defer p.outMu.Unlock()

	n, err := p.progOut.WriteString(s)
	if err != nil {
		return n, err
	}
	err = p.progOut.Flush()
	return n, err
}

// ReadFrom reads from r until EOF and forwards all bytes through the same path as Write.
func (p *Pipe) ReadFrom(r io.Reader) (int64, error) {
	p.outMu.Lock()
	defer p.outMu.Unlock()

	n, err := p.progOut.ReadFrom(r)
	if err != nil {
		return n, err
	}
	return n, p.progOut.Flush()
}

// Close stops pumps by closing pipe ends, optionally runs the mid-shutdown hook, and
// waits for background goroutines. It is safe to call more than once.
func (p *Pipe) Close() error {
	p.shutdownOnce.Do(func() {
		p.outMu.Lock()
		_ = p.progOut.Flush()
		p.outMu.Unlock()

		_ = p.sinkPipeW.Close()

		if p.midShutdown != nil {
			p.midShutdown()
		}

		if p.srcPipeR != nil {
			_ = p.srcPipeR.Close()
		}

		p.wg.Wait()
	})
	return nil
}
