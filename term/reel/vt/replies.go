// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
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

// Write queues p, and reports false when the stream is closed, which drops
// it.
func (r *replyBuf) Write(p []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	if len(p) > 0 {
		r.buf = append(r.buf, p...)
		r.cond.Signal()
	}
	return true
}

// Read waits until there are bytes to read, the stream is closed, or ctx is
// done.
func (r *replyBuf) Read(ctx context.Context, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 && !r.closed && ctx.Done() != nil {
		// Wake the wait below when ctx is done. It takes mu, so it cannot
		// run between the check of ctx and the wait.
		stop := context.AfterFunc(ctx, func() {
			r.mu.Lock()
			r.cond.Broadcast()
			r.mu.Unlock()
		})
		defer stop()
	}
	for len(r.buf) == 0 && !r.closed {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
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
