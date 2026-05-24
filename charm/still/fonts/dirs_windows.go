// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package fonts

import (
	"os"
	"path/filepath"
)

func fontDirectories() []string {
	return []string{
		filepath.Join(os.Getenv("localappdata"), "Microsoft", "Windows", "Fonts"),
		filepath.Join(os.Getenv("windir"), "Fonts"),
	}
}
