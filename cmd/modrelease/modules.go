// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// goModule is one module being released.
type goModule struct {
	dir, path, tag string
	siblings       []string // Paths of other released modules that it requires.

	// Set by the rehearsal, which predicts what the go command will compute
	// from the tag.
	sum, goModSum string
}

// skippedModule is a module that isn't being released, and why.
type skippedModule struct {
	dir, reason string
}

// isHidden reports whether a module directory is one the go tool ignores, which
// makes it support code like examples rather than something to release.
func isHidden(dir string) bool {
	if dir == "." {
		return false
	}
	return slices.ContainsFunc(strings.Split(dir, "/"), func(elem string) bool {
		return elem == "testdata" || strings.HasPrefix(elem, "_") || strings.HasPrefix(elem, ".")
	})
}

// tagFor returns the tag for a module directory at a version.
func tagFor(dir, version string) string {
	if dir == "." {
		return version
	}
	return dir + "/" + version
}

// discover finds the modules to release and orders them so that each one comes
// after the siblings it requires. It also returns the repository's module path,
// and everything about the modules that stops a release.
func (r *release) discover(ctx context.Context, root, version string) (mods []*goModule, skipped []skippedModule, prefix string, problems []string) {
	rootMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, nil, "", []string{"there's no go.mod at the repository root, and the version comes from its tags"}
	}
	prefix, _, _ = module.SplitPathVersion(modfile.ModulePath(rootMod))

	out, err := r.git(ctx, root, "ls-files", "--", ":(glob)**/go.mod")
	if err != nil {
		return nil, nil, "", []string{fmt.Sprintf("couldn't list modules: %v", err)}
	}

	files := map[string]*modfile.File{}
	skips := slices.Clone(r.flags.Skip)
	skippedPaths := map[string]string{}
	byPath := map[string]*goModule{}
	var found []*goModule
	for gomod := range strings.Lines(out) {
		name := strings.TrimSpace(gomod)
		dir := path.Dir(name)
		skip := slices.Contains(skips, dir)
		if skip {
			skips = slices.DeleteFunc(skips, func(s string) bool { return s == dir })
		}
		// A hidden go.mod isn't parsed, since one in testdata may be invalid on purpose.
		if !skip && isHidden(dir) {
			skipped = append(skipped, skippedModule{dir, "hidden directory"})
			continue
		}

		var file *modfile.File
		if file, err = parseGoMod(root, name); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		modPath := file.Module.Mod.Path

		if skip {
			if dir == "." {
				problems = append(problems, "--skip .: the root module can't be skipped, because the version comes from its tags")
				continue
			}
			skipped = append(skipped, skippedModule{dir, "--skip"})
			skippedPaths[modPath] = dir
			continue
		}

		_, major, _ := module.SplitPathVersion(modPath)
		if err = module.CheckPathMajor(version, major); err != nil {
			problems = append(problems, fmt.Sprintf("%s: module path %s can't be tagged %s, which needs a path ending in /%s. "+
				"Changing a module path also means changing its imports, so do that first, or use --skip",
				dir, modPath, version, semver.Major(version)))
			continue
		}
		want := path.Join(prefix, dir) + major
		if modPath != want {
			problems = append(problems, fmt.Sprintf("%s: module path %s can't be fetched from this directory, "+
				"it should be %s. Fix the path, or use --skip", dir, modPath, want))
			continue
		}

		m := &goModule{dir: dir, path: modPath, tag: tagFor(dir, version)}
		found = append(found, m)
		byPath[modPath] = m
		files[modPath] = file
	}
	for _, dir := range skips {
		problems = append(problems, fmt.Sprintf("--skip %s: no module in that directory", dir))
	}

	for _, m := range found {
		for _, req := range files[m.path].Require {
			if byPath[req.Mod.Path] != nil {
				m.siblings = append(m.siblings, req.Mod.Path)
			}
			if dir, ok := skippedPaths[req.Mod.Path]; ok {
				problems = append(problems, fmt.Sprintf("%s: requires %s, which is skipped", m.dir, dir))
			}
		}
		for _, rep := range files[m.path].Replace {
			if byPath[rep.Old.Path] != nil {
				problems = append(problems, fmt.Sprintf("%s: replaces sibling %s, so consumers would get something the rehearsal didn't test", m.dir, rep.Old.Path))
			}
		}
	}

	ordered, cycle := orderModules(found)
	if len(cycle) > 0 {
		problems = append(problems, "dependency cycle between modules: "+strings.Join(cycle, ", "))
	}
	return ordered, skipped, prefix, problems
}

// parseGoMod parses a go.mod, given its path relative to the repository root.
func parseGoMod(root, name string) (*modfile.File, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	file, err := modfile.Parse(name, data, nil)
	if err != nil {
		return nil, err
	}
	if file.Module == nil {
		return nil, errors.New("no module directive")
	}
	return file, nil
}

// orderModules sorts modules so each one comes after the siblings it requires,
// keeping their order otherwise. It also returns the directories of any modules
// that can't be ordered because of a cycle.
func orderModules(mods []*goModule) (ordered []*goModule, cycle []string) {
	done := map[string]bool{}
	for len(ordered) < len(mods) {
		var ready []*goModule
		for _, m := range mods {
			if done[m.path] {
				continue
			}
			if !slices.ContainsFunc(m.siblings, func(s string) bool { return !done[s] }) {
				ready = append(ready, m)
			}
		}
		if len(ready) == 0 {
			for _, m := range mods {
				if !done[m.path] {
					cycle = append(cycle, m.dir)
				}
			}
			return ordered, cycle
		}
		for _, m := range ready {
			done[m.path] = true
		}
		ordered = append(ordered, ready...)
	}
	return ordered, nil
}
