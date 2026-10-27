// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package ffmpeg

import (
	"bytes"
	"testing"
)

func TestMkvSizes(t *testing.T) {
	t.Parallel()

	for n, want := range map[uint64][]byte{
		0:     {0x80},
		126:   {0xFE},
		127:   {0x40, 0x7F}, // 0xFF alone means unknown size
		16382: {0x7F, 0xFE},
		16383: {0x20, 0x3F, 0xFF},
	} {
		if got := appendMkvSize([]byte{0xAA}, n); !bytes.Equal(got, append([]byte{0xAA}, want...)) {
			t.Errorf("size %d = % x, want aa % x", n, got, want)
		}
	}
	if got := appendMkvID(nil, mkvBlock); !bytes.Equal(got, []byte{0xA1}) {
		t.Errorf("ID %x = % x", mkvBlock, got)
	}
	if got := appendMkvID([]byte{1}, mkvEBML); !bytes.Equal(got, []byte{1, 0x1A, 0x45, 0xDF, 0xA3}) {
		t.Errorf("ID %x = % x", mkvEBML, got)
	}
	if got := appendMkvUint(nil, 0); !bytes.Equal(got, []byte{0}) {
		t.Errorf("uint 0 = % x", got)
	}
	if got := appendMkvUint(nil, 1_000_000); !bytes.Equal(got, []byte{0x0F, 0x42, 0x40}) {
		t.Errorf("uint 1e6 = % x", got)
	}
}
