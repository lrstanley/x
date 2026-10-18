// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/lrstanley/x/term/still/export"
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
	frameDelay time.Duration

	gifCh   chan export.Frame
	gifRec  export.FrameRecorder
	gifPath string

	lastPNG  image.Image
	lastSnap []byte
	log      *hookLog
	logPath  string
}

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

	delay := cfg.slowExport
	if delay == 0 {
		delay = defaultSlowExport
	}

	maxFrames := cfg.gifMaxFrames
	if maxFrames == 0 {
		maxFrames = 100
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
		frameDelay: delay,
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}

	if types.has(ExportTypeGIF) && cfg.exportMode != ExportOff {
		m.gifCh = make(chan export.Frame)
		switch cfg.exportMode { //nolint:exhaustive
		case ExportOn:
			m.gifPath = filepath.Join(dir, m.stem()+".gif")
		case ExportOnFailure:
			m.gifPath = filepath.Join(dir, m.stem()+"_failed.gif")
		}
		rec, err := export.GIF(
			append(
				m.exportOpts,
				export.WithMaxFrames(maxFrames),
				export.WithChannel(m.gifCh),
			)...,
		)
		if err != nil {
			return nil, err
		}
		m.gifRec = rec
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

func (m *exportManager) hook(event HookEvent, stage HookStage, _ any) error {
	if stage != HookStageAfter || !exportCaptureEvent(event) || event == HookClose {
		return nil
	}
	if m.types.has(ExportTypeGIF) {
		if err := m.captureGIF(); err != nil {
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

func (m *exportManager) captureGIF() error {
	if m.gifCh == nil || m.h.emulator.term.Held() {
		return nil
	}
	m.gifCh <- export.Frame{Image: m.h.copyImage(), Delay: m.frameDelay}
	return nil
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

	if m.gifCh != nil {
		close(m.gifCh)
		m.gifCh = nil
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
	src := h.drawImage()
	if src == nil {
		return nil
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}
