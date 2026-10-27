// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"os"
	"path/filepath"
)

// pendingFile is an output written to a temporary file beside its destination
// and renamed into place by commit, so a failed or discarded write leaves an
// existing file alone.
type pendingFile struct {
	path string
	// f is the temporary file. Writers that need it by name (such as ffmpeg)
	// close it first; commit and abort accept that.
	f *os.File
}

func createPending(path string) (*pendingFile, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, err
	}
	return &pendingFile{path: path, f: f}, nil
}

// commit moves the temporary file to the destination.
func (p *pendingFile) commit() error {
	if err := p.f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		_ = p.abort()
		return err
	}
	if err := os.Rename(p.f.Name(), p.path); err != nil {
		_ = os.Remove(p.f.Name())
		return err
	}
	return nil
}

// abort removes the temporary file.
func (p *pendingFile) abort() error {
	_ = p.f.Close()
	if err := os.Remove(p.f.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
