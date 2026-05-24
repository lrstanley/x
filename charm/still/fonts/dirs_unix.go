// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix && !darwin && !js && !wasm

package fonts

import (
	"os"
	"path/filepath"
	"runtime"
)

func fontDirectories() []string {
	if runtime.GOOS == "android" {
		return []string{"/system/fonts"}
	}
	out := getUserFontDirs()
	out = append(out, getSystemFontDirs()...)
	return out
}

func getUserFontDirs() []string {
	if dataPath := os.Getenv("XDG_DATA_HOME"); dataPath != "" {
		p := expandUser(dataPath)
		return []string{expandUser("~/.fonts/"), filepath.Join(p, "fonts")}
	}
	return []string{expandUser("~/.fonts/"), expandUser("~/.local/share/fonts/")}
}

func getSystemFontDirs() []string {
	if dataPaths := os.Getenv("XDG_DATA_DIRS"); dataPaths != "" {
		var paths []string
		for _, dataPath := range filepath.SplitList(dataPaths) {
			paths = append(paths, filepath.Join(expandUser(dataPath), "fonts"))
		}
		return paths
	}
	return []string{"/usr/local/share/fonts/", "/usr/share/fonts/"}
}
