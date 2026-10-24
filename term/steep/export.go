// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"cmp"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/lrstanley/x/term/still/export"
	"github.com/lrstanley/x/term/vt"
	"github.com/lrstanley/x/term/vt/screen"
)

// ExportMode controls when harness export artifacts are written.
type ExportMode int

const (
	// ExportOff disables built-in export hooks unless [ExportTypeLog] is set.
	ExportOff ExportMode = iota
	// ExportOnFailure buffers artifacts in memory and writes them at [Harness.Close]
	// when the test failed.
	ExportOnFailure
	// ExportOn writes artifacts continuously during the test.
	ExportOn
)

// ExportType selects built-in export artifact kinds.
type ExportType uint

const (
	ExportTypePNG ExportType = 1 << iota
	ExportTypeGIF
	ExportTypeLog
	ExportTypeSnapshot
)

// ExportTypeAll enables every export type.
const ExportTypeAll = ExportTypePNG | ExportTypeGIF | ExportTypeLog | ExportTypeSnapshot

func (t ExportType) has(flag ExportType) bool {
	return t&flag != 0
}

func exportCaptureEvent(event HookEvent) bool {
	switch event {
	case HookInput, HookTerminal, HookProgram, HookSettle, HookReady, HookClose:
		return true
	default:
		return false
	}
}

type exportManager struct {
	h          *Harness
	tb         testing.TB
	mode       ExportMode
	types      ExportType
	dir        string
	baseName   string
	exportOpts []export.Option

	gifCh   chan export.Frame
	gifRec  export.FrameRecorder
	gifPath string

	// frames turns captured frames into GIF frames; keys and cards belong to
	// its built-in stages. All three are only used on the test goroutine.
	frames exportPipeline
	keys   *keysOverlay
	cards  cardRenderer

	// mu guards the commit queue and the duplicate check. noteCommit runs on
	// the program goroutine; flush and hook capture run on the test goroutine.
	mu       sync.Mutex
	stopped  bool
	pending  []commitFrame
	haveSent bool
	sent     vt.Snapshot
	heldSkip bool
	// droppedPending records that the pending queue overflowed, so it is only
	// reported once.
	droppedPending bool

	// pristine is the emulator state before the program has written anything.
	pristine vt.Snapshot

	lastPNG  image.Image
	lastSnap []byte
	log      *hookLog
	logPath  string
}

// commitFrame is one visible terminal commit. The screen clone outlives
// later writes. state is the metadata Draw needs for that same moment.
type commitFrame struct {
	screen *screen.Buffer
	state  vt.Snapshot
	at     time.Time
}

const (
	// exportQueueSize sizes the channel to the encoder. Each queued frame is a
	// full-size RGBA image, so this only smooths over jitter between the test and
	// the encoder; a slower encoder blocks the test instead of piling up frames.
	// It does not limit how many frames the recording keeps.
	exportQueueSize = 4
	// maxPendingCommits bounds the commits queued between test hooks. Each holds
	// a clone of the screen, and the queue is drained at every hook and while
	// harness waits and [Harness.Sleep] poll, so reaching this means the
	// program painted far faster than the test looked at it.
	maxPendingCommits = 100
)

func newExportManager(h *Harness, cfg options) (*exportManager, error) {
	if cfg.exportMode == ExportOff && cfg.exportTypes == 0 {
		return nil, nil
	}
	types := cfg.exportTypes
	if cfg.exportMode != ExportOff && types == 0 {
		types = ExportTypeGIF | ExportTypePNG
	}

	dir := cfg.exportDir
	if dir == "" {
		dir = artifactDir(h.tb)
	}

	fps := min(max(cmp.Or(cfg.exportFPS, defaultExportFPS), 1), export.MaxFrameRate)
	speed := cfg.exportSpeed
	if !(speed > 0) { // Also catches NaN.
		speed = defaultExportSpeed
	}

	exportOpts := append([]export.Option(nil), cfg.exportOpts...)

	m := &exportManager{
		h:          h,
		tb:         h.tb,
		mode:       cfg.exportMode,
		types:      types,
		dir:        dir,
		baseName:   cfg.exportName,
		exportOpts: exportOpts,
	}
	m.pristine = h.emulator.term.Snapshot()
	m.cards.opts = cfg.imageRendererOpts
	m.frames = exportPipeline{
		base:  ExportContext{Mode: cfg.exportMode, Step: time.Second / time.Duration(fps), h: h, cards: &m.cards},
		speed: speed,
	}
	mutators := slices.Clip(cfg.exportMutators)
	gif := types.has(ExportTypeGIF) && cfg.exportMode != ExportOff
	if gif && cfg.exportKeys > 0 {
		m.keys = &keysOverlay{shown: cfg.exportKeys, speed: speed}
		mutators = append(mutators, m.keys.mutate)
	}
	for _, mutate := range append(mutators, cfg.exportIntro, cfg.exportOutro) {
		if mutate != nil {
			m.frames.stages = append(m.frames.stages, mutatorStage{mutate: mutate})
		}
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}

	if gif {
		m.gifCh = make(chan export.Frame, exportQueueSize)
		switch cfg.exportMode { //nolint:exhaustive
		case ExportOn:
			m.gifPath = filepath.Join(dir, m.stem()+".gif")
		case ExportOnFailure:
			m.gifPath = filepath.Join(dir, m.stem()+"_failed.gif")
		}
		rec, err := export.GIF(
			append(
				m.exportOpts,
				export.WithChannel(m.gifCh),
				export.WithDelayGrid(fps),
			)...,
		)
		if err != nil {
			return nil, err
		}
		m.gifRec = rec
		m.frames.out = m.gifCh
	}

	if types.has(ExportTypeLog) && cfg.exportMode == ExportOn {
		m.logPath = filepath.Join(dir, m.artifactName("log", ".jsonl", false))
		log, err := openHookLogFile(m.logPath)
		if err != nil {
			return nil, err
		}
		m.log = log
	}

	return m, nil
}

func (m *exportManager) hook(event HookEvent, stage HookStage, metadata any) error {
	if m.keys != nil && event == HookInput && stage == HookStageBefore {
		m.keys.press(metadata, time.Now())
	}
	if stage != HookStageAfter || !exportCaptureEvent(event) || event == HookClose {
		return nil
	}
	m.flushCommits()
	if m.types.has(ExportTypeGIF) {
		if err := m.captureGIF(event); err != nil {
			return err
		}
	}
	if m.types.has(ExportTypePNG) {
		if err := m.capturePNG(event); err != nil {
			return err
		}
	}
	if m.types.has(ExportTypeSnapshot) {
		if err := m.captureSnapshot(event); err != nil {
			return err
		}
	}
	return nil
}

// noteCommit queues a GIF frame for a program write that left a visible grid.
// It runs on the writer goroutine, after the write has been parsed, and
// returns before [vt.Terminal.OutputSeq] advances. A synchronized-output hold
// is not a frame; the write that ends the hold queues the released grid.
func (m *exportManager) noteCommit() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopped || m.gifCh == nil {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	term := m.h.emulator.term
	if term.Held() {
		return
	}
	st := term.Snapshot()
	if m.duplicateCommit(st) {
		return
	}
	scr := term.Screen()
	if scr == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.gifCh == nil || m.duplicateCommitLocked(st) {
		return
	}
	if m.beforeFirstFrameLocked() && m.isPristine(st, scr.Text()) {
		return
	}
	if len(m.pending) >= maxPendingCommits {
		m.pending[0] = commitFrame{}
		m.pending = m.pending[1:]
		if !m.droppedPending {
			m.droppedPending = true
			m.tb.Log("export dropped frames: the program painted faster than the test looked at it")
		}
	}
	m.pending = append(m.pending, commitFrame{screen: scr, state: st, at: time.Now()})
}

func (m *exportManager) duplicateCommit(st vt.Snapshot) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.duplicateCommitLocked(st)
}

func (m *exportManager) duplicateCommitLocked(st vt.Snapshot) bool {
	if m.haveSent && sameCommit(m.sent, st) {
		return true
	}
	n := len(m.pending)
	return n > 0 && sameCommit(m.pending[n-1].state, st)
}

// flushCommits renders queued commits into the GIF. It runs on the test
// goroutine so it can use the harness renderer without racing [Harness.Image].
func (m *exportManager) flushCommits() {
	if m == nil {
		return
	}
	for {
		m.mu.Lock()
		batch := m.pending
		m.pending = nil
		m.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		for _, frame := range batch {
			if m.duplicateCommit(frame.state) {
				continue
			}
			src := frameSource{cols: frame.screen.Width(), rows: frame.screen.Height(), state: frame.state}
			var img image.Image
			img, src.held = copyCommitted(m.h, frame.screen, frame.state, frame.at)
			m.sendFrame(src, img, frame.at)
		}
	}
}

func (m *exportManager) captureGIF(event HookEvent) error {
	if m.h.emulator.term.Held() {
		m.noteHeldSkip()
		return nil
	}
	src := frameSource{state: m.h.emulator.term.Snapshot()}
	if m.duplicateCommit(src.state) {
		return nil
	}
	if event == HookReady && m.beforeFirstFrame() && m.isPristine(src.state, m.h.emulator.term.Text()) {
		return nil
	}
	src.cols, src.rows = m.h.Dimensions()
	img := m.h.copyImage()
	m.h.imageMu.Lock()
	src.held = heldIndicatorsLocked(m.h, m.h.imageScreen)
	m.h.imageMu.Unlock()
	m.sendFrame(src, img, time.Now())
	return nil
}

func (m *exportManager) noteHeldSkip() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.heldSkip {
		return
	}
	m.heldSkip = true
	m.tb.Log("export skipped a frame: terminal synchronized output is held")
}

// sendFrame makes img the current GIF frame as of at, finishing the previous
// frame with the time it was on screen.
func (m *exportManager) sendFrame(src frameSource, img image.Image, at time.Time) {
	if img == nil {
		return
	}
	m.mu.Lock()
	if m.gifCh == nil || m.stopped || m.duplicateCommitLocked(src.state) {
		m.mu.Unlock()
		return
	}
	m.haveSent = true
	m.sent = src.state
	m.mu.Unlock()
	m.frames.record(img, at, src)
}

func (m *exportManager) beforeFirstFrame() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beforeFirstFrameLocked()
}

func (m *exportManager) beforeFirstFrameLocked() bool {
	return !m.haveSent && len(m.pending) == 0
}

// isPristine reports whether st and the screen text are indistinguishable from
// an emulator the program hasn't visibly written to yet. Bubble Tea starts by
// writing capability queries, which commit without changing anything, and
// [HookReady] can fire before its first paint. Exporting either as a frame
// shows a one-frame flicker of the default colors and cursor at 0,0 before the
// real first frame. This is only consulted until the first frame is recorded.
//
// TODO: this assumes the harness only drives Bubble Tea programs, where a
// pristine screen always means "hasn't rendered yet". If we add full exec
// support (launching a shell such as bash, typing into it, etc), a program can
// legitimately sit on a pristine-looking screen, so this should likely be
// removed or reworked at that point.
func (m *exportManager) isPristine(st vt.Snapshot, text string) bool {
	p := m.pristine
	return strings.TrimSpace(text) == "" &&
		st.Title == p.Title &&
		snapshotProgress(st) == snapshotProgress(p) &&
		st.ProgramStatusGeneration == p.ProgramStatusGeneration &&
		st.AltScreen == p.AltScreen &&
		st.Foreground == p.Foreground &&
		st.Background == p.Background &&
		st.Cursor.X == p.Cursor.X &&
		st.Cursor.Y == p.Cursor.Y &&
		st.Cursor.Visible == p.Cursor.Visible &&
		st.Cursor.Blink == p.Cursor.Blink &&
		st.Cursor.Shape == p.Cursor.Shape
}

func sameCommit(a, b vt.Snapshot) bool {
	if a.Generation != b.Generation || a.Title != b.Title || a.AltScreen != b.AltScreen ||
		snapshotProgress(a) != snapshotProgress(b) || a.ProgramStatusGeneration != b.ProgramStatusGeneration {
		return false
	}
	ac, bc := a.Cursor, b.Cursor
	return ac.X == bc.X && ac.Y == bc.Y && ac.Visible == bc.Visible && ac.Blink == bc.Blink && ac.Shape == bc.Shape
}

func (m *exportManager) capturePNG(event HookEvent) error {
	if m.h.emulator.term.Held() {
		return nil
	}
	img := m.h.copyImage()
	switch m.mode {
	case ExportOnFailure:
		m.lastPNG = img
	case ExportOn:
		path := filepath.Join(m.dir, m.artifactName(event.String(), ".png", false))
		return export.PNG(img, path, m.exportOpts...)
	}
	return nil
}

func (m *exportManager) captureSnapshot(event HookEvent) error {
	view := []byte(m.h.View())
	switch m.mode {
	case ExportOnFailure:
		m.lastSnap = append([]byte(nil), view...)
	case ExportOn:
		path := filepath.Join(m.dir, m.artifactName(event.String(), ".snap", false))
		return os.WriteFile(path, view, 0o600)
	}
	return nil
}

func (m *exportManager) Finalize(failed bool) error {
	if m == nil {
		return nil
	}

	// Commits still queued here were painted after the last hook, while the
	// program was shutting down: typically a repaint on exiting the alternate
	// screen, which is blank. A recording ends on the last state the test saw,
	// so they are discarded, not rendered.
	m.mu.Lock()
	m.stopped = true
	m.pending = nil
	ch := m.gifCh
	m.gifCh = nil
	m.mu.Unlock()
	if ch != nil {
		m.frames.finish(time.Now(), failed)
		close(ch)
	}
	if err := m.cards.close(); err != nil {
		return err
	}
	if m.gifRec != nil {
		if err := m.gifRec.Close(); err != nil {
			return err
		}
		switch m.mode {
		case ExportOn:
			if err := encodeGIFFile(m.gifPath, m.gifRec); err != nil {
				return err
			}
			if failed && m.gifPath != "" {
				data, err := os.ReadFile(m.gifPath)
				if err == nil && len(data) > 0 {
					failPath := filepath.Join(m.dir, m.stem()+"_failed.gif")
					_ = os.WriteFile(failPath, data, 0o600)
				}
			}
		case ExportOnFailure:
			if failed && len(m.gifRec.Frames()) > 0 {
				if err := encodeGIFFile(m.gifPath, m.gifRec); err != nil {
					return err
				}
			} else if m.gifPath != "" {
				_ = os.Remove(m.gifPath)
			}
		}
		m.gifRec = nil
	}

	if m.mode == ExportOnFailure {
		if !failed {
			return nil
		}
		if m.types.has(ExportTypePNG) && m.lastPNG != nil {
			path := filepath.Join(m.dir, m.stem()+"_failed.png")
			if err := export.PNG(m.lastPNG, path, m.exportOpts...); err != nil {
				return err
			}
		}
		if m.types.has(ExportTypeSnapshot) && len(m.lastSnap) > 0 {
			path := filepath.Join(m.dir, m.stem()+"_failed.snap")
			if err := os.WriteFile(path, m.lastSnap, 0o600); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *exportManager) stem() string {
	if m.baseName != "" {
		return m.baseName
	}
	return normalizeTestName(m.tb.Name())
}

func (m *exportManager) artifactName(event, ext string, failed bool) string {
	if m.baseName != "" && m.mode == ExportOn {
		name := m.baseName + "_" + event
		if failed {
			name += "_failed"
		}
		return name + ext
	}
	ts := time.Now().UTC().Format("20060102T150405.000")
	name := m.stem() + "_" + ts + "_" + event
	if failed {
		name += "_failed"
	}
	return name + ext
}

func normalizeTestName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "test"
	}
	return s
}

func artifactDir(tb testing.TB) string {
	tb.Helper()
	return tb.ArtifactDir()
}

func encodeGIFFile(path string, rec export.FrameRecorder) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return rec.EncodeAll(f)
}

func (h *Harness) copyImage() image.Image {
	h.tb.Helper()
	return cloneNRGBA(h.drawImage())
}
