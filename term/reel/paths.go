// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"fmt"
	"maps"
	"strings"
	"time"
)

// Path variables reel sets.
const (
	varDuration  = "duration"
	varTimestamp = "timestamp"
	varMark      = "mark"
)

// pathVars returns extra plus {timestamp} for start (or now, if zero).
// {duration} is left for the caller, since only a finished timeline has it.
func pathVars(extra map[string]string, start time.Time) (map[string]string, error) {
	for _, name := range []string{varDuration, varTimestamp, varMark} {
		if _, ok := extra[name]; ok {
			return nil, fmt.Errorf("reel: Vars cannot set the built-in path variable {%s}", name)
		}
	}
	if start.IsZero() {
		start = time.Now()
	}
	vars := maps.Clone(extra)
	if vars == nil {
		vars = make(map[string]string, 2)
	}
	vars[varTimestamp] = start.UTC().Format("20060102T150405")
	return vars, nil
}

// PathVars returns extra plus the path variables [Export] sets for t's
// window: {duration}, its length rounded to 100ms, such as "4.2s", and
// {timestamp}, its start in UTC as 20060102T150405, or now if t has no
// start time. It is an error for extra to set those or {mark}.
func (t *Timeline) PathVars(extra map[string]string) (map[string]string, error) {
	from, to := t.Window()
	start := t.started()
	if !start.IsZero() {
		start = start.Add(from)
	}
	vars, err := pathVars(extra, start)
	if err != nil {
		return nil, err
	}
	vars[varDuration] = (to - from).Round(100 * time.Millisecond).String()
	return vars, nil
}

// ExpandPath replaces each {name} in path with vars[name], as [Export] does
// for output paths; unknown names are an error.
func ExpandPath(path string, vars map[string]string) (string, error) {
	var b strings.Builder
	for rest := path; rest != ""; {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			break
		}
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			return "", fmt.Errorf("reel: path %q: unclosed {", path)
		}
		name := rest[open+1 : open+end]
		value, ok := vars[name]
		if !ok {
			switch name {
			case varDuration:
				return "", fmt.Errorf("reel: path %q: {%s} is only known once a recording ends; use Export", path, name)
			case varMark:
				return "", fmt.Errorf("reel: path %q: {%s} is only set in an output with Marks", path, name)
			}
			return "", fmt.Errorf("reel: path %q: unknown variable {%s}", path, name)
		}
		b.WriteString(rest[:open])
		b.WriteString(value)
		rest = rest[open+end+1:]
	}
	return b.String(), nil
}
