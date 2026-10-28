// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

// maxWebPDuration is the longest delay a WebP frame stores: 24 bits of
// milliseconds.
const maxWebPDuration = 1<<24 - 1

// fixWebP patches the loop count and last frame duration of an animated WebP
// that ffmpeg wrote. ffmpeg's webp muxer ignores the loop count for
// libwebp_anim, and older releases guess the last frame's duration from the
// others. The last frame gets whatever remains of total, which also holds
// when libwebp merged identical frames.
func fixWebP(path string, loopCount int, total time.Duration) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	var hdr [16]byte
	if _, err := f.ReadAt(hdr[:12], 0); err != nil {
		return err
	}
	if string(hdr[:4]) != "RIFF" || string(hdr[8:12]) != "WEBP" {
		return errors.New("not a WebP file")
	}
	var (
		anim, last int64 = -1, -1
		sum        time.Duration
		lastDelay  time.Duration
	)
	for off := int64(12); ; {
		if _, err := f.ReadAt(hdr[:8], off); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return err
		}
		size := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		switch string(hdr[:4]) {
		case "ANIM":
			anim = off + 8
		case "ANMF":
			// The payload starts with x, y, width-1, and height-1 (3 bytes
			// each), then a 3-byte duration in milliseconds.
			if _, err := f.ReadAt(hdr[:3], off+8+12); err != nil {
				return err
			}
			if last >= 0 {
				sum += lastDelay
			}
			last = off + 8 + 12
			lastDelay = time.Duration(int(hdr[0])|int(hdr[1])<<8|int(hdr[2])<<16) * time.Millisecond
		}
		off += 8 + size + size&1
	}
	if anim < 0 || last < 0 {
		// A single frame is written as a still image.
		return nil
	}

	var loop [2]byte
	binary.LittleEndian.PutUint16(loop[:], uint16(webpLoopCount(loopCount))) //nolint:gosec // <= 65535.
	if _, err := f.WriteAt(loop[:], anim+4); err != nil {
		return err
	}
	ms := min(max((total-sum)/time.Millisecond, 1), maxWebPDuration)
	le := []byte{byte(ms), byte(ms >> 8), byte(ms >> 16)} //nolint:gosec // ms fits in 24 bits.
	if _, err := f.WriteAt(le, last); err != nil {
		return err
	}
	return f.Close()
}
