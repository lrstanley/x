// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"io"
	"math/bits"
	"slices"
	"time"
)

// Matroska element IDs used by [mkvWriter]. IDs carry their own length
// marker, so they are written as is.
const (
	mkvEBML               = 0x1A45DFA3
	mkvEBMLVersion        = 0x4286
	mkvEBMLReadVersion    = 0x42F7
	mkvEBMLMaxIDLength    = 0x42F2
	mkvEBMLMaxSizeLength  = 0x42F3
	mkvDocType            = 0x4282
	mkvDocTypeVersion     = 0x4287
	mkvDocTypeReadVersion = 0x4285
	mkvSegment            = 0x18538067
	mkvInfo               = 0x1549A966
	mkvTimestampScale     = 0x2AD7B1
	mkvMuxingApp          = 0x4D80
	mkvWritingApp         = 0x5741
	mkvTracks             = 0x1654AE6B
	mkvTrackEntry         = 0xAE
	mkvTrackNumber        = 0xD7
	mkvTrackUID           = 0x73C5
	mkvTrackType          = 0x83
	mkvFlagLacing         = 0x9C
	mkvCodecID            = 0x86
	mkvVideo              = 0xE0
	mkvPixelWidth         = 0xB0
	mkvPixelHeight        = 0xBA
	mkvColourSpace        = 0x2EB524
	mkvCluster            = 0x1F43B675
	mkvTimestamp          = 0xE7
	mkvBlockGroup         = 0xA0
	mkvBlock              = 0xA1
	mkvBlockDuration      = 0x9B
)

// mkvTick is the Matroska timestamp unit [mkvWriter] declares. GIF delays are
// whole centiseconds, so a millisecond tick represents them exactly.
const mkvTick = time.Millisecond

// mkvUnknownSize marks an element whose size is not known up front: the
// segment, which grows for as long as frames arrive.
var mkvUnknownSize = []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// mkvWriter streams uncompressed RGBA frames as a Matroska file with one video
// track. Each frame carries its own timestamp and duration, which a pipe of
// raw frames cannot: ffmpeg reads it from stdin and keeps every frame's
// timing without frames being repeated to fill a fixed rate.
type mkvWriter struct {
	w io.Writer
	// pts is the timestamp of the next frame, in [mkvTick] units.
	pts uint64
}

// appendBE appends the low n bytes of v, most significant first.
func appendBE(b []byte, v uint64, n int) []byte {
	for i := n - 1; i >= 0; i-- {
		b = append(b, byte(v>>(8*i))) //nolint:gosec // truncation is the point.
	}
	return b
}

func appendMkvID(b []byte, id uint32) []byte {
	return appendBE(b, uint64(id), 4-bits.LeadingZeros32(id)/8)
}

func appendMkvUint(b []byte, v uint64) []byte {
	return appendBE(b, v, max(1, (bits.Len64(v)+7)/8))
}

// appendMkvSize appends n as an EBML variable-size integer, using the
// shortest form. The all-ones value of each length is reserved for unknown
// sizes, so it moves to the next length.
func appendMkvSize(b []byte, n uint64) []byte {
	length := 1
	for n >= 1<<(7*length)-1 {
		length++
	}
	return appendBE(b, n|1<<(7*length), length)
}

// mkvHeader is an element's ID and the size of its payload.
func mkvHeader(id uint32, size uint64) []byte {
	return appendMkvSize(appendMkvID(nil, id), size)
}

// mkvElem is a whole element whose payload is parts joined: child elements,
// or a single value.
func mkvElem(id uint32, parts ...[]byte) []byte {
	payload := slices.Concat(parts...)
	return append(mkvHeader(id, uint64(len(payload))), payload...)
}

func mkvUint(id uint32, v uint64) []byte   { return mkvElem(id, appendMkvUint(nil, v)) }
func mkvString(id uint32, v string) []byte { return mkvElem(id, []byte(v)) }

// writeHeader writes the EBML header, the open-ended segment, and a track for
// width×height RGBA frames.
func (m *mkvWriter) writeHeader(width, height int) error {
	_, err := m.w.Write(slices.Concat(
		mkvElem(mkvEBML,
			mkvUint(mkvEBMLVersion, 1),
			mkvUint(mkvEBMLReadVersion, 1),
			mkvUint(mkvEBMLMaxIDLength, 4),
			mkvUint(mkvEBMLMaxSizeLength, 8),
			mkvString(mkvDocType, "matroska"),
			mkvUint(mkvDocTypeVersion, 4),
			mkvUint(mkvDocTypeReadVersion, 2),
		),
		appendMkvID(nil, mkvSegment), mkvUnknownSize,
		mkvElem(mkvInfo,
			mkvUint(mkvTimestampScale, uint64(mkvTick)),
			mkvString(mkvMuxingApp, "still/export"),
			mkvString(mkvWritingApp, "still/export"),
		),
		mkvElem(mkvTracks, mkvElem(mkvTrackEntry,
			mkvUint(mkvTrackNumber, 1),
			mkvUint(mkvTrackUID, 1),
			mkvUint(mkvTrackType, 1), // video
			mkvUint(mkvFlagLacing, 0),
			mkvString(mkvCodecID, "V_UNCOMPRESSED"),
			mkvElem(mkvVideo,
				mkvUint(mkvPixelWidth, uint64(width)),   //nolint:gosec // bounds are positive.
				mkvUint(mkvPixelHeight, uint64(height)), //nolint:gosec // bounds are positive.
				mkvString(mkvColourSpace, "RGBA"),
			),
		)),
	))
	return err
}

// writeFrame writes one frame of size bytes, written to w by pixels, that
// plays for delay. Each frame is its own cluster, so its timestamp is absolute
// and never overflows a block's 16-bit relative timestamp. The pixels are
// streamed between the element headers rather than joined into them.
func (m *mkvWriter) writeFrame(size int, delay time.Duration, pixels func(w io.Writer) error) error {
	ticks := uint64(max(delay/mkvTick, 1)) //nolint:gosec // positive.
	timestamp := mkvUint(mkvTimestamp, m.pts)
	duration := mkvUint(mkvBlockDuration, ticks)
	blockLen := uint64(4 + size) //nolint:gosec // positive.
	block := mkvHeader(mkvBlock, blockLen)
	groupLen := uint64(len(block)) + blockLen + uint64(len(duration))
	group := mkvHeader(mkvBlockGroup, groupLen)
	clusterLen := uint64(len(timestamp)+len(group)) + groupLen

	head := slices.Concat(
		mkvHeader(mkvCluster, clusterLen), timestamp, group, block,
		[]byte{0x81, 0, 0, 0}, // track 1, relative timestamp 0, no flags.
	)
	if _, err := m.w.Write(head); err != nil {
		return err
	}
	if err := pixels(m.w); err != nil {
		return err
	}
	if _, err := m.w.Write(duration); err != nil {
		return err
	}
	m.pts += ticks
	return nil
}
