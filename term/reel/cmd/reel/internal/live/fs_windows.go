// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package live

import (
	"errors"
	"fmt"
	"os"
)

// checkDir reports an error unless dir is a directory, not a symlink. Its
// access is left to the ACLs the user's profile directories have.
func checkDir(dir string) error {
	info, err := os.Lstat(dir) //nolint:gosec // G703: the directory is the user's own choice, or their runtime directory.
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("session directory %s is not a directory", dir)
	}
	return nil
}

// errLocked is the error for a lock another process holds.
var errLocked = errors.New("locked")

// lock takes the lock file at path, which closing the returned file
// releases. Windows denies a second exclusive open of the same file.
func lock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if !errors.Is(err, os.ErrExist) {
		return f, err
	}
	// A lock file a running session holds open cannot be removed; one a
	// session that is gone left can.
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errLocked
	}
	f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, errLocked // Another session took it meanwhile.
	}
	return f, err
}
