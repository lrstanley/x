// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// walkTree makes files and directories under a new temp dir, from paths: a name
// ending in "/" is a directory, anything else an empty file. It returns the dir.
func walkTree(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// link makes new a symbolic link to target, or skips the test where links
// can't be made (Windows without the privilege).
func link(t *testing.T, target, newname string) {
	t.Helper()
	if err := os.Symlink(target, newname); err != nil {
		t.Skipf("cannot make symbolic links: %v", err)
	}
}

// relFiles returns the files found, relative to root and with slashes.
func relFiles(root string, srcs []string) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		rel, err := filepath.Rel(root, strings.TrimPrefix(s, "file:"))
		if err != nil {
			rel = s
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func checkFiles(t *testing.T, root string, srcs, want []string) {
	t.Helper()
	if got := relFiles(root, srcs); !slices.Equal(got, want) {
		t.Fatalf("found %v, want %v", got, want)
	}
}

func TestSystemFontFilesPlain(t *testing.T) {
	t.Parallel()

	root := walkTree(t, "b.ttf", "a.otf", "notes.txt", "sub/c.TTC", "sub/deeper/d.otc", "sub/e.woff")
	checkFiles(t, root, systemFontFiles([]string{root, filepath.Join(root, "missing")}),
		[]string{"a.otf", "b.ttf", "sub/c.TTC", "sub/deeper/d.otc"})
}

func TestSystemFontFilesFollowsLinkedDirs(t *testing.T) {
	t.Parallel()

	outside := walkTree(t, "x.ttf", "nested/y.ttf")
	root := walkTree(t, "a.ttf", "z.ttf")
	link(t, outside, filepath.Join(root, "m-linked"))

	// The linked directory is read where its name sorts, and its own
	// subdirectories too.
	checkFiles(t, root, systemFontFiles([]string{root}),
		[]string{"a.ttf", "m-linked/nested/y.ttf", "m-linked/x.ttf", "z.ttf"})

	// A font directory that is itself a link.
	top := t.TempDir()
	linkedRoot := filepath.Join(top, "fonts")
	link(t, root, linkedRoot)
	checkFiles(t, top, systemFontFiles([]string{linkedRoot}),
		[]string{"fonts/a.ttf", "fonts/m-linked/nested/y.ttf", "fonts/m-linked/x.ttf", "fonts/z.ttf"})
}

func TestSystemFontFilesLinkedFiles(t *testing.T) {
	t.Parallel()

	root := walkTree(t, "real/z.ttf")
	link(t, filepath.Join(root, "real", "z.ttf"), filepath.Join(root, "b.ttf"))
	link(t, filepath.Join(root, "nowhere.ttf"), filepath.Join(root, "broken.ttf"))

	// The font is found once, under the name the search met first; the link
	// that leads nowhere is ignored.
	checkFiles(t, root, systemFontFiles([]string{root}), []string{"b.ttf"})
}

func TestSystemFontFilesEndsLoops(t *testing.T) {
	t.Parallel()

	root := walkTree(t, "a.ttf", "sub/b.ttf")
	link(t, root, filepath.Join(root, "sub", "up"))                         // To an ancestor.
	link(t, filepath.Join(root, "sub"), filepath.Join(root, "sub", "self")) // To itself.
	link(t, filepath.Join(root, "sub"), filepath.Join(root, "zagain"))      // A second way in, met later.

	checkFiles(t, root, systemFontFiles([]string{root}), []string{"a.ttf", "sub/b.ttf"})

	// Two links to each other.
	loop := t.TempDir()
	link(t, filepath.Join(loop, "b"), filepath.Join(loop, "a"))
	link(t, filepath.Join(loop, "a"), filepath.Join(loop, "b"))
	if got := systemFontFiles([]string{loop}); len(got) != 0 {
		t.Fatalf("found %v in a directory of links to each other", got)
	}
}

func TestSystemFontFilesSharedDirsAreReadOnce(t *testing.T) {
	t.Parallel()

	shared := walkTree(t, "f.ttf")
	first, second := t.TempDir(), t.TempDir()
	link(t, shared, filepath.Join(first, "shared"))
	link(t, shared, filepath.Join(second, "shared"))

	// The first directory searched gets the fonts, as it would for a file in
	// both.
	got := systemFontFiles([]string{first, second})
	if len(got) != 1 || !strings.HasPrefix(got[0], "file:"+first) {
		t.Fatalf("found %v, want one font, through the first directory", got)
	}
}

func TestFontWalkerLimits(t *testing.T) {
	t.Parallel()

	// Depth: a root, then maxDepth levels of directories below it.
	root := walkTree(t, "0.ttf", "d1/1.ttf", "d1/d2/2.ttf", "d1/d2/d3/3.ttf", "d1/d2/d3/d4/4.ttf")
	w := newFontWalker()
	w.maxDepth = 2
	w.walk(root, 0)
	checkFiles(t, root, w.srcs, []string{"0.ttf", "d1/1.ttf", "d1/d2/2.ttf"})

	// Directories: a link to a place with many of them stops being read.
	many := walkTree(t, "d00/", "d01/", "d02/", "d03/", "d04/", "d05/", "d06/", "d07/", "d08/", "d09/")
	for i := range 10 {
		if err := os.WriteFile(filepath.Join(many, "d0"+string(rune('0'+i)), "f.ttf"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w = newFontWalker()
	w.maxDirs = 4
	w.walk(many, 0)
	if len(w.seenDirs) != 4 || len(w.srcs) != 3 { // The root and three subdirectories.
		t.Fatalf("read %d directories and found %d fonts, want 4 and 3", len(w.seenDirs), len(w.srcs))
	}

	// Files.
	w = newFontWalker()
	w.maxFiles = 5
	w.walk(many, 0)
	if len(w.srcs) != 5 {
		t.Fatalf("found %d fonts, want 5", len(w.srcs))
	}
}

// TestFontWalkerBoundsALinkToTheRoot is the case the limits are for: a link
// that leads out to a huge tree.
func TestFontWalkerBoundsALinkToTheRoot(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("no root directory to link to")
	}
	top := t.TempDir()
	link(t, "/", filepath.Join(top, "everything"))

	w := newFontWalker()
	w.maxDirs, w.maxFiles = 200, 50
	w.walk(top, 0)
	if len(w.seenDirs) > 200 || len(w.srcs) > 50 {
		t.Fatalf("read %d directories and found %d fonts, want at most 200 and 50", len(w.seenDirs), len(w.srcs))
	}
}
