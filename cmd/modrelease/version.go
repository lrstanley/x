// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// Bump keywords, which are the alternatives to an explicit version.
const (
	bumpMajor = "major"
	bumpMinor = "minor"
	bumpPatch = "patch"
)

var bumpKeywords = []string{bumpMajor, bumpMinor, bumpPatch}

// version is a canonical semantic version, split into its parts.
type version struct {
	major, minor, patch int
	pre                 string
}

// parseVersion parses a canonical version, such as v1.2.3 or v1.2.3-beta.1.
// Build metadata isn't canonical, and neither are shortened forms like v1.2.
func parseVersion(s string) (version, error) {
	if semver.Canonical(s) != s {
		return version{}, fmt.Errorf("%q is not a canonical version like v1.2.3-beta.1", s)
	}
	core, pre, _ := strings.Cut(strings.TrimPrefix(s, "v"), "-")
	v := version{pre: pre}
	if _, err := fmt.Sscanf(core, "%d.%d.%d", &v.major, &v.minor, &v.patch); err != nil {
		return version{}, fmt.Errorf("parsing %q: %w", s, err)
	}
	return v, nil
}

// core returns the version without its prerelease.
func (v version) core() string {
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
}

// bump applies a bump keyword. A prerelease that already has the shape the bump
// would produce stays on its version, so v1.3.0-beta.1 bumped by minor gives
// v1.3.0, rather than moving past the version in progress.
func (v version) bump(kind string) version {
	wasPre := v.pre != ""
	v.pre = ""
	switch kind {
	case bumpMajor:
		if !wasPre || v.minor != 0 || v.patch != 0 {
			v.major, v.minor, v.patch = v.major+1, 0, 0
		}
	case bumpMinor:
		if !wasPre || v.patch != 0 {
			v.minor, v.patch = v.minor+1, 0
		}
	case bumpPatch:
		if !wasPre {
			v.patch++
		}
	}
	return v
}

// currentVersion returns the highest canonical root version in tags, or "" if
// there isn't one. Tags for sub-modules, like reel/v1.2.3, are ignored.
func currentVersion(tags []string) string {
	var current string
	for _, tag := range tags {
		if semver.Canonical(tag) == tag && semver.Compare(tag, current) > 0 {
			current = tag
		}
	}
	return current
}

// nextVersion works out the version to release. bump is a bump keyword, or an
// explicit version already checked by Flags.Validate. channel is alpha, beta, rc,
// or "", and its number continues from the highest matching tag.
func nextVersion(tags []string, bump, channel string) (string, error) {
	current := currentVersion(tags)

	next := bump
	if slices.Contains(bumpKeywords, bump) {
		if current == "" {
			return "", errors.New("no version tags on the remote yet, so the first release needs an explicit version")
		}
		v, err := parseVersion(current)
		if err != nil {
			return "", err
		}
		next = v.bump(bump).core()
		if channel != "" {
			next = withChannel(tags, next, channel)
		}
	}

	if current != "" && semver.Compare(next, current) <= 0 {
		return "", fmt.Errorf("%s is not newer than the current version %s", next, current)
	}
	return next, nil
}

// withChannel returns the next prerelease of core on channel, such as
// v1.3.0-beta.2, numbered from 0 like the existing tags.
func withChannel(tags []string, core, channel string) string {
	prefix := core + "-" + channel + "."
	n := 0
	for _, tag := range tags {
		rest, ok := strings.CutPrefix(tag, prefix)
		if !ok {
			continue
		}
		if i, err := strconv.Atoi(rest); err == nil && i >= n {
			n = i + 1
		}
	}
	return prefix + strconv.Itoa(n)
}
