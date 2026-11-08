// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// check is one result of inspecting the repository.
type check struct {
	ok   bool
	text string
}

var symrefRE = regexp.MustCompile(`(?m)^ref: refs/heads/(\S+)\s+HEAD$`)

// inspect works out the plan, and checks whether it's safe to do. All of the
// problems are reported together, so they can be fixed in one go. The plan is
// only usable when every check passed.
func (r *release) inspect(ctx context.Context, root string) (*plan, []check) {
	p := &plan{root: root}
	var checks []check
	pass := func(format string, args ...any) { checks = append(checks, check{true, fmt.Sprintf(format, args...)}) }
	fail := func(format string, args ...any) { checks = append(checks, check{false, fmt.Sprintf(format, args...)}) }

	if status, err := r.git(ctx, root, "status", "--porcelain"); err != nil {
		fail("couldn't read the worktree status: %v", err)
	} else if status != "" {
		lines := strings.Split(status, "\n")
		fail("worktree is dirty (%d files), first: %s", len(lines), strings.TrimSpace(lines[0]))
	} else {
		pass("worktree is clean")
	}

	var err error
	var shallow string
	if shallow, err = r.git(ctx, root, "rev-parse", "--is-shallow-repository"); err != nil {
		fail("couldn't tell if this is a shallow clone: %v", err)
	} else if shallow == "true" {
		fail("this is a shallow clone, so its history is incomplete")
	} else {
		pass("full clone")
	}

	if p.head, err = r.git(ctx, root, "rev-parse", "HEAD"); err != nil {
		fail("repository has no commits")
		return p, checks
	}
	if p.branch, err = r.git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
		fail("HEAD is detached")
	}

	// Atomic only holds per destination, so with two, one could accept the release
	// while the other rejects it. Everything reads from where the push will go.
	urls, err := r.git(ctx, root, "remote", "get-url", "--push", "--all", r.flags.Remote)
	if err != nil {
		fail("remote %q isn't configured", r.flags.Remote)
		return p, checks
	}
	if strings.Contains(urls, "\n") {
		fail("remote %s has several push URLs, and a release has to go to exactly one", r.flags.Remote)
		return p, checks
	}
	p.pushURL = urls
	p.github = cmp.Or(r.github, githubRepo(p.pushURL))

	symref, err := r.git(ctx, root, "ls-remote", "--symref", p.pushURL, "HEAD")
	if err != nil {
		fail("remote %q isn't reachable: %v", r.flags.Remote, err)
		return p, checks
	}
	if p.branch != "" {
		r.checkBranch(ctx, p, symref, pass, fail)
	}

	if err = r.checkSigning(ctx, p); err != nil {
		fail("signing failed: %v", err)
	} else {
		pass("signing works")
	}

	// git grep exits 1 for no matches, which only means LFS isn't used.
	var exitErr *exec.ExitError
	_, err = r.git(ctx, root, "grep", "--quiet", "filter=lfs", "HEAD", "--", ":(glob)**/.gitattributes")
	switch {
	case err == nil:
		if _, err = r.git(ctx, root, "lfs", "version"); err != nil {
			fail("this repository uses LFS, and git-lfs isn't installed")
		} else {
			pass("git-lfs is available")
		}
	case !errors.As(err, &exitErr) || exitErr.ExitCode() != 1:
		fail("couldn't tell if this repository uses LFS: %v", err)
	}

	remote, err := r.remoteTags(ctx, p)
	if err != nil {
		fail("couldn't list tags on %s: %v", r.flags.Remote, err)
		return p, checks
	}
	names := slices.Sorted(maps.Keys(remote))
	p.current = currentVersion(names)
	if p.version, err = nextVersion(names, r.flags.Bump, r.flags.channel()); err != nil {
		fail("%v", err)
		return p, checks
	}

	var problems []string
	p.modules, p.skipped, p.prefix, problems = r.discover(ctx, root, p.version)
	for _, problem := range problems {
		fail("%s", problem)
	}

	local, err := r.git(ctx, root, "tag", "--list")
	if err != nil {
		fail("couldn't list local tags: %v", err)
		return p, checks
	}
	localTags := strings.Split(local, "\n")
	unused := true
	for _, m := range p.modules {
		_, onRemote := remote[m.tag]
		switch {
		case onRemote:
			unused = false
			fail("tag %s already exists on %s", m.tag, r.flags.Remote)
		case slices.Contains(localTags, m.tag):
			unused = false
			fail("tag %s exists locally but not on %s. An earlier release may have been interrupted: "+
				"check the remote, and delete the tag once you know that push failed", m.tag, r.flags.Remote)
		}
	}
	if unused && len(p.modules) > 0 {
		pass("none of the %d tags exist locally or on %s", len(p.modules), r.flags.Remote)
	}
	r.checkWorkflows(ctx, p, pass, fail)
	return p, checks
}

// checkBranch checks that HEAD is the remote's default branch, and matches it.
func (r *release) checkBranch(ctx context.Context, p *plan, symref string, pass, fail func(string, ...any)) {
	var def string
	if m := symrefRE.FindStringSubmatch(symref); m != nil {
		def = m[1]
	}
	if p.branch == def {
		pass("on default branch %s", def)
	} else {
		fail("on branch %s, not default branch %s", p.branch, cmp.Or(def, "(unknown)"))
	}

	out, err := r.git(ctx, p.root, "ls-remote", p.pushURL, "refs/heads/"+p.branch)
	if err != nil {
		fail("couldn't read %s/%s: %v", r.flags.Remote, p.branch, err)
		return
	}
	fields := strings.Fields(out)
	switch {
	case len(fields) == 0:
		fail("branch %s doesn't exist on %s", p.branch, r.flags.Remote)
	case fields[0] == p.head:
		pass("in sync with %s/%s at %s", r.flags.Remote, p.branch, p.head[:9])
	default:
		if _, err = r.git(ctx, p.root, "merge-base", "--is-ancestor", fields[0], p.head); err == nil {
			fail("local %s is ahead of %s, so push or reset it first", p.branch, r.flags.Remote)
		} else {
			fail("local %s is behind or has diverged from %s, so pull first", p.branch, r.flags.Remote)
		}
	}
}

// checkSigning makes a signature on a commit nothing refers to, so a locked or
// missing key shows up before the rehearsal instead of at the end of it.
func (r *release) checkSigning(ctx context.Context, p *plan) error {
	_, err := r.git(ctx, p.root, "commit-tree", "-S", p.head+"^{tree}", "-p", p.head, "-m", "modrelease signing check")
	return err
}

// remoteTags returns the remote's tags, mapped to the commits they point at.
func (r *release) remoteTags(ctx context.Context, p *plan) (map[string]string, error) {
	out, err := r.git(ctx, p.root, "ls-remote", "--tags", p.pushURL)
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for line := range strings.Lines(out) {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/tags/")
		if peeled, isPeeled := strings.CutSuffix(name, "^{}"); isPeeled {
			tags[peeled] = sha // The peeled line is the commit of an annotated tag.
		} else if _, seen := tags[name]; !seen {
			tags[name] = sha
		}
	}
	return tags, nil
}
