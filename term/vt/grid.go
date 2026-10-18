// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"image/color"
	"unicode/utf8"

	"github.com/lrstanley/x/term/vt/internal/wasmvt"
	"github.com/lrstanley/x/term/vt/screen"
)

// The grid is a snapshot of GhosttyRenderState, not a live view of Wasm
// memory. refreshLocked calls ghostty_render_state_update, then walks
// the row iterator. CELLS_RAW is a packed 8-byte word per cell
// (GhosttyCell). Graphemes, styles, and links are a second query
// through GhosttyRenderStateRowCells, and only for cells whose word
// says they need it. Copies leave Wasm before the next allocating call.
// Layout constants are the generated names in wasmvt.

// screenLocked returns the frozen frame during a synchronized-output
// hold. Otherwise it refreshes from the render state.
func (t *Terminal) screenLocked() *screen.Buffer {
	if t.held && t.frozen != nil {
		return t.frozen
	}
	return t.refreshLocked()
}

// captureFrame clones the current grid for the hold callback. The
// clone does not alias Wasm memory.
func (t *Terminal) captureFrame() (*screen.Buffer, Cursor) {
	grid := t.refreshLocked()
	if grid == nil {
		return nil, Cursor{}
	}
	return grid.Clone(), t.gridCursor
}

// refreshLocked pulls GhosttyRenderState into t.grid. The caller holds
// the terminal lock. A full dirty flag of 1 skips clean rows. The row
// iterator handle is passed by pointer in scratch. Failure leaves the
// previous grid in place.
func (t *Terminal) refreshLocked() *screen.Buffer {
	if rc := t.mod.Xghostty_render_state_update(t.render, t.term); rc != 0 && t.grid == nil {
		t.grid = screen.NewBuffer(t.cols, t.rows)
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
		t.grid = screen.NewBuffer(t.cols, t.rows)
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
	for t.mod.Xghostty_render_state_row_iterator_next(t.rowsIt) != 0 {
		y := int(t.rowI32(wasmvt.RenderStateRowDataViewportY))
		if y < 0 || y >= t.rows {
			continue
		}
		rowDirty := t.rowBool(wasmvt.RenderStateRowDataDirty)
		if dirty == 1 && !rowDirty {
			continue
		}
		t.decodeRow(y)
		if y < len(t.grid.Dirty) {
			t.grid.Dirty[y] = true
		}
	}
	t.readCursorLocked()
	t.alt = t.activeAlt()
	t.gridFresh = true
	return t.grid
}

// decodeRow copies CELLS_RAW for viewport row y out of Wasm, then
// fills grapheme, style, and hyperlink data for cells that need it.
// The raw pointer is only valid until the next render-state call, so
// the words are copied into t.rawRow first.
func (t *Terminal) decodeRow(y int) {
	view := t.scratch
	t.mod.Zero(view, 8)
	if err := wasmvt.Result("cells", t.mod.Xghostty_render_state_row_get(
		t.rowsIt, wasmvt.RenderStateRowDataCellsRaw, view,
	)); err != nil {
		return
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
	needsExtra := false
	if cap(t.decodedRow) < n {
		t.decodedRow = make([]decodedCell, n)
	}
	t.decodedRow = t.decodedRow[:n]
	decoded := t.decodedRow
	for i := range n {
		decoded[i] = rawCell(raw[i*8 : i*8+8])
		if decoded[i].extra {
			needsExtra = true
		}
		t.grid.SetRaw(i, y, decoded[i].cell)
	}
	if !needsExtra {
		return
	}
	t.mod.SetU32(t.scratch, 0, uint32(t.cells))
	if err := wasmvt.Result("row cells", t.mod.Xghostty_render_state_row_get(
		t.rowsIt, wasmvt.RenderStateRowDataCells, t.scratch,
	)); err != nil {
		return
	}
	for x := range n {
		word := decoded[x]
		if !word.extra {
			continue
		}
		if t.mod.Xghostty_render_state_row_cells_select(t.cells, int32(x)) != 0 {
			continue
		}
		if word.grapheme {
			if text := t.cellUTF8(); text != "" {
				c := t.grid.CellAt(x, y)
				if c != nil {
					c.Content = text
				}
			}
		}
		if word.styled {
			t.applyStyle(x, y)
		}
		if word.link {
			t.applyLink(x, y)
		}
	}
}

// decodedCell is one unpacked GhosttyCell word, before the slow path
// fills graphemes, styles, and links.
type decodedCell struct {
	// cell is what the packed word can express on its own.
	cell screen.Cell
	// extra is true when grapheme, styled, or link is set and the row
	// cells cursor has to be queried.
	extra bool
	// grapheme is true when the codepoint is only the first scalar of
	// the cluster.
	grapheme bool
	// styled is true when the style id is non-zero.
	styled bool
	// link is true when the word's link bit is set.
	link bool
}

// rawCell unpacks one GhosttyCell word. The bit layout is the packed
// manifest type: content tag in bits 0-1, 24 bits of content, 16-bit
// style id, 2-bit wide flag, and a link bit. Tag 1 means the codepoint
// is incomplete and cellUTF8 must load the grapheme cluster.
func rawCell(b []byte) decodedCell {
	v := uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
	tag := v & 0x3
	content := (v >> 2) & 0xffffff
	styleID := (v >> 26) & 0xffff
	wide := (v >> 42) & 0x3
	link := (v>>45)&1 == 1
	cp := rune(content & 0x1fffff)
	out := decodedCell{}
	switch wide {
	case 1:
		out.cell.Width = 2
	case 2, 3:
		out.cell.Width = 0
	default:
		out.cell.Width = 1
	}
	switch tag {
	case 0, 1:
		if cp == 0 {
			out.cell.Content = " "
			if wide == 0 {
				out.cell.Width = 1
			}
		} else {
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], cp)
			out.cell.Content = string(buf[:n])
		}
		out.grapheme = tag == 1
	case 2:
		out.cell.Content = " "
		out.cell.Style.Bg = screen.Indexed(content & 0xff)
	case 3:
		out.cell.Content = " "
		out.cell.Style.Bg = color.NRGBA{R: uint8(content), G: uint8(content >> 8), B: uint8(content >> 16), A: 0xff}
	}
	out.styled = styleID != 0
	out.link = link
	out.extra = out.grapheme || out.styled || out.link
	return out
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
		cell.Style.Attrs |= screen.AttrBold
	}
	if t.mod.U8(base, wasmvt.StyleFaintOff) != 0 {
		cell.Style.Attrs |= screen.AttrFaint
	}
	if t.mod.U8(base, wasmvt.StyleItalicOff) != 0 {
		cell.Style.Attrs |= screen.AttrItalic
	}
	if t.mod.U8(base, wasmvt.StyleBlinkOff) != 0 {
		cell.Style.Attrs |= screen.AttrBlink
	}
	if t.mod.U8(base, wasmvt.StyleInverseOff) != 0 {
		cell.Style.Attrs |= screen.AttrReverse
	}
	if t.mod.U8(base, wasmvt.StyleInvisibleOff) != 0 {
		cell.Style.Attrs |= screen.AttrConceal
	}
	if t.mod.U8(base, wasmvt.StyleStrikethroughOff) != 0 {
		cell.Style.Attrs |= screen.AttrStrikethrough
	}
	ul := t.mod.I32(base, wasmvt.StyleUnderlineOff)
	if ul > 0 && ul < 6 {
		cell.Style.Underline = screen.Underline(ul)
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
		return screen.Indexed(t.mod.U8(style, value))
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
	cur := Cursor{Shape: screen.CursorBlock}
	if t.mod.Xghostty_render_state_get(t.render, wasmvt.RenderStateDataCursor, ptr) != 0 {
		t.gridCursor = cur
		return
	}
	if t.mod.U8(ptr, wasmvt.RenderStateCursorViewportHasValueOff) != 0 {
		cur.X = int(t.mod.U16(ptr, wasmvt.RenderStateCursorViewportXOff))
		cur.Y = int(t.mod.U16(ptr, wasmvt.RenderStateCursorViewportYOff))
	}
	cur.Visible = t.mod.U8(ptr, wasmvt.RenderStateCursorVisibleOff) != 0
	cur.Blink = t.mod.U8(ptr, wasmvt.RenderStateCursorBlinkingOff) != 0
	switch t.mod.I32(ptr, wasmvt.RenderStateCursorVisualStyleOff) {
	case 0:
		cur.Shape = screen.CursorBar
	case 2:
		cur.Shape = screen.CursorUnderline
	case 3:
		cur.Shape = screen.CursorBlockHollow
	default:
		cur.Shape = screen.CursorBlock
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

// activeAlt reports whether TerminalDataActiveScreen is the
// alternate screen. A failed get reports the primary screen.
func (t *Terminal) activeAlt() bool {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, wasmvt.TerminalDataActiveScreen, t.scratch) != nil {
		return false
	}
	return t.mod.I32(t.scratch, 0) == wasmvt.TerminalScreenAlternate
}
