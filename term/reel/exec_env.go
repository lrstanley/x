// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// execEnv returns env with TERM and COLORTERM set for the session's
// terminal: replaced when env is inherited from the host, and added when
// env leaves them out.
func execEnv(env []string, inherited bool) []string {
	out := make([]string, 0, len(env)+2)
	var hasTerm, hasColorTerm bool
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		isTerm, isColorTerm := envKey(key, "TERM"), envKey(key, "COLORTERM")
		if inherited && (isTerm || isColorTerm) {
			continue
		}
		hasTerm, hasColorTerm = hasTerm || isTerm, hasColorTerm || isColorTerm
		out = append(out, kv)
	}
	if !hasTerm {
		term := "xterm-256color"
		if hasTerminfo(append(terminfoPath(env), systemTerminfo...), castTermType) {
			term = castTermType
		}
		out = append(out, "TERM="+term)
	}
	if !hasColorTerm {
		out = append(out, "COLORTERM=truecolor")
	}
	return out
}

// envKey reports whether the environment variable key is name, ignoring
// case on Windows, as its environment does.
func envKey(key, name string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(key, name)
	}
	return key == name
}

// envValue returns the value of the last name in env, which is the one a
// process sees.
func envValue(env []string, name string) string {
	var value string
	for _, kv := range env {
		if key, v, ok := strings.Cut(kv, "="); ok && envKey(key, name) {
			value = v
		}
	}
	return value
}

// systemTerminfo are the directories ncurses looks for terminfo entries in
// after those the environment names.
var systemTerminfo = []string{"/etc/terminfo", "/lib/terminfo", "/usr/share/terminfo", "/usr/lib/terminfo"}

// terminfoPath returns the directories the environment env tells a program
// to look for terminfo entries in, in order, before systemTerminfo.
func terminfoPath(env []string) []string {
	var dirs []string
	if dir := envValue(env, "TERMINFO"); dir != "" {
		dirs = append(dirs, dir)
	}
	if home := envValue(env, "HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".terminfo"))
	}
	for dir := range strings.SplitSeq(envValue(env, "TERMINFO_DIRS"), string(os.PathListSeparator)) {
		// An empty entry stands for the system directories, which come
		// last anyway.
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// hasTerminfo reports whether one of dirs has a terminfo entry for name,
// filed under its first letter or that letter's hex code.
func hasTerminfo(dirs []string, name string) bool {
	for _, dir := range dirs {
		for _, sub := range []string{name[:1], fmt.Sprintf("%02x", name[0])} {
			if fi, err := os.Stat(filepath.Join(dir, sub, name)); err == nil && fi.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}
