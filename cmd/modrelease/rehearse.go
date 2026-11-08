// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/zip"
)

// scratchIdentity names the author of scratch commits. They exist so a module's
// zip can be built, are never published, and nothing refers to them.
var scratchIdentity = []string{
	"GIT_AUTHOR_NAME=modrelease", "GIT_AUTHOR_EMAIL=modrelease@invalid",
	"GIT_COMMITTER_NAME=modrelease", "GIT_COMMITTER_EMAIL=modrelease@invalid",
}

// rehearse prepares the release in a temporary worktree, in dependency order.
// Each module is tidied, built, and tested against the modules before it, which
// are served from a local GOPROXY as if they were already published. It returns
// the tree to release, and a cleanup function that's always safe to call.
//
// The worktree uses the normal git config so tests get real LFS files. Zips come
// from a clone with no user config, because they have to match what the public
// proxy builds, which has LFS pointers.
func (r *release) rehearse(ctx context.Context, p *plan, tmp string) (tree string, cleanup func(), err error) {
	wt := filepath.Join(tmp, "worktree")
	zipClone := filepath.Join(tmp, "zipclone")
	proxy := filepath.Join(tmp, "proxy")

	if _, err = r.git(ctx, p.root, "worktree", "add", "--quiet", "--detach", wt, p.head); err != nil {
		return "", func() {}, err
	}
	cleanup = func() {
		_, _ = r.git(context.WithoutCancel(ctx), p.root, "worktree", "remove", "--force", wt)
	}
	// A linked worktree's .git is a file, which zip.CreateFromVCS doesn't accept.
	if _, err = r.cmdRun(ctx, tmp, r.hermeticEnv(), "git", "clone", "--quiet", "--shared", "--no-checkout", p.root, zipClone); err != nil {
		return "", cleanup, err
	}
	var env []string
	if env, err = r.rehearsalEnv(ctx, p, tmp, proxy); err != nil {
		return "", cleanup, err
	}

	for _, m := range p.modules {
		r.linef("%s", styleBold.Render(m.dir))
		start := time.Now()
		if err = r.updateModule(ctx, p, m, filepath.Join(wt, filepath.FromSlash(m.dir)), env); err != nil {
			return "", cleanup, fmt.Errorf("%s: %w", m.dir, err)
		}
		r.linef("  tidy, build, test: %s %s", styleOK.Render("ok"), styleFaint.Render(fmt.Sprintf("(%.1fs)", time.Since(start).Seconds())))
		if tree, err = r.git(ctx, wt, "write-tree"); err != nil {
			return "", cleanup, err
		}
		if err = r.predictModule(ctx, p, m, wt, tree, zipClone, proxy); err != nil {
			return "", cleanup, fmt.Errorf("%s: predicting its hash: %w", m.dir, err)
		}
		r.linef("  %s", styleFaint.Render(m.sum))
	}
	return tree, cleanup, nil
}

// rehearsalEnv returns the go environment for the rehearsal. It serves the local
// proxy first, so the public proxy is never asked about unpublished versions, and
// keeps the user's proxy and privacy settings for everything but these modules.
func (r *release) rehearsalEnv(ctx context.Context, p *plan, tmp, proxy string) ([]string, error) {
	out, err := r.cmdRun(ctx, p.root, []string{"GOWORK=off"}, "go", "env", "-json", "GOPROXY", "GONOPROXY", "GONOSUMDB")
	if err != nil {
		return nil, err
	}
	var goenv map[string]string
	if err = json.Unmarshal([]byte(out), &goenv); err != nil {
		return nil, fmt.Errorf("parsing go env: %w", err)
	}
	var paths []string
	for _, m := range p.modules {
		paths = append(paths, m.path)
	}
	return []string{
		"GOWORK=off",
		"GOFLAGS=-modcacherw",
		"GOMODCACHE=" + filepath.Join(tmp, "modcache"),
		"GOPROXY=" + fileURL(proxy) + "," + goenv["GOPROXY"],
		"GOPRIVATE=",
		"GONOPROXY=" + cmp.Or(withoutPatterns(goenv["GONOPROXY"], paths), "none"),
		"GONOSUMDB=" + strings.Trim(withoutPatterns(goenv["GONOSUMDB"], paths)+","+p.prefix, ","),
	}, nil
}

// fileURL returns the file:// URL of an absolute path, which may hold characters
// like # that a URL has to escape.
func fileURL(dir string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path // A Windows path starts with its drive.
	}
	return u.String()
}

// withoutPatterns returns a comma-separated list of module path patterns, minus
// any that match one of paths.
func withoutPatterns(patterns string, paths []string) string {
	var kept []string
	for pattern := range strings.SplitSeq(patterns, ",") {
		if pattern == "" {
			continue
		}
		if !slices.ContainsFunc(paths, func(p string) bool { return module.MatchPrefixPatterns(pattern, p) }) {
			kept = append(kept, pattern)
		}
	}
	return strings.Join(kept, ",")
}

// updateModule moves a module to the new version of its siblings, then tidies,
// builds, and tests it, and stages the result. It prints what changed.
func (r *release) updateModule(ctx context.Context, p *plan, m *goModule, dir string, env []string) error {
	before, err := readRequires(dir)
	if err != nil {
		return err
	}
	sumBefore, err := readSum(dir)
	if err != nil {
		return err
	}

	if len(m.siblings) > 0 {
		args := []string{"mod", "edit"}
		for _, sibling := range m.siblings {
			args = append(args, "-require="+sibling+"@"+p.version)
		}
		if _, err = r.cmdRun(ctx, dir, env, "go", args...); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"mod", "tidy"},
		{"build", "-o", os.DevNull, "./..."}, // One main package would write a binary.
		{"test", "-count=1", "./..."},
	} {
		if _, err = r.cmdRun(ctx, dir, env, "go", args...); err != nil {
			return err
		}
	}

	after, err := readRequires(dir)
	if err != nil {
		return err
	}
	sumAfter, err := readSum(dir)
	if err != nil {
		return err
	}
	for _, line := range requireChanges(before, after) {
		r.linef("  %s", line)
	}
	if !slices.Equal(sumBefore, sumAfter) {
		added, removed := countDiff(sumBefore, sumAfter), countDiff(sumAfter, sumBefore)
		r.linef("  go.sum: %s, %s", styleOK.Render(fmt.Sprintf("%d lines added", added)), styleRemoved.Render(fmt.Sprintf("%d removed", removed)))
	} else if maps.Equal(before, after) {
		r.linef("  %s", styleFaint.Render("go.mod and go.sum: no changes"))
	}

	// Stage by name, so nothing else a build or test wrote can be released.
	args := []string{"add", "--", "go.mod"}
	if sumBefore != nil || sumAfter != nil {
		args = append(args, "go.sum")
	}
	if _, err = r.git(ctx, dir, args...); err != nil {
		return err
	}
	return r.checkOnlyManifestsChanged(ctx, p, dir)
}

// checkOnlyManifestsChanged fails if the release differs from HEAD by anything but
// the go.mod and go.sum of a released module, whether a build or test left it
// unstaged or staged it, so what's published is only ever what was tested.
func (r *release) checkOnlyManifestsChanged(ctx context.Context, p *plan, dir string) error {
	unstaged, err := r.git(ctx, dir, "diff", "--name-only")
	if err != nil {
		return err
	}
	if unstaged != "" {
		return fmt.Errorf("the rehearsal changed a tracked file: %s", unstaged)
	}
	staged, err := r.git(ctx, dir, "diff", "--cached", "--name-only", p.head)
	if err != nil {
		return err
	}
	var allowed []string
	for _, m := range p.modules {
		allowed = append(allowed, path.Join(m.dir, "go.mod"), path.Join(m.dir, "go.sum"))
	}
	for name := range strings.Lines(staged) {
		if name = strings.TrimSpace(name); !slices.Contains(allowed, name) {
			return fmt.Errorf("the rehearsal changed a tracked file: %s", name)
		}
	}
	return nil
}

// predictModule builds the module's zip from a scratch commit of tree, records
// its hashes, and adds it to the local proxy for the modules after it.
func (r *release) predictModule(ctx context.Context, p *plan, m *goModule, wt, tree, zipClone, proxy string) error {
	scratch, err := r.cmdRun(ctx, wt, scratchIdentity, "git", "commit-tree", tree, "-p", p.head, "-m", "modrelease scratch")
	if err != nil {
		return err
	}

	escPath, err := module.EscapePath(m.path)
	if err != nil {
		return err
	}
	escVersion, err := module.EscapeVersion(p.version)
	if err != nil {
		return err
	}
	dir := filepath.Join(proxy, filepath.FromSlash(escPath), "@v")
	err = os.MkdirAll(dir, 0o750)
	if err != nil {
		return err
	}

	zipPath := filepath.Join(dir, escVersion+".zip")
	err = r.writeZip(zipPath, zipClone, scratch, m, p.version)
	if err != nil {
		return err
	}
	if m.sum, err = dirhash.HashZip(zipPath, dirhash.Hash1); err != nil {
		return err
	}

	goMod, err := os.ReadFile(filepath.Join(wt, filepath.FromSlash(m.dir), "go.mod"))
	if err != nil {
		return err
	}
	m.goModSum, err = dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(goMod)), nil
	})
	if err != nil {
		return err
	}

	info, err := json.Marshal(struct {
		Version string
		Time    time.Time
	}{p.version, time.Now().UTC()})
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{
		escVersion + ".mod":  goMod,
		escVersion + ".info": info,
		"list":               []byte(p.version + "\n"),
	} {
		err = os.WriteFile(filepath.Join(dir, name), data, 0o600)
		if err != nil {
			return err
		}
	}
	return nil
}

// writeZip builds a module's zip with no user git config. zip.CreateFromVCS runs
// git with the process's environment and has no way to change it, so this sets
// it around the call. Nothing else runs while it does, since the rehearsal is
// sequential.
func (r *release) writeZip(zipPath, repo, rev string, m *goModule, version string) error {
	restore := r.setHermeticEnv()
	defer restore()

	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sub := m.dir
	if sub == "." {
		sub = ""
	}
	return zip.CreateFromVCS(f, module.Version{Path: m.path, Version: version}, repo, rev, sub)
}

// hermeticEnv is the environment that makes git ignore the user's config.
func (r *release) hermeticEnv() []string {
	return []string{"GIT_CONFIG_GLOBAL=" + r.hermeticGitConfig, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0"}
}

// setHermeticEnv applies hermeticEnv to the process, and returns how to undo it.
func (r *release) setHermeticEnv() (restore func()) {
	var undo []func()
	for _, kv := range r.hermeticEnv() {
		key, value, _ := strings.Cut(kv, "=")
		if old, ok := os.LookupEnv(key); ok {
			undo = append(undo, func() { _ = os.Setenv(key, old) })
		} else {
			undo = append(undo, func() { _ = os.Unsetenv(key) })
		}
		_ = os.Setenv(key, value)
	}
	return func() {
		for _, f := range undo {
			f()
		}
	}
}

// readRequires returns the requirements in the go.mod in dir.
func readRequires(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	file, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		return nil, err
	}
	requires := make(map[string]string, len(file.Require))
	for _, req := range file.Require {
		requires[req.Mod.Path] = req.Mod.Version
	}
	return requires, nil
}

// readSum returns the lines of the go.sum in dir, or nil if there isn't one.
func readSum(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n"), nil
}

// requireChanges describes how requirements changed, sorted by module path.
func requireChanges(before, after map[string]string) []string {
	paths := slices.Sorted(maps.Keys(after))
	for p := range before {
		if _, ok := after[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)

	var changes []string
	for _, p := range paths {
		if before[p] != after[p] {
			changes = append(changes, fmt.Sprintf("%s  %s -> %s", p, cmp.Or(before[p], "(new)"), cmp.Or(after[p], "(removed)")))
		}
	}
	return changes
}

// countDiff returns how many lines of b aren't in a.
func countDiff(a, b []string) int {
	count := 0
	for _, line := range b {
		if !slices.Contains(a, line) {
			count++
		}
	}
	return count
}
