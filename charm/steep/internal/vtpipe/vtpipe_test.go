// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vtpipe_test

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lrstanley/x/charm/steep/internal/vtpipe"
)

func TestPipeWriteReachesSink(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := vtpipe.New(&buf, io.LimitReader(bytes.NewReader(nil), 0))
	defer func() { _ = p.Close() }()

	const want = "hello"
	if _, err := p.WriteString(want); err != nil {
		t.Fatal(err)
	}
	// Sink writes happen in pumpSink asynchronously; flush the bridge before reading sink.
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != want {
		t.Fatalf("sink got %q, want %q", got, want)
	}
}

func TestPipeReadsFromSrc(t *testing.T) {
	t.Parallel()

	src := strings.NewReader("stdin-bytes")
	p := vtpipe.New(io.Discard, src)
	t.Cleanup(func() { _ = p.Close() })

	data, err := io.ReadAll(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "stdin-bytes" {
		t.Fatalf("read got %q, want stdin-bytes", data)
	}
}

func TestPipeCloseIdempotentWait(t *testing.T) {
	t.Parallel()

	p := vtpipe.New(io.Discard, io.LimitReader(bytes.NewReader(nil), 0))

	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPipeAfterOutgoingClosedRunsBeforePumpEnd(t *testing.T) {
	t.Parallel()

	src := newBlockingSrc()
	out := strings.Builder{}

	var hook atomic.Bool

	p := vtpipe.New(
		&out,
		src,
		vtpipe.WithAfterOutgoingClosed(func() {
			hook.Store(true)
			src.unblock()
		}),
	)

	done := make(chan struct{})
	go func() {
		_, _ = p.WriteString("x")
		_ = p.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		src.unblock()
		t.Fatal("Close blocked unexpectedly")
	}
	if !hook.Load() {
		t.Fatal("expected WithAfterOutgoingClosed hook to run")
	}
	if out.String() != "x" {
		t.Fatalf(`sink got %q, want "x"`, out.String())
	}
}

type blockingSrc struct {
	once sync.Once
	ch   chan struct{}
}

func newBlockingSrc() *blockingSrc {
	return &blockingSrc{ch: make(chan struct{})}
}

func (b *blockingSrc) Read(_ []byte) (int, error) {
	<-b.ch
	return 0, io.EOF
}

func (b *blockingSrc) unblock() {
	b.once.Do(func() { close(b.ch) })
}
