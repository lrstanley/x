// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"fmt"
	"image/color"
	"io"
	"sync"
	"time"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

const (
	defaultScrollback = 1000
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

	// title is the last OSC 0 or OSC 2 window title.
	title string
	// pwd is the last OSC 7 working directory.
	pwd string
	// bell is when the last BEL arrived. The zero time means none yet.
	bell time.Time
	// alt is true when the alternate screen is active.
	alt bool
	// held is true during synchronized output (mode 2026).
	held bool
	// frozen is the grid captured when the current hold started. Reads
	// return it instead of a partial update. frozenCur is its caret.
	frozen    *screen.Buffer
	frozenCur screen.Cursor

	// progress is the latest OSC 9;4 report.
	progress screen.Progress
	// notifications are OSC 9 and OSC 777 messages, oldest first.
	notifications []Notification
	// clipRead is the host clipboard returned for an OSC 52 read.
	clipRead string
	// clipWrote is the last plain text the program wrote to the clipboard.
	clipWrote string
	// prompt is the latest OSC 133 semantic-prompt step.
	prompt Prompt
	// statuses are OSC 7501 program-status records in update order, least
	// recently updated first, which is the eviction order.
	statuses []screen.ProgramStatus
	// statusView is statuses as [Terminal.ProgramStatuses] returns them:
	// sorted by id, with App inherited. It is replaced, never changed in
	// place, so a [screen.State] can share it. statusGen counts changes to it,
	// and is never reset, so equal generations mean the same records.
	statusView []screen.ProgramStatus
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
	grid *screen.Buffer
	// gridCursor is the caret from the same refresh as grid.
	gridCursor screen.Cursor
	// gridFresh is true when grid matches the emulator since the last write.
	gridFresh bool
	// outputSeq counts non-empty Write calls that have finished their notify
	// callback. Waiters observe a new value only after that callback returns.
	outputSeq uint64
	// onWrite runs after each non-empty Write, without mu held. Close clears it.
	onWrite func()
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

	// kittyLimit is the direct-payload byte cap. Zero disables storage.
	// This Wasm build has no filesystem, so file transmissions are ignored.
	kittyLimit uint64
	// kittyImages are stored images keyed by protocol id.
	kittyImages map[uint32]*kittyImage
	// kittyPlacements are direct placements, oldest first. Col and Row
	// are the cursor cell at placement time.
	kittyPlacements []KittyPlacement
	// kittyScan is the APC parser carried across Write chunks.
	kittyScan kittyScan
}

// Write feeds VT bytes to the emulator. It never returns a short write.
// A non-empty write runs [Terminal.SetWriteNotify] after the bytes are
// parsed and the lock is released, then advances [Terminal.OutputSeq].
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	if t.idle {
		t.mu.Unlock()
		return 0, fmt.Errorf("vt: terminal is closed")
	}
	if err := t.writeLocked(p); err != nil {
		t.mu.Unlock()
		return 0, err
	}
	t.gridFresh = false
	fn := t.onWrite
	t.mu.Unlock()
	if len(p) > 0 && fn != nil {
		fn()
	}
	if len(p) > 0 {
		t.mu.Lock()
		t.outputSeq++
		t.mu.Unlock()
	}
	return len(p), nil
}

// OutputSeq reports how many non-empty [Terminal.Write] calls have completed.
// The count advances after a [Terminal.SetWriteNotify] callback returns, so a
// waiter that sees a new value also sees that callback's effects. An empty
// write does not advance it.
func (t *Terminal) OutputSeq() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.outputSeq
}

// SetWriteNotify registers fn to run after each non-empty [Terminal.Write],
// on the writer goroutine, once the bytes are parsed and the terminal lock
// is released. [Terminal.OutputSeq] advances after fn returns. fn may call
// back into the terminal; it must not call SetWriteNotify. A nil fn clears
// the hook. [Terminal.Close] clears it.
func (t *Terminal) SetWriteNotify(fn func()) {
	t.mu.Lock()
	t.onWrite = fn
	t.mu.Unlock()
}

// writeLocked copies p into the reusable feed buffer and calls
// ghostty_terminal_vt_write. The feed buffer is feedSize bytes, so
// large writes are split. The call never fails. Malformed input stays
// inside the emulator. Kitty graphics are tracked separately because
// this Wasm build does not export ghostty_kitty_graphics_*.
func (t *Terminal) writeLocked(p []byte) error {
	for len(p) > 0 {
		n := min(len(p), feedSize)
		t.mod.Put(t.feed, p[:n])
		t.mod.Xghostty_terminal_vt_write(t.term, t.feed, int32(n))
		t.feedKitty(p[:n])
		p = p[n:]
	}
	return nil
}

// Read reads pty replies produced while processing VT. It blocks until a
// reply arrives or the terminal is closed.
func (t *Terminal) Read(p []byte) (int, error) {
	if t.replies == nil {
		return 0, io.EOF
	}
	return t.replies.Read(p)
}

// Close resets the emulator and returns it to the pool. The Wasm image is
// kept. Close is safe to call more than once.
func (t *Terminal) Close() error {
	t.mu.Lock()
	if t.idle {
		t.mu.Unlock()
		return nil
	}
	t.onWrite = nil
	t.freeSearchesLocked()
	t.mod.Xghostty_terminal_reset(t.term)
	t.resetStateLocked()
	t.clearPooledLocked()
	t.reapplyLocked()
	t.idle = true
	replies := t.replies
	t.mu.Unlock()
	replies.Close()
	t.rt.put(t)
	return nil
}

// Reset performs a full terminal reset and restores the forced linefeed
// mode and scrollback cap.
func (t *Terminal) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return
	}
	t.mod.Xghostty_terminal_reset(t.term)
	t.resetStateLocked()
	t.reapplyLocked()
	t.gridFresh = false
}

func (t *Terminal) resetStateLocked() {
	t.title = ""
	t.pwd = ""
	t.bell = time.Time{}
	t.alt = false
	t.held = false
	t.frozen = nil
	t.progress = screen.Progress{}
	t.notifications = nil
	t.clipWrote = ""
	t.prompt = Prompt{}
	t.setProgramStatusesLocked(nil, false)
	t.resizeOK = false
	t.grid = nil
	t.invalidateRowWords()
	t.gridFresh = false
	t.hasCursor = false
	t.kittyLimit = 0
	t.kittyImages = nil
	t.kittyPlacements = nil
	t.kittyScan = kittyScan{}
}

// Resize changes the grid. Cell pixels stay at the instance default so
// size reports stay non-zero.
func (t *Terminal) Resize(cols, rows int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return fmt.Errorf("vt: terminal is closed")
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
	rc := t.mod.Xghostty_terminal_resize(t.term, int32(cols), int32(rows), int32(t.cellW), int32(t.cellH))
	if err := wasmvt.Result("resize", rc); err != nil {
		return err
	}
	if cols != t.cols || rows != t.rows {
		t.invalidateRowWords()
	}
	t.cols, t.rows = cols, rows
	return nil
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
func (t *Terminal) Screen() *screen.Buffer {
	t.mu.Lock()
	defer t.mu.Unlock()
	grid := t.screenLocked()
	if grid == nil {
		return nil
	}
	return grid.Clone()
}

// Cursor returns the caret from the latest grid read.
func (t *Terminal) Cursor() screen.Cursor {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorLocked()
}

func (t *Terminal) cursorLocked() screen.Cursor {
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
func (t *Terminal) State() screen.State {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur := t.cursorLocked()
	gen := t.gridGenerationLocked()
	t.alt = t.activeAlt()
	cc, hasCursor := t.cursorColorLocked()
	return screen.State{
		Title:          t.title,
		AltScreen:      t.alt,
		Cursor:         cur,
		Foreground:     t.shownColor(t.fgOverride, t.fgDefault, wasmvt.TerminalDataColorForeground, t.fg),
		Background:     t.shownColor(t.bgOverride, t.bgDefault, wasmvt.TerminalDataColorBackground, t.bg),
		CursorColor:    cc,
		CursorColorSet: hasCursor,
		ScrollbackLen:  t.scrollbackLenLocked(),
		Progress:       t.progress,
		Generation:     gen,

		ProgramStatuses:         t.statusView,
		ProgramStatusGeneration: t.statusGen,
		ProgramStatusReported:   t.statusReported,
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

// LastBellAt returns the time of the most recent BEL. The zero time means
// no bell has arrived.
func (t *Terminal) LastBellAt() time.Time {
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
func (t *Terminal) Progress() screen.Progress {
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

// Clipboard returns the text the program last wrote.
func (t *Terminal) Clipboard() string {
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
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
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
	return t.mod.SetU32Option(t.term, wasmvt.TerminalOptionScrollbackMaxLines, uint32(t.scrollbackLines))
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
	if t.feed, err = t.mod.Alloc(feedSize); err != nil {
		return err
	}
	if t.scratch, err = t.mod.Alloc(scratchSize); err != nil {
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
		return t.mod.Xghostty_terminal_new(0, slot, int32(cols), int32(rows))
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
	if err := t.installCallbacks(); err != nil {
		return err
	}
	if err := t.resizeLocked(cols, rows); err != nil {
		return err
	}
	if err := t.reapplyLocked(); err != nil {
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
	return nil
}

// clearPooledLocked drops host options that belong to the previous
// checkout. [Terminal.Reset] keeps them.
func (t *Terminal) clearPooledLocked() {
	t.checksumReport = false
	t.checksumFlags = 0
}

func (t *Terminal) checkout(cols, rows, scrollback int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.idle = false
	t.onWrite = nil
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
