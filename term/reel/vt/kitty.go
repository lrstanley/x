// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// KittyImageFormat is the pixel layout of a direct Kitty graphics payload.
// Values match GhosttyKittyImageFormat for the formats this build stores.
type KittyImageFormat int32

// Direct image formats. PNG is accepted by the protocol and not stored:
// the freestanding build has no PNG decoder callback.
const (
	KittyImageRGB KittyImageFormat = iota
	KittyImageRGBA
	KittyImagePNG
	KittyImageGrayAlpha
	KittyImageGray
)

// KittyImageCompression matches GhosttyKittyImageCompression.
type KittyImageCompression int32

// Kitty payload compression.
const (
	KittyCompressionNone KittyImageCompression = iota
	KittyCompressionZlib
)

// KittyImage is one stored direct-transmission image. Data is a copy.
type KittyImage struct {
	// ID is the protocol image id. Zero means the terminal assigned one.
	ID uint32
	// Width is the image width in pixels.
	Width int
	// Height is the image height in pixels.
	Height int
	// Format is the pixel layout of Data.
	Format KittyImageFormat
	// Compression is how Data is stored. Only uncompressed payloads are
	// kept. zlib is accepted by the protocol and not decoded here.
	Compression KittyImageCompression
	// Data is a copy of the direct-transmission payload.
	Data []byte
}

// KittyPlacement is one image placement. Col and Row are the cursor
// cell at placement time. The freestanding libghostty build does not
// export ghostty_kitty_graphics_*, so placements are tracked here for
// direct payloads only.
type KittyPlacement struct {
	// ImageID is the image this placement draws.
	ImageID uint32
	// PlacementID distinguishes this placement from others of the same image.
	PlacementID uint32
	// Virtual is true when the placement does not occupy screen cells.
	Virtual bool
	// XOffset is the horizontal pixel offset inside the first cell.
	XOffset uint32
	// YOffset is the vertical pixel offset inside the first cell.
	YOffset uint32
	// SourceX is the left edge of the source rectangle, in pixels.
	SourceX uint32
	// SourceY is the top edge of the source rectangle, in pixels.
	SourceY uint32
	// SourceWidth is the source rectangle width. Zero means the rest of the image.
	SourceWidth uint32
	// SourceHeight is the source rectangle height. Zero means the rest of the image.
	SourceHeight uint32
	// Columns is the placement width in cells. Zero derives it from the image.
	Columns uint32
	// Rows is the placement height in cells. Zero derives it from the image.
	Rows uint32
	// Z is the layer. Negative values sit under the text.
	Z int32
	// Col is the cursor column when the placement was created.
	Col int
	// Row is the cursor row when the placement was created.
	Row int
}

// kittyImage is the stored copy of one direct payload.
type kittyImage struct {
	id     uint32
	width  int
	height int
	format KittyImageFormat
	// data is the uncompressed pixel bytes. It counts against kittyLimit.
	data []byte
}

// kittyScan is the APC parser state carried across Write chunks. Kitty
// graphics arrive as APC sequences that may be split between writes.
type kittyScan struct {
	// state is one of the kittyGround constants.
	state int
	// buf accumulates the APC body until ST.
	buf []byte
}

const (
	kittyGround = iota
	kittyEsc
	kittyAPC
	kittyBody
	kittyBodyEsc
)

// SetKittyImageStorageLimit sets how many payload bytes Kitty graphics
// may store. Zero disables storage. File, temporary-file, and shared-memory
// transmissions are ignored: this Wasm build has no filesystem.
func (t *Terminal) SetKittyImageStorageLimit(n uint64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	t.kittyLimit = n
	if n == 0 {
		t.kittyImages = nil
		t.kittyPlacements = nil
	}
	return nil
}

// KittyImages returns stored direct images, in ascending id order.
func (t *Terminal) KittyImages() []KittyImage {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.kittyImages) == 0 {
		return nil
	}
	out := make([]KittyImage, 0, len(t.kittyImages))
	for id := range t.kittyImages {
		out = append(out, t.kittyImageCopy(id))
	}
	slices.SortFunc(out, func(a, b KittyImage) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// KittyImage returns one stored image.
func (t *Terminal) KittyImage(id uint32) (KittyImage, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kittyImages[id] == nil {
		return KittyImage{}, false
	}
	return t.kittyImageCopy(id), true
}

// KittyPlacements returns placements, oldest first. The slice is a copy.
func (t *Terminal) KittyPlacements() []KittyPlacement {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]KittyPlacement(nil), t.kittyPlacements...)
}

func (t *Terminal) kittyImageCopy(id uint32) KittyImage {
	img := t.kittyImages[id]
	return KittyImage{
		ID:          img.id,
		Width:       img.width,
		Height:      img.height,
		Format:      img.format,
		Compression: KittyCompressionNone,
		Data:        append([]byte(nil), img.data...),
	}
}

func (t *Terminal) feedKitty(p []byte) {
	s := &t.kittyScan
	for _, b := range p {
		switch s.state {
		case kittyGround:
			if b == 0x1b {
				s.state = kittyEsc
			}
		case kittyEsc:
			if b == '_' {
				s.state = kittyAPC
			} else if b != 0x1b {
				s.state = kittyGround
			}
		case kittyAPC:
			if b == 'G' {
				s.state = kittyBody
				s.buf = s.buf[:0]
			} else {
				s.state = kittyGround
			}
		case kittyBody:
			switch b {
			case 0x1b:
				s.state = kittyBodyEsc
			case 0x07:
				t.execKitty(s.buf)
				s.state = kittyGround
				s.buf = s.buf[:0]
			default:
				s.buf = append(s.buf, b)
			}
		case kittyBodyEsc:
			if b == '\\' {
				t.execKitty(s.buf)
				s.state = kittyGround
				s.buf = s.buf[:0]
			} else {
				s.buf = append(s.buf, 0x1b, b)
				s.state = kittyBody
			}
		}
		if len(s.buf) > 8<<20 {
			s.state = kittyGround
			s.buf = nil
		}
	}
}

func (t *Terminal) execKitty(body []byte) {
	if t.kittyLimit == 0 || len(body) == 0 {
		return
	}
	ctrl, payload, _ := splitKitty(body)
	keys := parseKittyKeys(ctrl)
	action := keys.letter('a')
	if action == 0 {
		action = 't'
	}
	switch action {
	case 'd':
		t.deleteKitty(keys)
	case 't', 'T', 'p':
		t.placeKitty(action, keys, payload)
	}
}

func (t *Terminal) placeKitty(action byte, keys kittyKeys, payload []byte) {
	if action == 'p' {
		id := keys.u32('i')
		if t.kittyImages[id] == nil {
			return
		}
		t.kittyPlacements = append(t.kittyPlacements, t.placementFrom(keys, id))
		return
	}
	if keys.letter('t') != 0 && keys.letter('t') != 'd' {
		return
	}
	if keys.letter('o') == 'z' {
		return
	}
	format, bpp, ok := kittyFormat(keys.u32('f'))
	if !ok {
		return
	}
	raw, err := decodeKittyPayload(payload)
	if err != nil {
		return
	}
	width := int(keys.u32('s'))
	height := int(keys.u32('v'))
	if width <= 0 || height <= 0 || width*height*bpp != len(raw) {
		return
	}
	if uint64(len(raw)) > t.kittyLimit {
		return
	}
	id := keys.u32('i')
	if id == 0 {
		id = t.nextKittyID()
	}
	if t.kittyImages == nil {
		t.kittyImages = map[uint32]*kittyImage{}
	}
	var used uint64
	for existing, img := range t.kittyImages {
		if existing == id {
			continue
		}
		used += uint64(len(img.data))
	}
	if used+uint64(len(raw)) > t.kittyLimit {
		return
	}
	t.kittyImages[id] = &kittyImage{id: id, width: width, height: height, format: format, data: raw}
	if action == 'T' {
		t.kittyPlacements = append(t.kittyPlacements, t.placementFrom(keys, id))
	}
}

func (t *Terminal) placementFrom(keys kittyKeys, id uint32) KittyPlacement {
	col, row := t.cursorCell()
	return KittyPlacement{
		ImageID:      id,
		PlacementID:  keys.u32('p'),
		Virtual:      keys.u32('U') != 0,
		XOffset:      keys.u32('X'),
		YOffset:      keys.u32('Y'),
		SourceX:      keys.u32('x'),
		SourceY:      keys.u32('y'),
		SourceWidth:  keys.u32('w'),
		SourceHeight: keys.u32('h'),
		Columns:      keys.u32('c'),
		Rows:         keys.u32('r'),
		Z:            keys.i32('z'),
		Col:          col,
		Row:          row,
	}
}

func (t *Terminal) deleteKitty(keys kittyKeys) {
	switch keys.letter('d') {
	case 'a', 'A':
		t.kittyImages = nil
		t.kittyPlacements = nil
	case 'i':
		t.deleteKittyPlacement(keys.u32('i'), keys.u32('p'), false)
	case 'I':
		t.deleteKittyPlacement(keys.u32('i'), keys.u32('p'), true)
	}
}

func (t *Terminal) deleteKittyPlacement(id, placement uint32, image bool) {
	kept := t.kittyPlacements[:0]
	for _, p := range t.kittyPlacements {
		if p.ImageID == id && (placement == 0 || p.PlacementID == placement) {
			continue
		}
		kept = append(kept, p)
	}
	t.kittyPlacements = kept
	if image && placement == 0 {
		delete(t.kittyImages, id)
	}
}

func (t *Terminal) nextKittyID() uint32 {
	var id uint32 = 1
	for t.kittyImages[id] != nil {
		id++
	}
	return id
}

// cursorCell reads TerminalDataCursorX and CursorY. These are grid
// cells, not the render-state cursor. A failed get leaves that axis
// at zero. Callers use it as the default Kitty placement cell.
func (t *Terminal) cursorCell() (x, y int) {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, wasmvt.TerminalDataCursorX, t.scratch) == nil {
		x = int(t.mod.U32(t.scratch, 0))
	}
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, wasmvt.TerminalDataCursorY, t.scratch) == nil {
		y = int(t.mod.U32(t.scratch, 0))
	}
	return x, y
}

type kittyKeys map[byte]string

func (k kittyKeys) letter(key byte) byte {
	v := k[key]
	if len(v) != 1 {
		return 0
	}
	return v[0]
}

func (k kittyKeys) u32(key byte) uint32 {
	v := k[key]
	if v == "" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

func (k kittyKeys) i32(key byte) int32 {
	v := k[key]
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0
	}
	return int32(n)
}

func parseKittyKeys(ctrl []byte) kittyKeys {
	keys := kittyKeys{}
	for len(ctrl) > 0 {
		item := ctrl
		if i := bytes.IndexByte(ctrl, ','); i >= 0 {
			item = ctrl[:i]
			ctrl = ctrl[i+1:]
		} else {
			ctrl = nil
		}
		if len(item) < 2 || item[1] != '=' {
			continue
		}
		keys[item[0]] = string(item[2:])
	}
	return keys
}

func splitKitty(body []byte) (ctrl, payload []byte, ok bool) {
	for i, b := range body {
		if b == ';' {
			return body[:i], body[i+1:], true
		}
	}
	return body, nil, false
}

func decodeKittyPayload(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("vt: empty kitty payload")
	}
	enc := base64.StdEncoding
	if len(payload)%4 != 0 {
		enc = base64.RawStdEncoding
	}
	return enc.DecodeString(string(payload))
}

func kittyFormat(f uint32) (KittyImageFormat, int, bool) {
	switch f {
	case 0, 32:
		return KittyImageRGBA, 4, true
	case 24:
		return KittyImageRGB, 3, true
	case 100:
		return KittyImagePNG, 0, false
	default:
		return 0, 0, false
	}
}

func errInvalid(what string) error {
	return fmt.Errorf("vt: invalid %s", what)
}
