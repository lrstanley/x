// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// pendingFile is an output written to a temporary file, which commit renames
// to path or copies to out. abort leaves an existing destination alone.
type pendingFile struct {
	// path is empty when out is set.
	path string
	out  io.Writer
	// f may already be closed by a writer that needs it by name (ffmpeg).
	f *os.File
}

// createPending returns a pending file beside path.
func createPending(path string) (*pendingFile, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, err
	}
	return &pendingFile{path: path, f: f}, nil
}

// createPendingWriter returns a pending file in the system temporary
// directory.
func createPendingWriter(w io.Writer) (*pendingFile, error) {
	f, err := os.CreateTemp("", "sink-*.tmp")
	if err != nil {
		return nil, err
	}
	return &pendingFile{out: w, f: f}, nil
}

func (p *pendingFile) commit() error {
	if err := p.f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		_ = p.abort()
		return err
	}
	if p.out != nil {
		err := p.copyOut()
		return errors.Join(err, p.abort())
	}
	if err := os.Rename(p.f.Name(), p.path); err != nil {
		_ = os.Remove(p.f.Name())
		return err
	}
	return nil
}

func (p *pendingFile) copyOut() error {
	f, err := os.Open(p.f.Name())
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(p.out, f)
	return err
}

func (p *pendingFile) abort() error {
	_ = p.f.Close()
	if err := os.Remove(p.f.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
