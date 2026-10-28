// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"image/color"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// asciiRunes holds interned one-byte strings for codepoints 1 through 127.
// rawCell uses them for the common single-rune ASCII path. Codepoint 0
// stays the " " literal, matching a blank cell.
var asciiRunes [128]string

func init() {
	for i := 1; i < len(asciiRunes); i++ {
		asciiRunes[i] = string(rune(i))
	}
}

// The grid is a snapshot of GhosttyRenderState, not a live view of Wasm
// memory. refreshLocked calls ghostty_render_state_update, then walks
// the row iterator. CELLS_RAW is a packed 8-byte word per cell
// (GhosttyCell). Graphemes, styles, and links are a second query
// through GhosttyRenderStateRowCells, and only for cells whose word
// says they need it. Copies leave Wasm before the next allocating call.
// Each viewport row keeps its last CELLS_RAW copy so an unchanged row
// is not unpacked again. Layout constants are the generated names in wasmvt.

// screenLocked returns the frozen frame during a synchronized-output
// hold. Otherwise it refreshes from the render state.
func (t *Terminal) screenLocked() *Buffer {
	if t.held && t.frozen != nil {
		return t.frozen
	}
	return t.refreshLocked()
}

// captureFrame clones the current grid for the hold callback. The
// clone does not alias Wasm memory.
func (t *Terminal) captureFrame() (*Buffer, Cursor) {
	grid := t.refreshLocked()
	if grid == nil {
		return nil, Cursor{}
	}
	return grid.Clone(), t.gridCursor
}

// refreshLocked pulls GhosttyRenderState into t.grid. The caller holds
// the terminal lock. A full dirty flag of 1 skips clean rows. A row
// whose GhosttyCell words match the cached copy is left clean as well.
// The row iterator handle is passed by pointer in scratch. Failure leaves
// the previous grid in place.
func (t *Terminal) refreshLocked() *Buffer {
	if rc := t.mod.Xghostty_render_state_update(t.render, t.term); rc != 0 && t.grid == nil {
		t.grid = NewBuffer(t.cols, t.rows)
		t.invalidateRowWords()
		t.gridFresh = true
		return t.grid
	}
	dirty := t.renderI32(wasmvt.RenderStateDataDirty)
	same := t.grid != nil && t.grid.Width() == t.cols && t.grid.Height() == t.rows
	if dirty == 0 && same {
		t.grid.ClearDirty()
		t.gridFresh = true
		t.alt = t.activeAlt()
		return t.grid
	}
	if !same {
		t.grid = NewBuffer(t.cols, t.rows)
		t.invalidateRowWords()
	} else {
		t.grid.ClearDirty()
	}
	t.mod.SetU32(t.scratch, 0, uint32(t.rowsIt))
	if err := wasmvt.Result("row iterator", t.mod.Xghostty_render_state_get(
		t.render, wasmvt.RenderStateDataRowIterator, t.scratch,
	)); err != nil {
		t.gridFresh = true
		return t.grid
	}
	t.prepareRowWords()
	changed := false
	for t.mod.Xghostty_render_state_row_iterator_next(t.rowsIt) != 0 {
		y := int(t.rowI32(wasmvt.RenderStateRowDataViewportY))
		if y < 0 || y >= t.rows {
			continue
		}
		rowDirty := t.rowBool(wasmvt.RenderStateRowDataDirty)
		if dirty == 1 && !rowDirty {
			continue
		}
		// Before decodeRow, which skips a row whose cells are unchanged:
		// writing past a full row wraps it without changing its cells.
		t.grid.SetWrapped(y, t.rowWrapped())
		if t.decodeRow(y) && y < len(t.grid.Dirty) {
			t.grid.Dirty[y] = true
			changed = true
		}
	}
	// A following refresh may ClearDirty before anyone draws. The
	// generation still tells that draw the cells are new.
	if changed {
		t.grid.AdvanceGeneration()
	}
	t.readCursorLocked()
	t.alt = t.activeAlt()
	t.gridFresh = true
	return t.grid
}

// decodeRow copies CELLS_RAW for viewport row y out of Wasm, then
// fills grapheme, style, and hyperlink data for cells that need it.
// The raw pointer is only valid until the next render-state call, so
// the words are copied into t.rawRow first. When those words match the
// copy kept for this row, the grid row is already current and decodeRow
// reports false so the caller leaves the row not dirty. A true result
// means the row was unpacked, or the read failed, and the caller marks
// the row dirty.
func (t *Terminal) decodeRow(y int) bool {
	view := t.scratch
	t.mod.Zero(view, 8)
	if err := wasmvt.Result("cells", t.mod.Xghostty_render_state_row_get(
		t.rowsIt, wasmvt.RenderStateRowDataCellsRaw, view,
	)); err != nil {
		return true
	}
	ptr := int32(t.mod.U32(view, 0))
	n := int(t.mod.U32(view, 4))
	if n > t.cols {
		n = t.cols
	}
	if cap(t.rawRow) < n*8 {
		t.rawRow = make([]byte, n*8)
	}
	t.rawRow = t.rawRow[:n*8]
	copy(t.rawRow, t.mod.Slice(ptr, n*8))
	raw := t.rawRow
	if t.rowCached(y, raw) {
		return false
	}
	if cap(t.rowFlags) < n {
		t.rowFlags = make([]byte, n)
	}
	t.rowFlags = t.rowFlags[:n]
	var needs byte
	for i := range n {
		cell := t.grid.CellAt(i, y)
		if cell == nil {
			t.rowFlags[i] = 0
			continue
		}
		f := rawCell(raw[i*8:i*8+8], cell)
		t.rowFlags[i] = f
		needs |= f
	}
	if needs != 0 {
		t.mod.SetU32(t.scratch, 0, uint32(t.cells))
		if err := wasmvt.Result("row cells", t.mod.Xghostty_render_state_row_get(
			t.rowsIt, wasmvt.RenderStateRowDataCells, t.scratch,
		)); err != nil {
			return true
		}
		for x := range n {
			word := t.rowFlags[x]
			if word == 0 {
				continue
			}
			if t.mod.Xghostty_render_state_row_cells_select(t.cells, int32(x)) != 0 {
				continue
			}
			if word&flagGrapheme != 0 {
				if text := t.cellUTF8(); text != "" {
					c := t.grid.CellAt(x, y)
					if c != nil {
						c.Content = text
					}
				}
			}
			if word&flagStyled != 0 {
				t.applyStyle(x, y)
			}
			if word&flagLink != 0 {
				t.applyLink(x, y)
			}
		}
	}
	t.storeRowWords(y, raw)
	return true
}

func (t *Terminal) invalidateRowWords() {
	for i := range t.rowWordsN {
		t.rowWordsN[i] = -1
	}
}

func (t *Terminal) prepareRowWords() {
	need := t.rows * t.cols * 8
	if len(t.rowWords) == need && len(t.rowWordsN) == t.rows {
		return
	}
	t.rowWords = make([]byte, need)
	t.rowWordsN = make([]int, t.rows)
	for i := range t.rowWordsN {
		t.rowWordsN[i] = -1
	}
}

func (t *Terminal) rowCached(y int, raw []byte) bool {
	if y < 0 || y >= len(t.rowWordsN) || t.rowWordsN[y] != len(raw) {
		return false
	}
	off := y * t.cols * 8
	end := off + len(raw)
	if off < 0 || end > len(t.rowWords) {
		return false
	}
	return bytes.Equal(t.rowWords[off:end], raw)
}

func (t *Terminal) storeRowWords(y int, raw []byte) {
	t.prepareRowWords()
	if y < 0 || y >= len(t.rowWordsN) {
		return
	}
	off := y * t.cols * 8
	end := off + len(raw)
	if off < 0 || end > len(t.rowWords) {
		return
	}
	copy(t.rowWords[off:end], raw)
	t.rowWordsN[y] = len(raw)
}

// Flag bits for one GhosttyCell word. They live in a side buffer so
// decode does not store a Cell twice.
const (
	// flagGrapheme means the codepoint is only the first scalar of the
	// cluster and the row-cells cursor has to load the rest.
	flagGrapheme byte = 1 << iota
	// flagStyled means the style id is non-zero.
	flagStyled
	// flagLink means the word's link bit is set.
	flagLink
)

// rawCell unpacks one GhosttyCell word into dst. The bit layout is the
// packed manifest type: content tag in bits 0-1, 24 bits of content,
// 16-bit style id, 2-bit wide flag, and a link bit. Tag 1 means the
// codepoint is incomplete and cellUTF8 must load the grapheme cluster.
// The returned flags are flagGrapheme, flagStyled, and flagLink.
func rawCell(b []byte, dst *Cell) byte {
	v := uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
	tag := v & 0x3
	content := (v >> 2) & 0xffffff
	styleID := (v >> 26) & 0xffff
	wide := (v >> 42) & 0x3
	cp := rune(content & 0x1fffff)
	var flags byte
	if (v>>45)&1 == 1 {
		flags |= flagLink
	}
	width := 1
	switch wide {
	case 1:
		width = 2
	case 2, 3:
		width = 0
	}
	var text string
	var bg color.Color
	switch tag {
	case 0, 1:
		if cp == 0 {
			text = " "
			if wide == 0 {
				width = 1
			}
		} else if cp < 128 {
			text = asciiRunes[cp]
		} else {
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], cp)
			text = string(buf[:n])
		}
		if tag == 1 {
			flags |= flagGrapheme
		}
	case 2:
		text = " "
		bg = Indexed(content & 0xff)
	case 3:
		text = " "
		bg = color.NRGBA{R: uint8(content), G: uint8(content >> 8), B: uint8(content >> 16), A: 0xff}
	}
	if styleID != 0 {
		flags |= flagStyled
	}
	*dst = Cell{Content: text, Width: width, Style: Style{Bg: bg}}
	return flags
}

// cellUTF8 reads GRAPHEMES_UTF8 for the selected row cell into a
// GhosttyBuffer parked in scratch. OUT_OF_SPACE and any other result
// yield an empty string. The 64-byte cap matches the buffer written
// at scratch+32.
func (t *Terminal) cellUTF8() string {
	// GhosttyBuffer is ptr, cap, len. Use a side buffer inside scratch past 16 bytes.
	const capN = 64
	data := t.scratch + 32
	t.mod.Zero(t.scratch, 16)
	t.mod.SetU32(t.scratch, 0, uint32(data))
	t.mod.SetU32(t.scratch, 4, capN)
	rc := t.mod.Xghostty_render_state_row_cells_get(
		t.cells, wasmvt.RenderStateRowCellsDataGraphemesUtf8, t.scratch,
	)
	if rc == wasmvt.ResultOutOfSpace {
		return ""
	}
	if rc != 0 {
		return ""
	}
	n := int(t.mod.U32(t.scratch, 8))
	if n <= 0 || n > capN {
		return ""
	}
	return string(append([]byte(nil), t.mod.Slice(data, n)...))
}

// applyStyle reads GhosttyStyle for the selected cell. The out
// pointer is sized. Colors are tagged unions (palette or RGB) decoded
// by styleColor. Attribute bytes are the manifest field offsets.
func (t *Terminal) applyStyle(x, y int) {
	n := wasmvt.SizeStyle
	stylePtr := t.scratch + 128
	t.mod.Zero(stylePtr, n)
	t.mod.SetU32(stylePtr, 0, uint32(n))
	if t.mod.Xghostty_render_state_row_cells_get(
		t.cells, wasmvt.RenderStateRowCellsDataStyle, stylePtr,
	) != 0 {
		return
	}
	cell := t.grid.CellAt(x, y)
	if cell == nil {
		return
	}
	cell.Style.Fg = t.styleColor(stylePtr, wasmvt.StyleFgColorOff)
	cell.Style.Bg = t.styleColor(stylePtr, wasmvt.StyleBgColorOff)
	cell.Style.UnderlineColor = t.styleColor(stylePtr, wasmvt.StyleUnderlineColorOff)
	base := stylePtr
	if t.mod.U8(base, wasmvt.StyleBoldOff) != 0 {
		cell.Style.Attrs |= AttrBold
	}
	if t.mod.U8(base, wasmvt.StyleFaintOff) != 0 {
		cell.Style.Attrs |= AttrFaint
	}
	if t.mod.U8(base, wasmvt.StyleItalicOff) != 0 {
		cell.Style.Attrs |= AttrItalic
	}
	if t.mod.U8(base, wasmvt.StyleBlinkOff) != 0 {
		cell.Style.Attrs |= AttrBlink
	}
	if t.mod.U8(base, wasmvt.StyleInverseOff) != 0 {
		cell.Style.Attrs |= AttrReverse
	}
	if t.mod.U8(base, wasmvt.StyleInvisibleOff) != 0 {
		cell.Style.Attrs |= AttrConceal
	}
	if t.mod.U8(base, wasmvt.StyleStrikethroughOff) != 0 {
		cell.Style.Attrs |= AttrStrikethrough
	}
	ul := t.mod.I32(base, wasmvt.StyleUnderlineOff)
	if ul > 0 && ul < 6 {
		cell.Style.Underline = Underline(ul)
	}
}

// styleColor reads one GhosttyStyleColor at style+field. field is a
// generated offset (fg, bg, or underline). NONE and any other tag
// return nil so the cell keeps no explicit color.
func (t *Terminal) styleColor(style int32, field int) color.Color {
	tag := t.mod.I32(style, field+wasmvt.StyleColorTagOff)
	value := field + wasmvt.StyleColorValueOff
	switch tag {
	case wasmvt.StyleColorTagPalette:
		return Indexed(t.mod.U8(style, value))
	case wasmvt.StyleColorTagRgb:
		return color.NRGBA{R: t.mod.U8(style, value), G: t.mod.U8(style, value+1), B: t.mod.U8(style, value+2), A: 0xff}
	default:
		return nil
	}
}

// applyLink resolves the cell's hyperlink. The point is a viewport
// GhosttyPoint. ghostty_terminal_grid_ref writes a sized GhosttyGridRef,
// and ghostty_grid_ref_hyperlink_uri writes the URI into scratch.
// A non-zero result means the cell has no URI.
func (t *Terminal) applyLink(x, y int) {
	point := t.scratch + 256
	ref := point + 32
	t.mod.Zero(point, wasmvt.SizePoint)
	t.mod.SetI32(point, wasmvt.PointTagOff, wasmvt.PointTagViewport)
	coord := wasmvt.PointValueOff
	t.mod.SetU16(point, coord+wasmvt.PointCoordinateXOff, uint16(x))
	t.mod.SetU32(point, coord+wasmvt.PointCoordinateYOff, uint32(y))
	t.mod.Zero(ref, wasmvt.SizeGridRef)
	t.mod.SetU32(ref, 0, uint32(wasmvt.SizeGridRef))
	if t.mod.Xghostty_terminal_grid_ref(t.term, point, ref) != 0 {
		return
	}
	buf := ref + 32
	lenPtr := buf + 128
	t.mod.Zero(lenPtr, 4)
	rc := t.mod.Xghostty_grid_ref_hyperlink_uri(ref, buf, 128, lenPtr)
	if rc != 0 {
		return
	}
	n := int(t.mod.U32(lenPtr, 0))
	if n <= 0 {
		return
	}
	cell := t.grid.CellAt(x, y)
	if cell == nil {
		return
	}
	cell.Link.URL = string(append([]byte(nil), t.mod.Slice(buf, n)...))
}

// readCursorLocked reads GhosttyRenderStateCursor into scratch. The
// out struct is sized. viewport_has_value gates the coordinates.
// visual_style maps to the screen cursor shapes used by drawers.
func (t *Terminal) readCursorLocked() {
	n := wasmvt.SizeRenderStateCursor
	ptr := t.scratch + 512
	t.mod.Zero(ptr, n)
	t.mod.SetU32(ptr, 0, uint32(n))
	cur := Cursor{Shape: CursorBlock}
	if t.mod.Xghostty_render_state_get(t.render, wasmvt.RenderStateDataCursor, ptr) != 0 {
		t.gridCursor = cur
		return
	}
	if t.mod.U8(ptr, wasmvt.RenderStateCursorViewportHasValueOff) != 0 {
		cur.X = int(t.mod.U16(ptr, wasmvt.RenderStateCursorViewportXOff))
		cur.Y = int(t.mod.U16(ptr, wasmvt.RenderStateCursorViewportYOff))
	} else {
		cur.X, cur.Y = -1, -1
	}
	cur.Visible = t.mod.U8(ptr, wasmvt.RenderStateCursorVisibleOff) != 0
	cur.Blink = t.mod.U8(ptr, wasmvt.RenderStateCursorBlinkingOff) != 0
	switch t.mod.I32(ptr, wasmvt.RenderStateCursorVisualStyleOff) {
	case 0:
		cur.Shape = CursorBar
	case 2:
		cur.Shape = CursorUnderline
	case 3:
		cur.Shape = CursorBlockHollow
	default:
		cur.Shape = CursorBlock
	}
	t.gridCursor = cur
}

// renderI32 reads one i32 GhosttyRenderStateData value. A failed get
// returns 0, which is also a valid dirty flag, so callers treat 0 as
// "nothing new" rather than as an error.
func (t *Terminal) renderI32(data int32) int32 {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Xghostty_render_state_get(t.render, data, t.scratch) != 0 {
		return 0
	}
	return t.mod.I32(t.scratch, 0)
}

// rowI32 reads one i32 GhosttyRenderStateRowData value from the
// current iterator row. A failed get returns 0.
func (t *Terminal) rowI32(data int32) int32 {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Xghostty_render_state_row_get(t.rowsIt, data, t.scratch) != 0 {
		return 0
	}
	return t.mod.I32(t.scratch, 0)
}

// rowBool reads a C bool GhosttyRenderStateRowData value. Ghostty
// stores that bool as one byte.
func (t *Terminal) rowBool(data int32) bool {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Xghostty_render_state_row_get(t.rowsIt, data, t.scratch) != 0 {
		return false
	}
	return t.mod.U8(t.scratch, 0) != 0
}

// rowWrapped reports whether the current iterator row is soft-wrapped,
// from the GhosttyRow word RenderStateRowDataRaw holds. A failed get
// reports a row that is not.
func (t *Terminal) rowWrapped() bool {
	t.mod.Zero(t.scratch, 16)
	if t.mod.Xghostty_render_state_row_get(t.rowsIt, wasmvt.RenderStateRowDataRaw, t.scratch) != 0 {
		return false
	}
	row := int64(t.mod.U64(t.scratch, 0)) //nolint:gosec // A bit field, passed back as it is.
	if t.mod.Xghostty_row_get(row, wasmvt.RowDataWrap, t.scratch+8) != 0 {
		return false
	}
	return t.mod.U8(t.scratch, 8) != 0
}

// activeAlt reports whether TerminalDataActiveScreen is the
// alternate screen. A failed get reports the primary screen.
func (t *Terminal) activeAlt() bool {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, wasmvt.TerminalDataActiveScreen, t.scratch) != nil {
		return false
	}
	return t.mod.I32(t.scratch, 0) == wasmvt.TerminalScreenAlternate
}
