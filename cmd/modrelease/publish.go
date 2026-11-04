// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// publish signs the rehearsed tree, tags it, pushes them atomically, and brings
// the local branch up to date. Anything that fails before the push leaves nothing
// published, and the tags are removed again.
//
// The commit comes from commit-tree because it takes the exact tree the rehearsal
// tested, and runs no hooks that could change it.
func (r *release) publish(ctx context.Context, p *plan, tree, message string) error {
	ident, err := r.git(ctx, p.root, "var", "GIT_COMMITTER_IDENT")
	if err != nil {
		return fmt.Errorf("reading your git identity, nothing was published: %w", err)
	}
	name, _, _ := strings.Cut(ident, ">")
	commit, err := r.git(ctx, p.root, "commit-tree", "-S", tree, "-p", p.head,
		"-m", message, "-m", "Signed-off-by: "+name+">")
	if err != nil {
		return fmt.Errorf("creating the commit, nothing was published: %w", err)
	}
	r.linef("committed %s", commit[:9])

	var tags []string
	removeTags := func() {
		// Signing may have failed because ctx expired, and the tags still have to go.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), confirmTimeout)
		defer cancel()
		for _, tag := range tags {
			if _, deleteErr := r.git(cleanupCtx, p.root, "tag", "--delete", tag); deleteErr != nil {
				r.linef("warning: couldn't delete local tag %s: %v", tag, deleteErr)
			}
		}
	}
	for _, m := range p.modules {
		if _, err = r.git(ctx, p.root, "tag", "-s", "-m", m.tag, m.tag, commit); err != nil {
			removeTags()
			return fmt.Errorf("creating tag %s, nothing was published: %w", m.tag, err)
		}
		tags = append(tags, m.tag)
	}
	r.linef("tagged %d modules", len(tags))

	// --no-follow-tags stops push.followTags from also publishing older local tags.
	args := []string{"push", "--atomic", "--porcelain", "--no-follow-tags", r.flags.Remote, commit + ":refs/heads/" + p.branch}
	for _, tag := range tags {
		args = append(args, "refs/tags/"+tag)
	}
	output, err := r.git(ctx, p.root, args...)
	switch {
	case err == nil:
		r.linef("pushed")
	case pushWasRejected(output):
		removeTags()
		return fmt.Errorf("%s rejected the push, so nothing was published and the local tags are removed. Pull and run again: %w", r.flags.Remote, err)
	default:
		confirmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), confirmTimeout)
		defer cancel()
		published, confirmErr := r.confirmPublished(confirmCtx, p, tags, commit)
		if confirmErr != nil || !published {
			// The tags stay, since the push may still be running on the server,
			// and tell the next run what happened. See inspect.
			return fmt.Errorf("the outcome of the push is unknown, so commit %s and its tags are being kept. "+
				"Check %s, and delete the local tags once you know the push failed: %w", commit[:9], r.flags.Remote, err)
		}
		r.linef("pushed (the push didn't finish, but every tag is on %s)", r.flags.Remote)
	}

	if _, err = r.git(ctx, p.root, "merge", "--quiet", "--ff-only", commit); err != nil {
		r.linef("warning: couldn't fast-forward local %s, so pull to update it: %v", p.branch, err)
	} else {
		r.linef("fast-forwarded local %s", p.branch)
	}
	return nil
}

// pushWasRejected reports whether the remote answered and refused the push. Only
// that proves nothing was published. Any other failure, like a lost connection,
// says nothing about what the remote did.
func pushWasRejected(porcelain string) bool {
	for line := range strings.Lines(porcelain) {
		if strings.HasPrefix(line, "!\t") {
			return true
		}
	}
	return false
}

// confirmPublished reports whether every tag is on the remote, at the commit.
func (r *release) confirmPublished(ctx context.Context, p *plan, tags []string, commit string) (bool, error) {
	remote, err := r.remoteTags(ctx, p)
	if err != nil {
		return false, err
	}
	for _, tag := range tags {
		if remote[tag] != commit {
			return false, nil
		}
	}
	return true, nil
}

// goDownload is the output of go mod download -json.
type goDownload struct {
	Sum, GoModSum, Error string
}

// proxyAttempts is how many times to ask the public proxy for a version, since it
// fetches new versions on demand.
const proxyAttempts = 12

// verify compares what was published with what the rehearsal predicted. A wrong
// hash is an error, since it means the release's go.sum files are wrong. Not being
// able to fetch is only a warning, because a private repo can't be checked this
// way, and a release that's out is out. It reports whether every check finished.
func (r *release) verify(ctx context.Context, p *plan, tmp string) (complete bool, err error) {
	direct := r.compareHashes(ctx, p, tmp, "direct", 1, append(r.hermeticEnv(),
		"GOPROXY=direct", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB="+p.prefix))
	if !direct.fetched {
		r.linef("the proxy wasn't checked, because a repo that can't be fetched directly is probably private")
		return false, direct.err
	}
	if r.publicProxy == "" {
		return true, direct.err
	}
	proxy := r.compareHashes(ctx, p, tmp, "proxy", proxyAttempts, []string{
		"GOPROXY=" + r.publicProxy, "GOPRIVATE=", "GONOPROXY=none", "GONOSUMDB=",
	})
	if !proxy.fetched {
		r.linef("the direct check is what counts: the proxy can keep answering not found for a while after a release")
	}
	return proxy.fetched, errors.Join(direct.err, proxy.err)
}

// hashResult is the outcome of comparing one source of modules with the plan.
type hashResult struct {
	fetched bool // Every module could be fetched.
	err     error
}

// compareHashes downloads each module from one source into a fresh module cache.
// Modules that can't be fetched yet are retried together, so the wait doesn't grow
// with the number of modules.
func (r *release) compareHashes(ctx context.Context, p *plan, tmp, label string, attempts int, env []string) hashResult {
	dir := filepath.Join(tmp, "check-"+label)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return hashResult{err: err}
	}
	env = append(env, "GOWORK=off", "GOFLAGS=-modcacherw", "GOMODCACHE="+filepath.Join(dir, "modcache"))
	r.linef("%s:", label)

	result := hashResult{fetched: true}
	pending := p.modules
	var lastErr map[*goModule]string
	for attempt := 0; attempt < attempts && len(pending) > 0; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				attempt = attempts
				continue
			case <-time.After(r.proxyWait):
			}
		}
		lastErr = map[*goModule]string{}
		var failed []*goModule
		for _, m := range pending {
			got := r.download(ctx, dir, env, m.path+"@"+p.version)
			switch {
			case got.Sum == "":
				failed = append(failed, m)
				lastErr[m] = got.Error
			case got.Sum == m.sum && got.GoModSum == m.goModSum:
				r.linef("  match    %s", m.dir)
			default:
				r.linef("  MISMATCH %s: predicted %s, got %s", m.dir, m.sum, got.Sum)
				result.err = errors.Join(result.err, fmt.Errorf("%s from %s has hash %s, not the predicted %s", m.tag, label, got.Sum, m.sum))
			}
		}
		pending = failed
	}
	for _, m := range pending {
		result.fetched = false
		r.linef("  warning: couldn't fetch %s: %s", m.dir, tailLines(lastErr[m], 1))
	}
	return result
}

// download fetches one module version. A failed download reports its error in the
// JSON on stdout.
func (r *release) download(ctx context.Context, dir string, env []string, modVersion string) goDownload {
	var got goDownload
	out, err := r.cmdRun(ctx, dir, env, "go", "mod", "download", "-json", modVersion)
	if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil && err != nil {
		got.Error = err.Error()
	}
	return got
}
