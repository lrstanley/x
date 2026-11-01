// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package live

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// checkDir reports an error unless dir is a directory, not a symlink, that
// the user owns and only the user can open: as the sockets in it take no
// credentials, it is what keeps other users out of the sessions.
func checkDir(dir string) error {
	info, err := os.Lstat(dir) //nolint:gosec // G703: the directory is the user's own choice, or their runtime directory.
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.IsDir():
		return fmt.Errorf("session directory %s is not a directory", dir)
	case !ok || int(st.Uid) != os.Geteuid():
		return fmt.Errorf("session directory %s is not owned by you", dir)
	case info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("session directory %s is open to other users (mode %v); it must be 0700", dir, info.Mode().Perm())
	}
	return nil
}

// errLocked is the error for a lock another process holds.
var errLocked = errors.New("locked")

// lock takes the lock file at path, which closing the returned file
// releases, so that a lock a process held ends with it.
func lock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // A file descriptor fits in an int.
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return f, nil
}
