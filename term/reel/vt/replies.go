// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"io"
	"sync"
)

// replyBuf is the pty reply stream. Writes come from the emulator callback
// and must not block. Reads wait until bytes arrive or Close.
type replyBuf struct {
	// mu guards buf and closed. cond waits on mu.
	mu   sync.Mutex
	cond *sync.Cond
	// buf is the unread reply bytes. Write appends. Read consumes from the front.
	buf []byte
	// closed makes Read return EOF once buf is empty. Further writes are dropped.
	closed bool
}

func newReplyBuf() *replyBuf {
	r := &replyBuf{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *replyBuf) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.buf = append(r.buf, p...)
		r.cond.Signal()
	}
	r.mu.Unlock()
}

func (r *replyBuf) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.buf) == 0 && !r.closed {
		r.cond.Wait()
	}
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = append([]byte(nil), r.buf[n:]...)
	return n, nil
}

func (r *replyBuf) pending() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf...)
}

func (r *replyBuf) Close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}
