// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"fmt"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// PointTag says which coordinate space a [Point] uses.
// Values match GhosttyPointTag.
type PointTag int32

// Point coordinate spaces.
const (
	PointActive PointTag = iota
	PointViewport
	PointScreen
	PointHistory
)

// Point is one cell. Y is a row in Tag's coordinate space.
type Point struct {
	// Tag selects the coordinate space Y is measured in.
	Tag PointTag
	// X is the column, starting at 0.
	X int
	// Y is the row in Tag's space. For history it grows backward from
	// the top of the screen.
	Y int
}

// Selection is a snapshot of a grid range. Start and End are inclusive.
// Rectangle is true for a block selection. The snapshot is valid only
// until the next call that mutates the terminal, matching GhosttySelection.
type Selection struct {
	// Start is the inclusive anchor.
	Start Point
	// End is the inclusive end. It may sit in a different coordinate space
	// from Start.
	End Point
	// Rectangle is true for a block selection. False is a stream selection.
	Rectangle bool
	// raw is a copy of the GhosttySelection bytes. Later adjust and
	// format calls copy it back into Wasm. A zero-length raw means the
	// selection was built by the caller and cannot be sent back.
	raw []byte
}

// SelectionAdjust moves the logical end of a selection.
// Values match GhosttySelectionAdjust.
type SelectionAdjust int32

// Selection adjustments.
const (
	SelectionAdjustLeft SelectionAdjust = iota
	SelectionAdjustRight
	SelectionAdjustUp
	SelectionAdjustDown
	SelectionAdjustHome
	SelectionAdjustEnd
	SelectionAdjustPageUp
	SelectionAdjustPageDown
	SelectionAdjustBeginningOfLine
	SelectionAdjustEndOfLine
)

// SelectionOrder is the direction from Start to End.
// Values match GhosttySelectionOrder.
type SelectionOrder int32

// Selection orders.
const (
	SelectionOrderForward SelectionOrder = iota
	SelectionOrderReverse
	SelectionOrderMirroredForward
	SelectionOrderMirroredReverse
)

// SelectWord returns the word containing at. An empty cell returns [ErrNoValue].
func (t *Terminal) SelectWord(at Point) (Selection, error) {
	return t.selectAt("select word", wasmvt.SizeTerminalSelectWordOptions, wasmvt.TerminalSelectWordOptionsRefOff, at, t.mod.Xghostty_terminal_select_word)
}

// SelectLine returns the line containing at. An empty line returns [ErrNoValue].
func (t *Terminal) SelectLine(at Point) (Selection, error) {
	return t.selectAt("select line", wasmvt.SizeTerminalSelectLineOptions, wasmvt.TerminalSelectLineOptionsRefOff, at, t.mod.Xghostty_terminal_select_line)
}

// SelectOutput returns the semantic command output containing at.
// The shell marks that region with OSC 133. A cell outside command
// output returns [ErrNoValue].
func (t *Terminal) SelectOutput(at Point) (Selection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return Selection{}, errClosed
	}
	ref, refN, err := t.gridRefLocked(at)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(ref, refN)
	return t.selectionCall("select output", func(out int32) int32 {
		return t.mod.Xghostty_terminal_select_output(t.term, ref, out)
	})
}

// SelectWordBetween returns the nearest word between start and end, inclusive.
func (t *Terminal) SelectWordBetween(start, end Point) (Selection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return Selection{}, errClosed
	}
	a, aN, err := t.gridRefLocked(start)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(a, aN)
	b, bN, err := t.gridRefLocked(end)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(b, bN)
	optN := wasmvt.SizeTerminalSelectWordBetweenOptions
	opt, err := t.mod.Alloc(optN)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(opt, optN)
	t.mod.Zero(opt, optN)
	t.mod.SetU32(opt, 0, uint32(optN))
	copy(t.mod.Slice(opt+int32(wasmvt.TerminalSelectWordBetweenOptionsStartOff), aN), t.mod.Slice(a, aN))
	copy(t.mod.Slice(opt+int32(wasmvt.TerminalSelectWordBetweenOptionsEndOff), bN), t.mod.Slice(b, bN))
	return t.selectionCall("select word between", func(out int32) int32 {
		return t.mod.Xghostty_terminal_select_word_between(t.term, opt, out)
	})
}

// SelectAll returns every selectable cell. An empty screen returns [ErrNoValue].
func (t *Terminal) SelectAll() (Selection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return Selection{}, errClosed
	}
	return t.selectionCall("select all", func(out int32) int32 {
		return t.mod.Xghostty_terminal_select_all(t.term, out)
	})
}

// SelectionText formats sel as plain text. sel must still be valid.
func (t *Terminal) SelectionText(sel Selection) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return "", errClosed
	}
	return t.selectionTextLocked(sel)
}

// SelectionOrder reports the direction of sel.
func (t *Terminal) SelectionOrder(sel Selection) (SelectionOrder, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return 0, errClosed
	}
	raw, n, err := t.stageSelection(sel)
	if err != nil {
		return 0, err
	}
	defer t.mod.Free(raw, n)
	out, err := t.mod.Alloc(4)
	if err != nil {
		return 0, err
	}
	defer t.mod.Free(out, 4)
	if err = wasmvt.Result("selection order", t.mod.Xghostty_terminal_selection_order(t.term, raw, out)); err != nil {
		return 0, err
	}
	return SelectionOrder(t.mod.I32(out, 0)), nil
}

// SelectionContains reports whether at lies inside sel.
func (t *Terminal) SelectionContains(sel Selection, at Point) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return false, errClosed
	}
	raw, n, err := t.stageSelection(sel)
	if err != nil {
		return false, err
	}
	defer t.mod.Free(raw, n)
	pn := wasmvt.SizePoint
	pt, err := t.mod.Alloc(pn)
	if err != nil {
		return false, err
	}
	defer t.mod.Free(pt, pn)
	t.writePoint(pt, at)
	out, err := t.mod.Alloc(1)
	if err != nil {
		return false, err
	}
	defer t.mod.Free(out, 1)
	if err = wasmvt.Result("selection contains", t.mod.Xghostty_terminal_selection_contains(t.term, raw, pt, out)); err != nil {
		return false, err
	}
	return t.mod.U8(out, 0) != 0, nil
}

// SelectionAdjust moves the logical end of sel and returns the updated snapshot.
func (t *Terminal) SelectionAdjust(sel Selection, adj SelectionAdjust) (Selection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return Selection{}, errClosed
	}
	raw, n, err := t.stageSelection(sel)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(raw, n)
	if err = wasmvt.Result("selection adjust", t.mod.Xghostty_terminal_selection_adjust(t.term, raw, int32(adj))); err != nil {
		return Selection{}, err
	}
	return t.selectionFrom(raw)
}

// selectAt implements word and line selection. optN and refOff are
// the generated size and "ref" offset of the options struct
// (GhosttyTerminalSelectWordOptions or SelectLineOptions). The struct
// is sized, then the grid ref is copied onto that field. call is the
// matching ghostty_terminal_select_* export.
func (t *Terminal) selectAt(op string, optN, refOff int, at Point, call func(term, opt, out int32) int32) (Selection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return Selection{}, errClosed
	}
	ref, refN, err := t.gridRefLocked(at)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(ref, refN)
	opt, err := t.mod.Alloc(optN)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(opt, optN)
	t.mod.Zero(opt, optN)
	t.mod.SetU32(opt, 0, uint32(optN))
	copy(t.mod.Slice(opt+int32(refOff), refN), t.mod.Slice(ref, refN))
	return t.selectionCall(op, func(out int32) int32 {
		return call(t.term, opt, out)
	})
}

// selectionCall allocates a sized GhosttySelection out-parameter and
// decodes it. call writes the selection and returns a GhosttyResult.
func (t *Terminal) selectionCall(op string, call func(out int32) int32) (Selection, error) {
	n := wasmvt.SizeSelection
	raw, err := t.mod.Alloc(n)
	if err != nil {
		return Selection{}, err
	}
	defer t.mod.Free(raw, n)
	t.mod.Zero(raw, n)
	t.mod.SetU32(raw, 0, uint32(n))
	if err = wasmvt.Result(op, call(raw)); err != nil {
		return Selection{}, err
	}
	return t.selectionFrom(raw)
}

// selectionFrom copies a GhosttySelection out of Wasm. Start and end
// are grid refs. pointFromRef turns each into an active-screen point.
// The raw bytes are kept so a later adjust or format call can hand
// the same selection back without rebuilding it.
func (t *Terminal) selectionFrom(raw int32) (Selection, error) {
	n := wasmvt.SizeSelection
	sel := Selection{
		Rectangle: t.mod.U8(raw, wasmvt.SelectionRectangleOff) != 0,
		raw:       append([]byte(nil), t.mod.Slice(raw, n)...),
	}
	start, err := t.pointFromRef(raw+int32(wasmvt.SelectionStartOff), PointActive)
	if err != nil {
		return Selection{}, err
	}
	end, err := t.pointFromRef(raw+int32(wasmvt.SelectionEndOff), PointActive)
	if err != nil {
		return Selection{}, err
	}
	sel.Start = start
	sel.End = end
	return sel, nil
}

// stageSelection copies a previously captured GhosttySelection back
// into Wasm. The caller frees the pointer. A selection that was not
// produced by this terminal has no raw bytes and returns [ErrNoValue].
func (t *Terminal) stageSelection(sel Selection) (int32, int, error) {
	n := wasmvt.SizeSelection
	if len(sel.raw) != n {
		return 0, 0, fmt.Errorf("vt: selection: %w", ErrNoValue)
	}
	raw, err := t.mod.Alloc(n)
	if err != nil {
		return 0, 0, err
	}
	copy(t.mod.Slice(raw, n), sel.raw)
	return raw, n, nil
}

// selectionTextLocked formats a selection as plain text.
// ghostty_terminal_selection_format_buf is called once with a null
// buffer to read the required size (GHOSTTY_OUT_OF_SPACE) and again
// to fill it. The options struct points at the staged selection.
func (t *Terminal) selectionTextLocked(sel Selection) (string, error) {
	raw, n, err := t.stageSelection(sel)
	if err != nil {
		return "", err
	}
	defer t.mod.Free(raw, n)
	optN := wasmvt.SizeTerminalSelectionFormatOptions
	opt, err := t.mod.Alloc(optN)
	if err != nil {
		return "", err
	}
	defer t.mod.Free(opt, optN)
	t.mod.Zero(opt, optN)
	t.mod.SetU32(opt, 0, uint32(optN))
	t.mod.SetI32(opt, wasmvt.TerminalSelectionFormatOptionsEmitOff, wasmvt.FormatterFormatPlain)
	t.mod.SetU32(opt, wasmvt.TerminalSelectionFormatOptionsSelectionOff, uint32(raw))
	lenPtr, err := t.mod.Alloc(4)
	if err != nil {
		return "", err
	}
	defer t.mod.Free(lenPtr, 4)
	rc := t.mod.Xghostty_terminal_selection_format_buf(t.term, opt, 0, 0, lenPtr)
	outOfSpace := rc == wasmvt.ResultOutOfSpace
	if rc != 0 && !outOfSpace {
		return "", wasmvt.Result("selection text", rc)
	}
	need := int(t.mod.U32(lenPtr, 0))
	if need == 0 {
		return "", nil
	}
	buf, err := t.mod.Alloc(need)
	if err != nil {
		return "", err
	}
	defer t.mod.Free(buf, need)
	if err = wasmvt.Result("selection text", t.mod.Xghostty_terminal_selection_format_buf(t.term, opt, buf, int32(need), lenPtr)); err != nil {
		return "", err
	}
	wrote := int(t.mod.U32(lenPtr, 0))
	if wrote > need {
		wrote = need
	}
	return string(append([]byte(nil), t.mod.Slice(buf, wrote)...)), nil
}

// gridRefLocked converts a Go point into a sized GhosttyGridRef via
// ghostty_terminal_grid_ref. The caller frees the ref. The temporary
// point is freed here.
func (t *Terminal) gridRefLocked(at Point) (int32, int, error) {
	refN := wasmvt.SizeGridRef
	ref, err := t.mod.Alloc(refN)
	if err != nil {
		return 0, 0, err
	}
	pn := wasmvt.SizePoint
	pt, err := t.mod.Alloc(pn)
	if err != nil {
		t.mod.Free(ref, refN)
		return 0, 0, err
	}
	defer t.mod.Free(pt, pn)
	t.writePoint(pt, at)
	t.mod.Zero(ref, refN)
	t.mod.SetU32(ref, 0, uint32(refN))
	if err = wasmvt.Result("grid ref", t.mod.Xghostty_terminal_grid_ref(t.term, pt, ref)); err != nil {
		t.mod.Free(ref, refN)
		return 0, 0, err
	}
	return ref, refN, nil
}

// pointFromRef calls ghostty_terminal_point_from_grid_ref. ref points
// at a GhosttyGridRef. The output is a GhosttyPointCoordinate, not a
// full tagged point. tag is recorded on the Go value.
func (t *Terminal) pointFromRef(ref int32, tag PointTag) (Point, error) {
	n := wasmvt.SizePointCoordinate
	pt, err := t.mod.Alloc(n)
	if err != nil {
		return Point{}, err
	}
	defer t.mod.Free(pt, n)
	t.mod.Zero(pt, n)
	if err = wasmvt.Result("point", t.mod.Xghostty_terminal_point_from_grid_ref(t.term, ref, int32(tag), pt)); err != nil {
		return Point{}, err
	}
	return Point{
		Tag: tag,
		X:   int(t.mod.U16(pt, wasmvt.PointCoordinateXOff)),
		Y:   int(t.mod.U32(pt, wasmvt.PointCoordinateYOff)),
	}, nil
}

// writePoint stores p as a GhosttyPoint at ptr. The coordinate lives
// in the tagged-union value arm. Every arm this host writes uses
// GhosttyPointCoordinate, so x and y share those offsets.
func (t *Terminal) writePoint(ptr int32, p Point) {
	n := wasmvt.SizePoint
	t.mod.Zero(ptr, n)
	t.mod.SetI32(ptr, wasmvt.PointTagOff, int32(p.Tag))
	coord := wasmvt.PointValueOff
	t.mod.SetU16(ptr, coord+wasmvt.PointCoordinateXOff, uint16(p.X))
	t.mod.SetU32(ptr, coord+wasmvt.PointCoordinateYOff, uint32(p.Y))
}
