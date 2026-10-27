// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package sink

import (
	"image"
	"testing"
	"time"
)

func storedBytes(rec *gifRecording) int64 {
	var n int64
	for _, img := range rec.images {
		n += int64(len(img.Pix))
	}
	return n
}

func TestGIFMaxBytesRetention(t *testing.T) {
	t.Parallel()

	const n = 8
	frames := make([]image.Image, n)
	for i := range n {
		frames[i] = solidFrame(i, 4, 4) // 16 palettized bytes, all of it changes each frame.
	}
	tests := []struct {
		name string
		opts []Option
	}{
		{"delta", nil},
		{"full frames", []Option{WithOptimize(OptimizeFrames)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := t.TempDir() + "/max-bytes.gif"
			opts := append([]Option{WithMaxBytes(16 * 3)}, tt.opts...)
			if err := recordViaChannel(path, frames, 50*time.Millisecond, opts...); err != nil {
				t.Fatal(err)
			}
			g, err := gifDecodeFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Image) != 3 {
				t.Fatalf("got %d frames, want 3 within 48 bytes", len(g.Image))
			}
			replayed, err := replayGIFFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 3 {
				assertPalettizedSimilar(t, frames[n-3+i], replayed[i])
			}
		})
	}
}

func TestGIFMaxBytesKeepsNewestFrame(t *testing.T) {
	t.Parallel()

	frames := []image.Image{solidFrame(1, 4, 4), solidFrame(2, 4, 4), solidFrame(3, 4, 4)}
	path := t.TempDir() + "/max-bytes-tiny.gif"
	if err := recordViaChannel(path, frames, 50*time.Millisecond, WithMaxBytes(1)); err != nil {
		t.Fatal(err)
	}
	replayed, err := replayGIFFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 {
		t.Fatalf("got %d frames, want only the newest", len(replayed))
	}
	assertPalettizedSimilar(t, frames[2], replayed[0])
}

func TestGIFMaxBytesUnlimited(t *testing.T) {
	t.Parallel()

	const n = 8
	frames := make([]image.Image, n)
	for i := range n {
		frames[i] = solidFrame(i, 4, 4)
	}
	for _, limit := range []int64{0, -5} {
		path := t.TempDir() + "/max-bytes-unlimited.gif"
		if err := recordViaChannel(path, frames, 50*time.Millisecond, WithMaxBytes(limit)); err != nil {
			t.Fatal(err)
		}
		g, err := gifDecodeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Image) != n {
			t.Fatalf("WithMaxBytes(%d): got %d frames, want %d", limit, len(g.Image), n)
		}
	}
}

// TestGIFMaxBytesAccounting checks rec.bytes tracks the stored pixels through
// every path that adds or drops a frame, including a canvas size change.
func TestGIFMaxBytesAccounting(t *testing.T) {
	t.Parallel()

	for _, optimize := range []OptimizeFlags{OptimizeNone, OptimizeFrames, OptimizeAll} {
		for _, maxFrames := range []int{0, 3} {
			for _, maxBytes := range []int64{0, 40, 1000} {
				rec := &gifRecording{maxFrames: maxFrames, maxBytes: maxBytes}
				for i := range 20 {
					size := 4
					if i >= 12 {
						size = 5 // Leaves delta mode.
					}
					frame := solidFrame(i%4, size, size)
					if i%5 == 0 { // A partial change, so deltas are smaller than a frame.
						frame.SetNRGBA(1, 1, solidFrame(i+1, 1, 1).NRGBAAt(0, 0))
					}
					err := appendRecordingFrame(rec, defaultGIFPalette(), true, optimize, nil, false, false, frame, time.Millisecond)
					if err != nil {
						t.Fatal(err)
					}
					if got := storedBytes(rec); got != rec.bytes {
						t.Fatalf("optimize=%v frames=%d bytes=%d step %d: rec.bytes = %d, stored %d", optimize, maxFrames, maxBytes, i, rec.bytes, got)
					}
					if len(rec.images) != len(rec.delays) {
						t.Fatalf("%d images but %d delays", len(rec.images), len(rec.delays))
					}
					if maxFrames > 0 && len(rec.images) > maxFrames {
						t.Fatalf("kept %d frames, limit %d", len(rec.images), maxFrames)
					}
					if maxBytes > 0 && len(rec.images) > 1 && rec.bytes > maxBytes {
						t.Fatalf("holds %d bytes across %d frames, limit %d", rec.bytes, len(rec.images), maxBytes)
					}
				}
			}
		}
	}
}
