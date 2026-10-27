// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/lrstanley/x/term/reel/sink"
)

// mediaTypes are the animated export types, in the order their outputs are
// created, with how each creates its encoder at path.
var mediaTypes = []struct {
	typ    ExportType
	ext    string
	create func(path string, opts ...sink.Option) (sink.Encoder, error)
}{
	{ExportTypeGIF, "gif", func(path string, opts ...sink.Option) (sink.Encoder, error) {
		return sink.CreateGIF(path, opts...)
	}},
	{ExportTypeMP4, "mp4", createVideo(sink.VideoMP4)},
	{ExportTypeWebM, "webm", createVideo(sink.VideoWebM)},
	{ExportTypeWebP, "webp", createVideo(sink.VideoWebP)},
}

func createVideo(format sink.VideoFormat) func(string, ...sink.Option) (sink.Encoder, error) {
	return func(path string, opts ...sink.Option) (sink.Encoder, error) {
		enc, err := sink.CreateVideo(path, format, opts...)
		if err != nil {
			return nil, fmt.Errorf(
				"steep: %w; remove %s from WithExport or STEEP_EXPORT_TYPE to run without it",
				err, format,
			)
		}
		return enc, nil
	}
}

// mediaOutput is one animated recording, the GIF or a video, with extension
// ext. Every recorder reads the same frames from its own channel in
// [exportPipeline.out].
type mediaOutput struct {
	ext string
	rec *sink.Recorder
}

// openOutputs starts a recorder for every animated type, so they all fail
// here, before the test runs, if one cannot record (such as a video without
// ffmpeg).
func (m *exportManager) openOutputs(fps int) error {
	for _, t := range mediaTypes {
		if !m.types.has(t.typ) {
			continue
		}
		enc, err := t.create(m.mediaPath(t.ext), m.exportOpts...)
		if err != nil {
			return err
		}
		ch := make(chan sink.Frame, exportQueueSize)
		rec, err := sink.Record(enc, append(
			slices.Clip(m.exportOpts),
			sink.WithChannel(ch),
			sink.WithDelayGrid(fps),
		)...)
		if err != nil {
			return err
		}
		m.outputs = append(m.outputs, &mediaOutput{ext: t.ext, rec: rec})
		m.frames.out = append(m.frames.out, ch)
	}
	return nil
}

// abortOutputs discards every output, for when construction fails after some
// were started.
func (m *exportManager) abortOutputs() {
	for _, o := range m.outputs {
		_ = o.rec.Discard()
	}
	m.outputs = nil
}

// finishOutput finishes o once its recorder has read every frame, applying
// the [ExportMode] rules. With [ExportOn] it keeps the recording, and also
// copies it to the failure name when the test failed. With [ExportOnFailure]
// it keeps the recording only when the test failed.
func (m *exportManager) finishOutput(o *mediaOutput, failed bool) error {
	if m.mode == ExportOnFailure {
		// A recording left by an earlier failing run would otherwise outlive
		// the test that fixed it.
		_ = os.Remove(m.failedPath(o.ext))
		if !failed {
			return o.rec.Discard()
		}
		return o.rec.Close()
	}
	if err := o.rec.Close(); err != nil {
		return err
	}
	if failed {
		// Best effort: the recording itself is already written.
		if data, err := os.ReadFile(m.mediaPath(o.ext)); err == nil && len(data) > 0 {
			_ = os.WriteFile(m.failedPath(o.ext), data, 0o600) //nolint:gosec // the name comes from the test name.
		}
	}
	return nil
}

// mediaPath is where the recording with extension ext is written: the final
// name for [ExportOn], or the failure name for [ExportOnFailure], which is
// only written when the test fails.
func (m *exportManager) mediaPath(ext string) string {
	if m.mode == ExportOnFailure {
		return m.failedPath(ext)
	}
	return filepath.Join(m.dir, m.stem()+"."+ext)
}

func (m *exportManager) failedPath(ext string) string {
	return filepath.Join(m.dir, m.stem()+"_failed."+ext)
}
