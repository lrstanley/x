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

// goModule is one module being released, or a dependent: a module that isn't
// released, but builds against released modules from their directories.
type goModule struct {
	dir, path, tag string   // A dependent has no tag.
	siblings       []string // Paths of released modules that it requires.

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

// unreleased is a module found by discover that isn't released. Its go.mod is
// nil if it wasn't parsed.
type unreleased struct {
	dir, reason string
	file        *modfile.File
}

// tagFor returns the tag for a module directory at a version.
func tagFor(dir, version string) string {
	if dir == "." {
		return version
	}
	return dir + "/" + version
}

// discover finds the modules to release and orders them so that each one comes
// after the siblings it requires. It also returns the dependents, the modules
// that aren't released but replace a released module with its directory, since
// the release changes what they build against. And it returns the repository's
// module path, and everything about the modules that stops a release.
func (r *release) discover(ctx context.Context, root, version string) (mods, dependents []*goModule, skipped []skippedModule, prefix string, problems []string) {
	rootMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, nil, nil, "", []string{"there's no go.mod at the repository root, and the version comes from its tags"}
	}
	prefix, _, _ = module.SplitPathVersion(modfile.ModulePath(rootMod))

	out, err := r.git(ctx, root, "ls-files", "--", ":(glob)**/go.mod")
	if err != nil {
		return nil, nil, nil, "", []string{fmt.Sprintf("couldn't list modules: %v", err)}
	}

	files := map[string]*modfile.File{}
	skips := slices.Clone(r.flags.Skip)
	skippedPaths := map[string]string{}
	byPath := map[string]*goModule{}
	var found []*goModule
	var others []unreleased
	for gomod := range strings.Lines(out) {
		name := strings.TrimSpace(gomod)
		dir := path.Dir(name)
		skip := slices.Contains(skips, dir)
		if skip {
			skips = slices.DeleteFunc(skips, func(s string) bool { return s == dir })
		}
		// A hidden module may be a dependent, but one that can't be parsed is
		// just skipped, and one in testdata isn't parsed at all, since it may be
		// invalid on purpose.
		if !skip && isHidden(dir) {
			u := unreleased{dir: dir, reason: "hidden directory"}
			if !inTestdata(dir) {
				u.file, _ = parseGoMod(root, name)
			}
			others = append(others, u)
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
			others = append(others, unreleased{dir: dir, reason: "--skip", file: file})
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
		problems = append(problems, linkSiblings(m, files[m.path], byPath, skippedPaths)...)
	}

	for _, u := range others {
		if d := dependent(u, byPath); d != nil {
			dependents = append(dependents, d)
		} else {
			skipped = append(skipped, skippedModule{u.dir, u.reason})
		}
	}

	ordered, cycle := orderModules(found)
	if len(cycle) > 0 {
		problems = append(problems, "dependency cycle between modules: "+strings.Join(cycle, ", "))
	}
	return ordered, dependents, skipped, prefix, problems
}

// linkSiblings sets the released siblings a module requires, and returns the
// problems with its requirements and replacements.
func linkSiblings(m *goModule, file *modfile.File, released map[string]*goModule, skippedPaths map[string]string) (problems []string) {
	for _, req := range file.Require {
		if released[req.Mod.Path] != nil {
			m.siblings = append(m.siblings, req.Mod.Path)
		}
		if dir, ok := skippedPaths[req.Mod.Path]; ok {
			problems = append(problems, fmt.Sprintf("%s: requires %s, which is skipped", m.dir, dir))
		}
	}
	for _, rep := range file.Replace {
		if released[rep.Old.Path] != nil {
			problems = append(problems, fmt.Sprintf("%s: replaces sibling %s, so consumers would get something the rehearsal didn't test", m.dir, rep.Old.Path))
		}
	}
	return problems
}

// inTestdata reports whether a module directory is in, or is, a testdata
// directory.
func inTestdata(dir string) bool {
	return slices.Contains(strings.Split(dir, "/"), "testdata")
}

// dependent returns the module as a dependent, if it replaces a released module
// with a directory, or nil. Its siblings are the replaced modules it requires.
func dependent(u unreleased, released map[string]*goModule) *goModule {
	if u.file == nil {
		return nil
	}
	replaces := false
	for _, rep := range u.file.Replace {
		// A replacement with no version is a directory.
		if released[rep.Old.Path] != nil && rep.New.Version == "" {
			replaces = true
		}
	}
	if !replaces {
		return nil
	}
	d := &goModule{dir: u.dir, path: u.file.Module.Mod.Path}
	for _, req := range u.file.Require {
		if released[req.Mod.Path] != nil {
			d.siblings = append(d.siblings, req.Mod.Path)
		}
	}
	return d
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
