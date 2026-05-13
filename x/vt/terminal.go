// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"errors"
	"image/color"
	"sync"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Options configures a new [Terminal].
type Options struct {
	// Width and Height are the terminal size in cells. Both must be positive.
	Width, Height int

	// MaxScrollback is the maximum number of scrollback lines to retain.
	MaxScrollback int

	// CellWidthPx and CellHeightPx are the cell geometry used for size reports
	// and resize side effects inside libghostty-vt. When zero, 10×20 is used.
	CellWidthPx, CellHeightPx uint32

	// WidthMethod selects how string widths are derived when building cells.
	// The zero value selects [ansi.WcWidth].
	WidthMethod ansi.Method
}

// Terminal is a libghostty-vt-backed virtual terminal with an API shaped
// similarly to [github.com/charmbracelet/x/vt].
type Terminal struct {
	mu    sync.Mutex
	g     *ghostLib
	h     uintptr
	wm    ansi.Method
	cellW uint32
	cellH uint32
}

// Open loads libghostty-vt (if needed) and allocates a terminal instance.
func Open(o Options) (*Terminal, error) {
	if o.Width <= 0 || o.Height <= 0 {
		return nil, &GhosttyError{Code: ghosttyInvalidValue}
	}
	if o.MaxScrollback < 0 {
		return nil, &GhosttyError{Code: ghosttyInvalidValue}
	}
	g, err := ghostLibSingleton()
	if err != nil {
		return nil, err
	}
	cw := o.CellWidthPx
	ch := o.CellHeightPx
	if cw == 0 {
		cw = 10
	}
	if ch == 0 {
		ch = 20
	}
	wm := o.WidthMethod
	opts := ghosttyTerminalOptions{
		Cols:          uint16(o.Width),  //nolint:gosec // validated above
		Rows:          uint16(o.Height), //nolint:gosec
		MaxScrollback: uintptr(o.MaxScrollback),
	}
	h, err := g.terminalNew(opts)
	if err != nil {
		return nil, err
	}
	t := &Terminal{g: g, h: h, wm: wm, cellW: cw, cellH: ch}
	if err := g.terminalResize(h, uint16(o.Width), uint16(o.Height), cw, ch); err != nil { //nolint:gosec
		g.terminalFree(h)
		return nil, err
	}
	return t, nil
}

// Close releases the native terminal handle.
func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return ErrClosed
	}
	t.g.terminalFree(t.h)
	t.h = 0
	return nil
}

// Write processes raw VT stream bytes (similar to [github.com/charmbracelet/x/vt.Terminal.Write]).
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return 0, ErrClosed
	}
	if err := t.g.terminalVtWrite(t.h, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// WriteString is a convenience wrapper around [Terminal.Write].
func (t *Terminal) WriteString(s string) (int, error) {
	return t.Write([]byte(s))
}

// Resize resizes the terminal grid to width×height cells.
func (t *Terminal) Resize(width, height int) error {
	if width <= 0 || height <= 0 {
		return &GhosttyError{Code: ghosttyInvalidValue}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return ErrClosed
	}
	return t.g.terminalResize(t.h, uint16(width), uint16(height), t.cellW, t.cellH) //nolint:gosec
}

// Width returns the current width in cells.
func (t *Terminal) Width() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return 0
	}
	var v uint16
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataCols, unsafe.Pointer(&v)); err != nil {
		return 0
	}
	return int(v)
}

// Height returns the current height in cells.
func (t *Terminal) Height() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return 0
	}
	var v uint16
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataRows, unsafe.Pointer(&v)); err != nil {
		return 0
	}
	return int(v)
}

// Bounds returns the rectangle covering the active grid, aligned with
// [github.com/charmbracelet/ultraviolet.Screen.Bounds].
func (t *Terminal) Bounds() uv.Rectangle {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return uv.Rectangle{}
	}
	var cols, rows uint16
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataCols, unsafe.Pointer(&cols)); err != nil {
		return uv.Rectangle{}
	}
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataRows, unsafe.Pointer(&rows)); err != nil {
		return uv.Rectangle{}
	}
	return uv.Rectangle{Max: uv.Position{X: int(cols), Y: int(rows)}}
}

// WidthMethod reports the width strategy used when constructing cell contents.
func (t *Terminal) WidthMethod() uv.WidthMethod {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wm
}

// CursorPosition returns the cursor location in the active area (zero-based).
func (t *Terminal) CursorPosition() uv.Position {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return uv.Position{}
	}
	var x, y uint16
	_ = t.g.terminalGet(t.h, ghosttyTerminalDataCursorX, unsafe.Pointer(&x))
	_ = t.g.terminalGet(t.h, ghosttyTerminalDataCursorY, unsafe.Pointer(&y))
	return uv.Position{X: int(x), Y: int(y)}
}

// IsAltScreen reports whether the alternate screen buffer is active.
func (t *Terminal) IsAltScreen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return false
	}
	var screen int32
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataActiveScreen, unsafe.Pointer(&screen)); err != nil {
		return false
	}
	return screen == ghosttyTerminalScreenAlternate
}

// ScrollbackLen reports how many scrollback rows exist beyond the viewport.
func (t *Terminal) ScrollbackLen() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return 0
	}
	var v uint64
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataScrollbackRows, unsafe.Pointer(&v)); err != nil {
		return 0
	}
	return int(v)
}

// ForegroundColor returns the effective foreground RGBA, if configured.
func (t *Terminal) ForegroundColor() color.Color {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return nil
	}
	var rgb ghosttyColorRGB
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataColorForeground, unsafe.Pointer(&rgb)); err != nil {
		return nil
	}
	return color.NRGBA{R: rgb.R, G: rgb.G, B: rgb.B, A: 0xff}
}

// BackgroundColor returns the effective background RGBA, if configured.
func (t *Terminal) BackgroundColor() color.Color {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return nil
	}
	var rgb ghosttyColorRGB
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataColorBackground, unsafe.Pointer(&rgb)); err != nil {
		return nil
	}
	return color.NRGBA{R: rgb.R, G: rgb.G, B: rgb.B, A: 0xff}
}

// CursorColor returns the effective cursor RGBA, if configured.
func (t *Terminal) CursorColor() color.Color {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return nil
	}
	var rgb ghosttyColorRGB
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataColorCursor, unsafe.Pointer(&rgb)); err != nil {
		return nil
	}
	return color.NRGBA{R: rgb.R, G: rgb.G, B: rgb.B, A: 0xff}
}

// CellAt returns the resolved cell at (x, y) in active coordinates, or nil if
// out of bounds.
func (t *Terminal) CellAt(x, y int) *uv.Cell {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h == 0 {
		return nil
	}
	var cols, rows uint16
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataCols, unsafe.Pointer(&cols)); err != nil {
		return nil
	}
	if err := t.g.terminalGet(t.h, ghosttyTerminalDataRows, unsafe.Pointer(&rows)); err != nil {
		return nil
	}
	if x < 0 || y < 0 || x >= int(cols) || y >= int(rows) {
		return nil
	}
	pt := ghosttyPointActive(uint16(x), uint32(y)) //nolint:gosec
	var ref ghosttyGridRef
	ref.Size = uintptr(sizeofGhosttyGridRef)
	if err := t.g.terminalGridRef(t.h, pt, &ref); err != nil {
		return nil
	}
	var cellID uint64
	if err := t.g.gridRefCell(&ref, &cellID); err != nil {
		return nil
	}
	var wide int32
	_ = t.g.cellGet(cellID, ghosttyCellDataWide, unsafe.Pointer(&wide))

	buf := make([]uint32, 128)
	nCP, err := t.g.gridRefGraphemes(&ref, buf)
	for {
		var gerr *GhosttyError
		if err == nil {
			break
		}
		if errors.As(err, &gerr) && gerr.Code == ghosttyOutOfSpace && nCP > len(buf) {
			buf = make([]uint32, nCP)
			nCP, err = t.g.gridRefGraphemes(&ref, buf)
			continue
		}
		return &uv.Cell{Content: " ", Width: 1}
	}
	text := graphemesToString(buf[:nCP])
	gw := t.wm.StringWidth(text)
	w := cellWidthFromGhostty(wide, gw)
	if text == "" && w == 0 {
		return &uv.Cell{}
	}
	if text == "" && w > 0 {
		text = " "
	}

	var st ghosttyStyle
	st.Size = uintptr(sizeofGhosttyStyle)
	if err := t.g.gridRefStyle(&ref, &st); err != nil {
		st = ghosttyStyle{}
	}
	style := styleFromGhostty(&st)

	linkBuf := make([]byte, 256)
	nURI, err := t.g.gridRefHyperlinkURI(&ref, linkBuf)
	for {
		var gerr *GhosttyError
		if err == nil {
			break
		}
		if errors.As(err, &gerr) && gerr.Code == ghosttyOutOfSpace && nURI > len(linkBuf) {
			linkBuf = make([]byte, nURI)
			nURI, err = t.g.gridRefHyperlinkURI(&ref, linkBuf)
			continue
		}
		break
	}
	var link uv.Link
	if err == nil && nURI > 0 {
		link.URL = string(linkBuf[:nURI])
	}

	return &uv.Cell{
		Content: text,
		Width:   w,
		Style:   style,
		Link:    link,
	}
}
