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

// pendingFile is an output written to a temporary file and, by commit,
// renamed into place beside it, or copied to a writer. A failed or discarded
// write leaves an existing destination alone.
type pendingFile struct {
	// path is the destination. It is empty when out is set.
	path string
	// out, when set, receives the file's contents on commit, and the file is
	// removed.
	out io.Writer
	// f is the temporary file. Writers that need it by name (such as ffmpeg)
	// close it first; commit and abort accept that.
	f *os.File
}

// createPending returns a pending file beside path, which commit renames to
// path.
func createPending(path string) (*pendingFile, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, err
	}
	return &pendingFile{path: path, f: f}, nil
}

// createPendingWriter returns a pending file in the system temporary
// directory, which commit copies to w.
func createPendingWriter(w io.Writer) (*pendingFile, error) {
	f, err := os.CreateTemp("", "sink-*.tmp")
	if err != nil {
		return nil, err
	}
	return &pendingFile{out: w, f: f}, nil
}

// commit moves the temporary file to the destination, or copies it to the
// writer and removes it.
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

// abort removes the temporary file.
func (p *pendingFile) abort() error {
	_ = p.f.Close()
	if err := os.Remove(p.f.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
