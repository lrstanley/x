// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"sync"
	"time"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// DefaultScrollback is how many lines of history a terminal keeps unless
// [WithScrollbackLines] says otherwise.
const DefaultScrollback = 1000

const (
	defaultCellWidth  = 10
	defaultCellHeight = 20
	scratchSize       = 8192
	feedSize          = 64 * 1024
)

// xtversion is the name and version the terminal answers XTVERSION
// (CSI > q) with.
const xtversion = "reel"

// Terminal is one emulator instance. Methods are safe for concurrent use.
// [Terminal.Close] resets the instance and returns it to its pool.
//
// [Terminal.Read] blocks until the emulator writes a pty reply or Close
// returns EOF. Query answers are written there so a program does not stall.
type Terminal struct {
	// rt is the pool this instance returns to on Close. The Wasm module
	// stays allocated across checkouts.
	rt *Runtime

	// mu serializes every call into mod. One module is one linear memory.
	mu sync.Mutex
	// mod is the Wasm instance. Handles below are indexes into it, not
	// Go pointers. They stay valid when memory grows. Slices from mod do not.
	mod *wasmvt.Module
	// term is the GhosttyTerminal handle from ghostty_terminal_new.
	term int32
	// render is the GhosttyRenderState used to snapshot the grid.
	render int32
	// keys is the GhosttyKeyEncoder. keyEv is the event it encodes.
	keys  int32
	keyEv int32
	// mice is the GhosttyMouseEncoder. mouseEv is the event it encodes.
	mice    int32
	mouseEv int32
	// rowsIt is the reusable GhosttyRenderStateRowIterator.
	rowsIt int32
	// cells is the GhosttyRenderStateRowCells cursor for graphemes,
	// styles, and links on the current row.
	cells int32

	// feed is the reusable VT input buffer, feedSize bytes, passed to
	// ghostty_terminal_vt_write.
	feed int32
	// scratch is the reusable ABI scratch, scratchSize bytes. Grid and
	// link readers reserve fixed offsets inside it.
	scratch int32
	// answer is the single ACK byte returned for ENQ. It outlives the callback.
	answer int32
	// version is the [xtversion] bytes returned for XTVERSION.
	version int32

	// cols and rows are the grid size in cells.
	cols, rows int
	// cellW and cellH are the pixel size of one cell. Mouse encoding and
	// in-band size reports use them. They are not changed by Resize.
	cellW, cellH uint32
	// scrollbackLines is the history cap applied with
	// TerminalOptionScrollbackMaxLines. Zero disables history.
	scrollbackLines int
	// scrollbackOff is true after a zero cap sets the byte limit to
	// zero. libghostty keeps that screen in no-scrollback mode until
	// the byte limit is cleared, even if a line cap is set later.
	scrollbackOff bool
	// checksumReport is the host DECRQCRA reply switch. A new checkout
	// starts off. Reset keeps the current value.
	checksumReport bool
	// checksumFlags is the XTCHECKSUM variant restored by a full reset.
	checksumFlags ChecksumFlags

	// replies is the pty response stream drained by Read. The VT-write
	// callback appends to it and must not block.
	replies *replyBuf
	// captured is set while [Terminal.WriteReplies] runs, to what it
	// returns, which the VT-write callback then also appends each reply to.
	captured *[]byte

	// title is the last OSC 0 or OSC 2 window title.
	title string
	// pwd is the last OSC 7 working directory.
	pwd string
	// clock stamps program events. See [WithClock].
	clock func() time.Duration
	// bell is the latest BEL.
	bell Bell
	// alt is true when the alternate screen is active.
	alt bool
	// held is true during synchronized output (mode 2026).
	held bool
	// frozen is the grid captured when the current hold started. Reads
	// return it instead of a partial update. frozenCur is its caret.
	frozen    *Buffer
	frozenCur Cursor

	// progress is the latest OSC 9;4 report.
	progress Progress
	// notifications are OSC 9 and OSC 777 messages, oldest first. Only
	// appended to, and replaced on reset, so a [State] can share it.
	notifications []Notification
	// clipRead is the host clipboard returned for an OSC 52 read.
	clipRead string
	// clipWrote is the last plain text the program wrote to the clipboard.
	clipWrote ClipboardWrite
	// prompt is the latest OSC 133 semantic-prompt step.
	prompt Prompt
	// statuses are OSC 7501 program-status records in update order, least
	// recently updated first, which is the eviction order.
	statuses []ProgramStatus
	// statusView is statuses as [Terminal.ProgramStatuses] returns them:
	// sorted by id, with App inherited. It is replaced, never changed in
	// place, so a [State] can share it. statusGen counts changes to it,
	// and is never reset, so equal generations mean the same records.
	statusView []ProgramStatus
	statusGen  uint64
	// statusReported is true once any OSC 7501 report has arrived since the
	// last full reset.
	statusReported bool
	// resizeCols and resizeRows are the last CSI 8 t window-resize
	// request. resizeOK is false until the program asks. The terminal
	// does not apply the request.
	resizeCols int
	resizeRows int
	resizeOK   bool

	// fg, bg, and cursor are the colors last shown, after overrides.
	fg, bg, cursor color.NRGBA
	// fgOverride, bgOverride, and cursorOverride replace the emulator
	// colors when non-nil. Nil means use the default.
	fgOverride     color.Color
	bgOverride     color.Color
	cursorOverride color.Color
	// fgDefault, bgDefault, and cursorDefault are restored when the
	// matching override is cleared.
	fgDefault     color.Color
	bgDefault     color.Color
	cursorDefault color.Color
	// hasCursor is true when the host or the program set a cursor color.
	hasCursor bool

	// grid is the last decoded screen. It does not alias Wasm memory.
	grid *Buffer
	// gridCursor is the caret from the same refresh as grid.
	gridCursor Cursor
	// gridFresh is true when grid matches the emulator since the last write.
	gridFresh bool
	// outputSeq counts non-empty Write calls.
	outputSeq uint64
	// rawRow is scratch for one viewport row of GhosttyCell words.
	// rowFlags is one flag byte per cell in that row (grapheme, style, link).
	// rowWords is the last words copied for every viewport row of the
	// current grid, cols*8 bytes each. rowWordsN is the cached byte length
	// of each row, or -1 when that row has not been decoded into this grid.
	// Resize and any replacement of grid storage clear rowWordsN.
	rawRow    []byte
	rowFlags  []byte
	rowWords  []byte
	rowWordsN []int
	// callbacksOn is true after installCallbacks. The table entries stay
	// for the life of the module, including while the instance is pooled.
	callbacksOn bool
	// idle is true while the instance sits in the pool. Methods fail with
	// errClosed until the next checkout.
	idle bool

	// searches are live GhosttySearch handles. Close frees any that remain.
	searches []*Search

	// kitty is the Kitty graphics state.
	kitty kittyState
}

// Write feeds VT bytes to the emulator. It never returns a short write.
// A non-empty write advances [Terminal.OutputSeq] once the bytes are parsed.
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeOutputLocked(p)
}

// WriteReplies is [Terminal.Write], which also returns the replies the
// emulator wrote while it parsed p, such as its answers to queries in p, in
// the order it wrote them. They are queued for [Terminal.Read] all the same.
// A query split across two writes is answered by the second.
func (t *Terminal) WriteReplies(p []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var replies []byte
	t.captured = &replies
	_, err := t.writeOutputLocked(p)
	t.captured = nil
	return replies, err
}

// writeOutputLocked is [Terminal.Write] with t.mu held.
func (t *Terminal) writeOutputLocked(p []byte) (int, error) {
	if t.idle {
		return 0, errors.New("vt: terminal is closed")
	}
	if err := t.writeLocked(p); err != nil {
		return 0, err
	}
	t.gridFresh = false
	if len(p) > 0 {
		t.outputSeq++
	}
	return len(p), nil
}

// OutputSeq reports how many non-empty [Terminal.Write] calls have completed,
// so a waiter that sees a new value sees the screen that write left. An empty
// write does not advance it.
func (t *Terminal) OutputSeq() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.outputSeq
}

// writeLocked copies p into the reusable feed buffer and calls
// ghostty_terminal_vt_write. The feed buffer is feedSize bytes, so
// large writes are split. The call never fails. Malformed input stays
// inside the emulator.
func (t *Terminal) writeLocked(p []byte) error {
	for len(p) > 0 {
		n := min(len(p), feedSize)
		t.mod.Put(t.feed, p[:n])
		t.mod.Xghostty_terminal_vt_write(t.term, t.feed, int32(n))
		p = p[n:]
	}
	return nil
}

// Read reads pty replies produced while processing VT. It blocks until a
// reply arrives or the terminal is closed.
func (t *Terminal) Read(p []byte) (int, error) {
	return t.ReadContext(context.Background(), p)
}

// ReadContext is [Terminal.Read], which also returns ctx's error once ctx is
// done, unless there is something to read by then. What it has not read is
// left for the next read.
func (t *Terminal) ReadContext(ctx context.Context, p []byte) (int, error) {
	t.mu.Lock()
	replies := t.replies
	t.mu.Unlock()
	if replies == nil {
		return 0, io.EOF
	}
	return replies.Read(ctx, p)
}

// Close resets the emulator and returns it to the pool. The Wasm image is
// kept. Close is safe to call more than once.
func (t *Terminal) Close() error {
	t.mu.Lock()
	if t.idle {
		t.mu.Unlock()
		return nil
	}
	t.freeSearchesLocked()
	t.mod.Xghostty_terminal_reset(t.term)
	t.resetStateLocked()
	t.clearPooledLocked()
	err := t.reapplyLocked()
	t.idle = true
	replies := t.replies
	t.mu.Unlock()
	replies.Close()
	// One that could not be set up again, or holds too much memory, is not
	// reused.
	t.rt.put(t, err == nil && len(t.mod.Memory()) <= maxPooledMemory)
	return err
}

// Reset performs a full terminal reset and restores the forced linefeed
// mode and scrollback cap.
func (t *Terminal) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil
	}
	t.mod.Xghostty_terminal_reset(t.term)
	t.resetStateLocked()
	t.gridFresh = false
	return t.reapplyLocked()
}

func (t *Terminal) resetStateLocked() {
	t.title = ""
	t.pwd = ""
	t.bell = Bell{}
	t.alt = false
	t.held = false
	t.frozen = nil
	t.progress = Progress{}
	t.notifications = nil
	t.clipWrote = ClipboardWrite{}
	t.prompt = Prompt{}
	t.setProgramStatusesLocked(nil, false)
	t.resizeOK = false
	t.grid = nil
	t.invalidateRowWords()
	t.gridFresh = false
	t.hasCursor = false
	t.kitty.reset()
}

// Resize changes the grid. Cell pixels stay as [WithCellSize] set them, so
// size reports stay non-zero.
func (t *Terminal) Resize(cols, rows int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errors.New("vt: terminal is closed")
	}
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("vt: resize %dx%d", cols, rows)
	}
	if err := t.resizeLocked(cols, rows); err != nil {
		return err
	}
	t.gridFresh = false
	return nil
}

// resizeLocked calls ghostty_terminal_resize. Cell width and height
// are the pixel size used by mouse encoding and in-band size reports.
// They are not changed here.
func (t *Terminal) resizeLocked(cols, rows int) error {
	rc := t.mod.Xghostty_terminal_resize(t.term, wasmvt.Int32(cols), wasmvt.Int32(rows), wasmvt.AsI32(t.cellW), wasmvt.AsI32(t.cellH))
	if err := wasmvt.Result("resize", rc); err != nil {
		return err
	}
	if cols != t.cols || rows != t.rows {
		t.invalidateRowWords()
	}
	t.cols, t.rows = cols, rows
	return nil
}

// CellSize returns the pixel size of one cell, which size reports and
// Kitty graphics placements use. See [WithCellSize].
func (t *Terminal) CellSize() (width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return int(t.cellW), int(t.cellH)
}

// Size returns the grid in cells.
func (t *Terminal) Size() (cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}

// Text returns the viewport as plain lines padded to the terminal width.
func (t *Terminal) Text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.held && t.frozen != nil {
		return t.frozen.Text()
	}
	return t.screenLocked().Text()
}

// Screen returns a copy of the viewport grid. The copy stays stable if the
// terminal is written again. Dirty marks rows that changed since the previous
// read. While synchronized output is held, the copy is the frame captured
// when the hold began.
func (t *Terminal) Screen() *Buffer {
	t.mu.Lock()
	defer t.mu.Unlock()
	grid := t.screenLocked()
	if grid == nil {
		return nil
	}
	return grid.Clone()
}

// Cursor returns the caret from the latest grid read.
func (t *Terminal) Cursor() Cursor {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorLocked()
}

func (t *Terminal) cursorLocked() Cursor {
	if t.held && t.frozen != nil {
		return t.frozenCur
	}
	if !t.gridFresh {
		t.screenLocked()
	}
	return t.gridCursor
}

// GridGeneration returns the content generation of the viewport grid
// [Terminal.Screen] would copy. It does not copy cells. Zero means there
// is no grid. A caller that already holds a screen copy can keep that
// copy while this value is unchanged.
func (t *Terminal) GridGeneration() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gridGenerationLocked()
}

func (t *Terminal) gridGenerationLocked() uint64 {
	if t.held && t.frozen != nil {
		return t.frozen.Generation()
	}
	if !t.gridFresh {
		t.screenLocked()
	}
	return t.grid.Generation()
}

// State returns the state around the viewport grid, read under one lock.
// Generation is the content generation [Terminal.Screen] would copy, but
// the grid itself is not copied. Focused is false: the terminal does not
// track focus, so whoever drives it sets that field; a renderer draws a
// state left unfocused as a dimmed window.
func (t *Terminal) State() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur := t.cursorLocked()
	gen := t.gridGenerationLocked()
	t.alt = t.activeAlt()
	cc, hasCursor := t.cursorColorLocked()
	return State{
		Title:            t.title,
		AltScreen:        t.alt,
		Cursor:           cur,
		Foreground:       t.shownColor(t.fgOverride, t.fgDefault, wasmvt.TerminalDataColorForeground, t.fg),
		Background:       t.shownColor(t.bgOverride, t.bgDefault, wasmvt.TerminalDataColorBackground, t.bg),
		CursorColor:      cc,
		CursorColorSet:   hasCursor,
		ScrollbackLen:    t.scrollbackLenLocked(),
		ScrollbackOffset: t.scrollbackOffsetLocked(),
		Progress:         t.progress,
		Generation:       gen,

		ProgramStatuses:         t.statusView,
		ProgramStatusGeneration: t.statusGen,
		ProgramStatusReported:   t.statusReported,

		WorkingDirectory: t.pwd,
		Bell:             t.bell,
		Notifications:    t.notifications[:len(t.notifications):len(t.notifications)],
		Clipboard:        t.clipWrote,
		Prompt:           t.prompt,
	}
}

// Held reports synchronized-output mode 2026. Image capture skips frames
// while this is true.
func (t *Terminal) Held() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.held
}

// Title returns the last OSC 0 / OSC 2 title.
func (t *Terminal) Title() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.title
}

// WorkingDirectory returns the last OSC 7 directory.
func (t *Terminal) WorkingDirectory() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pwd
}

// Bell returns the latest BEL.
func (t *Terminal) Bell() Bell {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bell
}

// AltScreen reports whether the alternate screen is active.
func (t *Terminal) AltScreen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alt = t.activeAlt()
	return t.alt
}

// CloseReader unblocks [Terminal.Read] with EOF. The grid stays available
// until [Terminal.Close].
func (t *Terminal) CloseReader() {
	t.mu.Lock()
	replies := t.replies
	t.mu.Unlock()
	if replies != nil {
		replies.Close()
	}
}

// Progress returns the latest OSC 9;4 report.
func (t *Terminal) Progress() Progress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.progress
}

// Notifications returns desktop notifications since the terminal was last
// reset, oldest first. The slice is a copy.
func (t *Terminal) Notifications() []Notification {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Notification(nil), t.notifications...)
}

// Clipboard returns the program's latest clipboard write.
func (t *Terminal) Clipboard() ClipboardWrite {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.clipWrote
}

// SetClipboard sets the text a program-initiated clipboard read returns.
func (t *Terminal) SetClipboard(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipRead = text
}

// Prompt returns the latest OSC 133 step.
func (t *Terminal) Prompt() Prompt {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.prompt
}

// ResizeRequest returns the last CSI 8 t request. ok is false until the
// program asks. The harness is not resized.
func (t *Terminal) ResizeRequest() (cols, rows int, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.resizeCols, t.resizeRows, t.resizeOK
}

// Foreground returns the override color, or the default when the override is clear.
func (t *Terminal) Foreground() color.NRGBA {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.shownColor(t.fgOverride, t.fgDefault, wasmvt.TerminalDataColorForeground, t.fg)
}

// Background returns the override color, or the default when the override is clear.
func (t *Terminal) Background() color.NRGBA {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.shownColor(t.bgOverride, t.bgDefault, wasmvt.TerminalDataColorBackground, t.bg)
}

// CursorColor returns the cursor color and whether the host or the program
// (OSC 12) set one.
func (t *Terminal) CursorColor() (color.NRGBA, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorColorLocked()
}

func (t *Terminal) cursorColorLocked() (color.NRGBA, bool) {
	if t.cursorOverride == nil && t.cursorDefault == nil {
		// Without a host color, Ghostty reports one only once the program
		// sets it. Otherwise the read falls back to t.cursor, which is
		// transparent.
		c := t.readRGB(wasmvt.TerminalDataColorCursor, t.cursor)
		return c, c.A != 0
	}
	return t.shownColor(t.cursorOverride, t.cursorDefault, wasmvt.TerminalDataColorCursor, t.cursor), true
}

// HostColors returns the foreground, background, and cursor colors the host
// set, each the current color if set, else the default. A color is nil when
// the host set neither, so the terminal's own (or the program's) applies.
// Non-nil colors are [color.NRGBA].
func (t *Terminal) HostColors() (fg, bg, cursor color.Color) {
	t.mu.Lock()
	defer t.mu.Unlock()
	host := func(override, def color.Color) color.Color {
		if c := firstColor(override, def); c != nil {
			return colorNRGBA(c)
		}
		return nil
	}
	return host(t.fgOverride, t.fgDefault), host(t.bgOverride, t.bgDefault), host(t.cursorOverride, t.cursorDefault)
}

// SetForeground sets the current foreground. Nil clears it back to the default.
func (t *Terminal) SetForeground(c color.Color) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fgOverride = c
	return t.applyColorLocked(wasmvt.TerminalOptionColorForeground, firstColor(c, t.fgDefault))
}

// SetBackground sets the current background. Nil clears it back to the default.
func (t *Terminal) SetBackground(c color.Color) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bgOverride = c
	return t.applyColorLocked(wasmvt.TerminalOptionColorBackground, firstColor(c, t.bgDefault))
}

// SetCursorColor sets the current cursor color. Nil clears it back to the default.
func (t *Terminal) SetCursorColor(c color.Color) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cursorOverride = c
	t.hasCursor = c != nil || t.cursorDefault != nil
	return t.applyColorLocked(wasmvt.TerminalOptionColorCursor, firstColor(c, t.cursorDefault))
}

// SetDefaultForeground sets the color used when the current foreground is cleared.
func (t *Terminal) SetDefaultForeground(c color.Color) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fgDefault = c
	if t.fgOverride != nil {
		return nil
	}
	return t.applyColorLocked(wasmvt.TerminalOptionColorForeground, c)
}

// SetDefaultBackground sets the color used when the current background is cleared.
func (t *Terminal) SetDefaultBackground(c color.Color) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bgDefault = c
	if t.bgOverride != nil {
		return nil
	}
	return t.applyColorLocked(wasmvt.TerminalOptionColorBackground, c)
}

// SetDefaultCursorColor sets the color used when the current cursor color is cleared.
func (t *Terminal) SetDefaultCursorColor(c color.Color) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cursorDefault = c
	t.hasCursor = t.cursorOverride != nil || c != nil
	if t.cursorOverride != nil {
		return nil
	}
	return t.applyColorLocked(wasmvt.TerminalOptionColorCursor, c)
}

func (t *Terminal) shownColor(override, def color.Color, data int32, fallback color.NRGBA) color.NRGBA {
	if override != nil {
		return colorNRGBA(override)
	}
	if def != nil {
		return colorNRGBA(def)
	}
	return t.readRGB(data, fallback)
}

func firstColor(a, b color.Color) color.Color {
	if a != nil {
		return a
	}
	return b
}

func colorNRGBA(c color.Color) color.NRGBA {
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8 & 0xff), G: uint8(g >> 8 & 0xff), B: uint8(b >> 8 & 0xff), A: uint8(a >> 8 & 0xff)}
}

// Mode reports whether mode is set. ok is false when the terminal does not
// recognize the mode.
func (t *Terminal) Mode(mode Mode) (on, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	on, ok, _ = t.modeKnownLocked(mode)
	return on, ok
}

// ScrollbackLen returns the number of scrollback rows. It is zero on the
// alternate screen.
func (t *Terminal) ScrollbackLen() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scrollbackLenLocked()
}

// ScrollbackOffset returns how many rows the viewport is scrolled up from the
// bottom, so zero while it follows live output. It is zero on the alternate
// screen.
func (t *Terminal) ScrollbackOffset() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scrollbackOffsetLocked()
}

// ScrollTo scrolls the viewport to offset rows up from the bottom, clamped to
// the scrollback: zero is the live screen. Like a real terminal's scrollbar, it
// changes what [Terminal.Screen] shows but not what the program sees, and the
// viewport stays where it is as the program writes more, until it is scrolled
// back to the bottom. The alternate screen has no scrollback, so it changes
// nothing there.
func (t *Terminal) ScrollTo(offset int) error {
	return t.scroll(func(int) int { return offset })
}

// ScrollUp scrolls the viewport n rows toward older scrollback, stopping at
// the oldest row. See [Terminal.ScrollTo]. n <= 0 changes nothing.
func (t *Terminal) ScrollUp(n int) error {
	return t.scroll(func(cur int) int { return cur + min(max(n, 0), math.MaxInt-cur) })
}

// ScrollDown scrolls the viewport n rows toward the live screen, stopping at
// the bottom. See [Terminal.ScrollTo]. n <= 0 changes nothing.
func (t *Terminal) ScrollDown(n int) error {
	return t.scroll(func(cur int) int { return cur - max(n, 0) })
}

// ScrollToTop scrolls the viewport to the oldest scrollback row. See
// [Terminal.ScrollTo].
func (t *Terminal) ScrollToTop() error { return t.ScrollTo(math.MaxInt) }

// ScrollToBottom scrolls the viewport back to the live screen. See
// [Terminal.ScrollTo].
func (t *Terminal) ScrollToBottom() error { return t.ScrollTo(0) }

// scroll moves the viewport to the offset next returns for the current one,
// clamped to the scrollback.
func (t *Terminal) scroll(next func(cur int) int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	if t.alt = t.activeAlt(); t.alt {
		return nil
	}
	n, cur, ok := t.viewportLocked()
	if !ok {
		return errors.New("vt: cannot read the viewport position")
	}
	if want := max(0, min(n, next(cur))); want != cur {
		t.setOffsetLocked(want, n)
	}
	return nil
}

// SetScrollbackMaxLines sets the retained history cap. Zero disables it.
// A later positive cap enables history again. Reset keeps the cap.
func (t *Terminal) SetScrollbackMaxLines(n int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	if n < 0 {
		n = 0
	}
	t.scrollbackLines = n
	return t.applyScrollbackLocked()
}

// applyScrollbackLocked installs scrollbackLines. A zero cap sets the
// byte limit to zero, which is libghostty's no-scrollback switch. A
// positive cap clears that switch before applying the line limit.
func (t *Terminal) applyScrollbackLocked() error {
	if t.scrollbackLines == 0 {
		t.scrollbackOff = true
		return t.mod.SetU32Option(t.term, wasmvt.TerminalOptionScrollbackMaxBytes, 0)
	}
	if t.scrollbackOff {
		if err := t.mod.SetOption(t.term, wasmvt.TerminalOptionScrollbackMaxBytes, 0); err != nil {
			return err
		}
		t.scrollbackOff = false
	}
	return t.mod.SetU32Option(t.term, wasmvt.TerminalOptionScrollbackMaxLines, wasmvt.Uint32(t.scrollbackLines))
}

// ClearScrollback erases retained history.
func (t *Terminal) ClearScrollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.writeLocked([]byte("\x1b[3J")); err != nil {
		return err
	}
	t.gridFresh = false
	return nil
}

// prepare builds the Wasm objects for a new instance. Opaque handles
// come from [wasmvt.Module.Ctor], which uses ghostty_wasm_alloc_opaque
// and the matching ghostty_*_new export. feed, scratch, and the ENQ
// and XTVERSION reply bytes stay allocated for the life of the module.
// Callbacks are installed once. Scrollback and the forced linefeed
// mode are applied after the handles exist.
func (t *Terminal) prepare(cols, rows, scrollback int) error {
	var err error
	t.replies = newReplyBuf()
	t.cellW = defaultCellWidth
	t.cellH = defaultCellHeight
	t.scrollbackLines = scrollback
	t.kitty = kittyState{limit: DefaultKittyStorage, def: DefaultKittyStorage}
	if t.feed, err = t.mod.Alloc(feedSize); err != nil {
		return err
	}
	if t.scratch, err = t.mod.Alloc(scratchSize); err != nil {
		return err
	}
	if t.kitty.scratch, err = t.mod.Alloc(kittyScratchSize); err != nil {
		return err
	}
	ack := []byte{0x06}
	if t.answer, err = t.mod.Alloc(len(ack)); err != nil {
		return err
	}
	t.mod.Put(t.answer, ack)
	ver := []byte(xtversion)
	if t.version, err = t.mod.Alloc(len(ver)); err != nil {
		return err
	}
	t.mod.Put(t.version, ver)
	t.term, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_terminal_new(0, slot, wasmvt.Int32(cols), wasmvt.Int32(rows))
	})
	if err != nil {
		return err
	}
	t.cols, t.rows = cols, rows
	t.render, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_render_state_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.rowsIt, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_render_state_row_iterator_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.cells, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_render_state_row_cells_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.keys, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_key_encoder_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.keyEv, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_key_event_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.mice, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_mouse_encoder_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.mouseEv, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_mouse_event_new(0, slot)
	})
	if err != nil {
		return err
	}
	t.kitty.iter, err = t.mod.Ctor(func(slot int32) int32 {
		return t.mod.Xghostty_kitty_graphics_placement_iterator_new(0, slot)
	})
	if err != nil {
		return err
	}
	err = t.installCallbacks()
	if err != nil {
		return err
	}
	err = t.resizeLocked(cols, rows)
	if err != nil {
		return err
	}
	err = t.reapplyLocked()
	if err != nil {
		return err
	}
	t.fg = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	t.bg = color.NRGBA{A: 0xff}
	return nil
}

// reapplyLocked restores options that [Terminal.Reset] and checkout
// clear. Scrollback, the checksum host options, and mode 20 (the
// linefeed new-line mode this host forces on) are applied again.
func (t *Terminal) reapplyLocked() error {
	if err := t.applyScrollbackLocked(); err != nil {
		return err
	}
	if err := t.mod.SetBoolOption(t.term, wasmvt.TerminalOptionXtChecksumReport, t.checksumReport); err != nil {
		return err
	}
	if err := t.mod.SetU8Option(t.term, wasmvt.TerminalOptionXtChecksumExtension, byte(t.checksumFlags)); err != nil {
		return err
	}
	if err := t.setModeLocked(Mode{Number: 20, ANSI: true}, true); err != nil {
		return err
	}
	return t.mod.SetU64Option(t.term, wasmvt.TerminalOptionKittyImageStorageLimit, t.kitty.limit)
}

// clearPooledLocked drops what belongs to the previous checkout: the
// checksum switches, the host colors, and the colors the program set with
// OSC 10, 11, and 12, which a full reset keeps. [Terminal.Reset] keeps the
// host options.
func (t *Terminal) clearPooledLocked() {
	t.checksumReport = false
	t.checksumFlags = 0
	t.fgOverride, t.bgOverride, t.cursorOverride = nil, nil, nil
	t.fgDefault, t.bgDefault, t.cursorDefault = nil, nil, nil
	t.hasCursor = false
	for _, opt := range [...]int32{
		wasmvt.TerminalOptionColorForeground,
		wasmvt.TerminalOptionColorBackground,
		wasmvt.TerminalOptionColorCursor,
	} {
		_ = t.mod.SetOption(t.term, opt, 0)
	}
	_ = t.writeLocked([]byte("\x1b]110\x07\x1b]111\x07\x1b]112\x07"))
}

func (t *Terminal) checkout(cols, rows, scrollback int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.idle = false
	t.outputSeq = 0
	t.replies = newReplyBuf()
	t.scrollbackLines = scrollback
	t.resetStateLocked()
	t.clearPooledLocked()
	if err := t.resizeLocked(cols, rows); err != nil {
		return err
	}
	return t.reapplyLocked()
}
