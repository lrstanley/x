// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"errors"
	"io"
)

// ErrReadLimitExceeded is returned by [NewBodyLimitReader] when a read would
// exceed the configured maximum body size (more bytes remain on the reader).
var ErrReadLimitExceeded = errors.New("httpccache: read limit exceeded")

// NewBodyLimitReader wraps r so that at most max bytes are delivered. After max
// bytes have been read, the next read attempts to take one more byte from r; if
// successful, it returns (0, ErrReadLimitExceeded) instead of EOF.
func NewBodyLimitReader(r io.Reader, max int64) io.Reader {
	if max <= 0 {
		return r
	}
	return &bodyLimitReader{r: r, max: max}
}

type bodyLimitReader struct {
	r   io.Reader
	n   int64
	max int64
}

func (l *bodyLimitReader) Read(p []byte) (int, error) {
	if l.n >= l.max {
		var buf [1]byte
		n, err := l.r.Read(buf[:])
		if n > 0 {
			return 0, ErrReadLimitExceeded
		}
		return 0, err
	}
	room := l.max - l.n
	if int64(len(p)) > room {
		p = p[:room]
	}
	nr, err := l.r.Read(p)
	l.n += int64(nr)
	return nr, err
}
