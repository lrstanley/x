// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	stdzip "archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const fixtureModule = "github.com/lrstanley/modrelease-fixture"

// fixture is a repository with a bare remote and an isolated git setup. Signing
// uses a throwaway SSH key, and the module path is rewritten to the bare remote.
// These tests can't be parallel, since they set the environment.
type fixture struct {
	t              *testing.T
	repo, remote   string
	hermeticConfig string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	for _, tool := range []string{"git", "go", "ssh-keygen"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed", tool)
		}
	}

	base := t.TempDir()
	f := &fixture{
		t:              t,
		repo:           filepath.Join(base, "repo"),
		remote:         filepath.Join(base, "remote.git"),
		hermeticConfig: filepath.Join(base, "hermetic.gitconfig"),
	}
	key := filepath.Join(base, "key")
	f.must(base, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	signers := filepath.Join(base, "allowed_signers")
	f.write(signers, "test@example.invalid "+string(pub))

	rewrite := fmt.Sprintf("[url %q]\n\tinsteadOf = https://%s\n", "file://"+f.remote, fixtureModule)
	f.write(f.hermeticConfig, rewrite)
	global := filepath.Join(base, "gitconfig")
	f.write(global, rewrite+fmt.Sprintf(`[user]
	name = test
	email = test@example.invalid
	signingkey = %s
[gpg]
	format = ssh
[gpg "ssh"]
	allowedSignersFile = %s
[init]
	defaultBranch = master
[core]
	fsync = none
[gc]
	auto = 0
`, key, signers))

	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GOPROXY", "off") // Fixtures have no third-party requirements.
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GONOSUMDB", "")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOTOOLCHAIN", "local")

	f.must(base, "git", "init", "--quiet", "--bare", f.remote)
	f.must(base, "git", "init", "--quiet", f.repo)
	f.must(f.repo, "git", "remote", "add", "origin", "file://"+f.remote)

	f.writeRepo("LICENSE", "license\n")
	f.writeRepo(".gitignore", "go.work\n")
	f.writeRepo("go.mod", "module "+fixtureModule+"\n\ngo 1.25\n\nrequire "+fixtureModule+"/reel v0.0.0\n")
	f.writeRepo("root.go", "package root\n\nimport \""+fixtureModule+"/reel\"\n\n// F is F.\nfunc F() string { return reel.F() }\n")
	f.writeRepo("reel/go.mod", "module "+fixtureModule+"/reel\n\ngo 1.25\n")
	f.writeRepo("reel/reel.go", "package reel\n\n// F is F.\nfunc F() string { return \"f\" }\n")
	// A single main package makes go build ./... write a binary, if it's allowed.
	f.writeRepo("cmd/app/go.mod", "module "+fixtureModule+"/cmd/app\n\ngo 1.25\n\nrequire "+fixtureModule+"/reel v0.0.0\n")
	f.writeRepo("cmd/app/main.go", "package main\n\nimport \""+fixtureModule+"/reel\"\n\nfunc main() { println(reel.F()) }\n")
	f.writeRepo("_examples/go.mod", "module "+fixtureModule+"/_examples\n\ngo 1.25\n")
	f.commit("initial")
	f.must(f.repo, "git", "push", "--quiet", "--set-upstream", "origin", "master")
	return f
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec
		f.t.Fatal(err)
	}
}

func (f *fixture) writeRepo(name, content string) {
	f.t.Helper()
	f.write(filepath.Join(f.repo, filepath.FromSlash(name)), content)
}

func (f *fixture) must(dir, name string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	return f.must(f.repo, "git", args...)
}

func (f *fixture) commit(message string) {
	f.t.Helper()
	f.git("add", "--all")
	f.git("commit", "--quiet", "-m", message)
}

// commitAndPush publishes a change, so the fixture stays in sync with the remote.
func (f *fixture) commitAndPush(message string) {
	f.t.Helper()
	f.commit(message)
	f.git("push", "--quiet", "origin", "master")
}

// hook installs a local git hook, skipping the test where sh isn't available.
func (f *fixture) hook(name, script string) {
	f.t.Helper()
	if runtime.GOOS == "windows" {
		f.t.Skip("needs a POSIX shell for its git hook")
	}
	path := filepath.Join(f.repo, ".git", "hooks", name)
	f.write(path, "#!/bin/sh\n"+script+"\n")
	if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec
		f.t.Fatal(err)
	}
}

// run runs the tool in the fixture and returns its output.
func (f *fixture) run(flags Flags) (string, error) {
	f.t.Helper()
	if flags.Remote == "" {
		flags.Remote = "origin"
	}
	var out bytes.Buffer
	r := &release{
		flags:             flags,
		out:               &out,
		dir:               f.repo,
		hermeticGitConfig: f.hermeticConfig,
	}
	err := r.run(context.Background())
	return out.String(), err
}

// mustRun runs the tool and fails the test if it fails.
func (f *fixture) mustRun(flags Flags) string {
	f.t.Helper()
	out, err := f.run(flags)
	if err != nil {
		f.t.Fatalf("%v\n%s", err, out)
	}
	return out
}

// state is everything a plan must leave alone.
func (f *fixture) state() string {
	f.t.Helper()
	return strings.Join([]string{
		f.git("rev-parse", "HEAD"),
		f.git("status", "--porcelain"),
		f.git("tag", "--list"),
		f.git("worktree", "list", "--porcelain"),
		f.git("ls-remote", "origin"),
	}, "\n")
}

func (f *fixture) remoteTagNames() []string {
	f.t.Helper()
	var names []string
	for line := range strings.Lines(f.git("ls-remote", "--tags", "origin")) {
		if _, ref, ok := strings.Cut(strings.TrimSpace(line), "refs/tags/"); ok && !strings.HasSuffix(ref, "^{}") {
			names = append(names, ref)
		}
	}
	return names
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("missing %q in:\n%s", want, got)
	}
}

func TestPlanChangesNothing(t *testing.T) {
	f := newFixture(t)
	before := f.state()

	out := f.mustRun(Flags{Bump: "v0.1.0"})

	if after := f.state(); after != before {
		t.Errorf("state changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, want := range []string{
		"plan only",
		"(no version tags yet) -> v0.1.0",
		"Release 3 modules, in dependency order",
		"skip _examples (hidden directory)",
		fixtureModule + "/reel  v0.0.0 -> v0.1.0",
		"tidy, build, test: ok",
		"cmd/app/v0.1.0",
		"Run again with --apply",
	} {
		assertContains(t, out, want)
	}
}

func TestApplyPublishesOneSignedCommit(t *testing.T) {
	f := newFixture(t)

	out := f.mustRun(Flags{Bump: "v0.1.0", Apply: true})
	assertContains(t, out, "Released v0.1.0.")

	want := []string{"cmd/app/v0.1.0", "reel/v0.1.0", "v0.1.0"}
	if got := f.remoteTagNames(); !slices.Equal(got, want) {
		t.Errorf("remote tags: got %v, want %v", got, want)
	}

	commit := f.git("rev-parse", "HEAD")
	if remote := strings.Fields(f.git("ls-remote", "origin", "refs/heads/master"))[0]; remote != commit {
		t.Errorf("remote master is %s, local is %s", remote, commit)
	}
	for _, tag := range want {
		if got := f.git("rev-parse", tag+"^{commit}"); got != commit {
			t.Errorf("tag %s is at %s, want the release commit %s", tag, got, commit)
		}
		if got := f.git("for-each-ref", "--format=%(contents:signature)", "refs/tags/"+tag); got == "" {
			t.Errorf("tag %s isn't signed", tag)
		}
	}
	if got := f.git("log", "-1", "--format=%G?"); got != "G" {
		t.Errorf("commit signature status is %q, want G", got)
	}
	assertContains(t, f.git("log", "-1", "--format=%B"), "Signed-off-by: test <test@example.invalid>")
	assertContains(t, f.git("log", "-1", "--format=%s"), "chore(release): v0.1.0")

	// Nothing but go.mod and go.sum may be released, including a built binary.
	for name := range strings.Lines(f.git("show", "--name-only", "--format=", "HEAD")) {
		base := filepath.Base(strings.TrimSpace(name))
		if base != "go.mod" && base != "go.sum" {
			t.Errorf("release commit includes %s", strings.TrimSpace(name))
		}
	}
	if got := f.git("status", "--porcelain"); got != "" {
		t.Errorf("worktree isn't clean:\n%s", got)
	}

	// The go.sum in the release must hold what a real fetch of the tag computes.
	assertContains(t, out, "match    reel")
	assertContains(t, f.git("show", "HEAD:cmd/app/go.sum"), fixtureModule+"/reel v0.1.0 h1:")
}

func TestPublishedModulesBuildWithoutAWorkspace(t *testing.T) {
	f := newFixture(t)
	f.mustRun(Flags{Bump: "v0.1.0", Apply: true})

	// A fresh checkout with a read-only go.sum proves the hashes are usable.
	clone := filepath.Join(t.TempDir(), "clone")
	f.must(t.TempDir(), "git", "clone", "--quiet", "file://"+f.remote, clone)
	t.Setenv("GIT_CONFIG_GLOBAL", f.hermeticConfig)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "direct")
	t.Setenv("GONOSUMDB", fixtureModule)
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "modcache"))
	t.Setenv("GOFLAGS", "-modcacherw -mod=readonly")
	for _, dir := range []string{".", "cmd/app", "reel"} {
		f.must(filepath.Join(clone, dir), "go", "build", "-o", os.DevNull, "./...")
	}
}

func TestBumpsThroughARelease(t *testing.T) {
	f := newFixture(t)
	f.mustRun(Flags{Bump: "v1.2.0", Apply: true})

	for _, step := range []struct {
		flags Flags
		want  string
	}{
		{Flags{Bump: "minor", Beta: true, Apply: true}, "v1.3.0-beta.0"},
		{Flags{Bump: "minor", RC: true, Apply: true}, "v1.3.0-rc.0"},
		{Flags{Bump: "minor", Apply: true}, "v1.3.0"},
	} {
		out := f.mustRun(step.flags)
		assertContains(t, out, "Released "+step.want+".")
	}
	if got := currentVersion(f.remoteTagNames()); got != "v1.3.0" {
		t.Errorf("current version is %s, want v1.3.0", got)
	}

	out, err := f.run(Flags{Bump: "major"})
	if !errors.Is(err, errReported) {
		t.Fatalf("major to v2: got %v, want the checks to fail", err)
	}
	assertContains(t, out, "which needs a path ending in /v2")
}

func TestChecksFail(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
		flags Flags
		want  []string
	}{
		{
			name:  "modified file",
			setup: func(f *fixture) { f.writeRepo("LICENSE", "changed\n") },
			want:  []string{"worktree is dirty (1 files), first: M LICENSE"},
		},
		{
			name:  "untracked file",
			setup: func(f *fixture) { f.writeRepo("stray", "x") },
			want:  []string{"worktree is dirty (1 files), first: ?? stray"},
		},
		{
			name: "staged file",
			setup: func(f *fixture) {
				f.writeRepo("stray", "x")
				f.git("add", "stray")
			},
			want: []string{"worktree is dirty (1 files), first: A  stray"},
		},
		{
			name:  "detached",
			setup: func(f *fixture) { f.git("checkout", "--quiet", "--detach") },
			want:  []string{"HEAD is detached"},
		},
		{
			name:  "wrong branch",
			setup: func(f *fixture) { f.git("checkout", "--quiet", "-b", "wip") },
			want:  []string{"on branch wip, not default branch master", "branch wip doesn't exist on origin"},
		},
		{
			name:  "ahead",
			setup: func(f *fixture) { f.git("commit", "--quiet", "--allow-empty", "-m", "ahead") },
			want:  []string{"local master is ahead of origin"},
		},
		{
			name: "behind",
			setup: func(f *fixture) {
				f.git("commit", "--quiet", "--allow-empty", "-m", "elsewhere")
				f.git("push", "--quiet", "origin", "master")
				f.git("reset", "--quiet", "--hard", "HEAD~1")
			},
			want: []string{"local master is behind or has diverged from origin"},
		},
		{
			name:  "tag only local",
			setup: func(f *fixture) { f.git("tag", "reel/v0.1.0") },
			want:  []string{"tag reel/v0.1.0 exists locally but not on origin"},
		},
		{
			name: "tag only on remote",
			setup: func(f *fixture) {
				f.git("tag", "reel/v0.1.0")
				f.git("push", "--quiet", "origin", "refs/tags/reel/v0.1.0")
				f.git("tag", "--delete", "reel/v0.1.0")
			},
			want: []string{"tag reel/v0.1.0 already exists on origin"},
		},
		{
			name:  "missing remote",
			setup: func(*fixture) {},
			flags: Flags{Remote: "nope"},
			want:  []string{`remote "nope" isn't configured`},
		},
		{
			name: "two push URLs",
			setup: func(f *fixture) {
				other := filepath.Join(f.t.TempDir(), "other.git")
				f.must(f.repo, "git", "init", "--quiet", "--bare", other)
				f.git("remote", "set-url", "--add", "--push", "origin", "file://"+f.remote)
				f.git("remote", "set-url", "--add", "--push", "origin", "file://"+other)
			},
			want: []string{"remote origin has several push URLs"},
		},
		{
			name:  "broken signing",
			setup: func(f *fixture) { f.git("config", "user.signingkey", filepath.Join(f.repo, "missing")) },
			want:  []string{"signing failed"},
		},
		{
			name:  "several at once",
			setup: func(f *fixture) { f.writeRepo("stray", "x"); f.git("checkout", "--quiet", "-b", "wip") },
			want:  []string{"worktree is dirty", "on branch wip", "3 checks failed"},
		},
		{
			name: "bad module path",
			setup: func(f *fixture) {
				f.writeRepo("reel/go.mod", "module "+fixtureModule+"/v2/reel\n\ngo 1.25\n")
				f.commitAndPush("bad path")
			},
			want: []string{"reel: module path " + fixtureModule + "/v2/reel can't be fetched from this directory"},
		},
		{
			name: "cycle",
			setup: func(f *fixture) {
				f.writeRepo("reel/go.mod", "module "+fixtureModule+"/reel\n\ngo 1.25\n\nrequire "+fixtureModule+" v0.0.0\n")
				f.commitAndPush("cycle")
			},
			want: []string{"dependency cycle between modules"},
		},
		{
			name: "replaced sibling",
			setup: func(f *fixture) {
				f.writeRepo("cmd/app/go.mod", "module "+fixtureModule+"/cmd/app\n\ngo 1.25\n\nrequire "+fixtureModule+"/reel v0.0.0\n\nreplace "+fixtureModule+"/reel => ../../reel\n")
				f.commitAndPush("replace")
			},
			want: []string{"cmd/app: replaces sibling"},
		},
		{
			name: "go.mod without a module directive",
			setup: func(f *fixture) {
				f.writeRepo("reel/go.mod", "go 1.25\n")
				f.commitAndPush("no module directive")
			},
			want: []string{"reel/go.mod: no module directive"},
		},
		{
			name:  "skip of a required module",
			setup: func(*fixture) {},
			flags: Flags{Skip: []string{"reel"}},
			want:  []string{"requires reel, which is skipped"},
		},
		{
			name:  "skip of nothing",
			setup: func(*fixture) {},
			flags: Flags{Skip: []string{"nope"}},
			want:  []string{"--skip nope: no module in that directory"},
		},
		{
			name:  "skip of the root",
			setup: func(*fixture) {},
			flags: Flags{Skip: []string{"."}},
			want:  []string{"the root module can't be skipped"},
		},
		{
			name:  "bump with no tags",
			setup: func(*fixture) {},
			flags: Flags{Bump: "patch"},
			want:  []string{"the first release needs an explicit version"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			before := f.state()

			flags := tt.flags
			if flags.Bump == "" {
				flags.Bump = "v0.1.0"
			}
			out, err := f.run(flags)
			if !errors.Is(err, errReported) {
				t.Fatalf("got error %v, want the checks to fail\n%s", err, out)
			}
			assertContains(t, out, "Nothing changed.")
			for _, want := range tt.want {
				assertContains(t, out, want)
			}
			if after := f.state(); after != before {
				t.Errorf("state changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestIgnoredFilesDontCountAsDirty(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("go.work", "go 1.25\n\nuse ./reel\n")
	assertContains(t, f.mustRun(Flags{Bump: "v0.1.0"}), "worktree is clean")
}

func TestSkip(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun(Flags{Bump: "v0.1.0", Skip: []string{"cmd/app"}, Apply: true})
	assertContains(t, out, "skip cmd/app (--skip)")
	want := []string{"reel/v0.1.0", "v0.1.0"}
	if got := f.remoteTagNames(); !slices.Equal(got, want) {
		t.Errorf("remote tags: got %v, want %v", got, want)
	}
}

func TestRunsFromASubdirectory(t *testing.T) {
	f := newFixture(t)
	var out bytes.Buffer
	r := &release{
		flags:             Flags{Bump: "v0.1.0", Remote: "origin"},
		out:               &out,
		dir:               filepath.Join(f.repo, "reel"),
		hermeticGitConfig: f.hermeticConfig,
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	assertContains(t, out.String(), "Release 3 modules")
}

func TestRunsFromALinkedWorktree(t *testing.T) {
	f := newFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	f.git("checkout", "--quiet", "--detach")
	f.git("worktree", "add", "--quiet", linked, "master")

	var out bytes.Buffer
	r := &release{
		flags:             Flags{Bump: "v0.1.0", Remote: "origin", Apply: true},
		out:               &out,
		dir:               linked,
		hermeticGitConfig: f.hermeticConfig,
	}
	if err := r.run(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	assertContains(t, out.String(), "Released v0.1.0.")
}

func TestFailingTestStopsTheRelease(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("reel/reel_test.go", "package reel\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) { t.Fatal(\"the marker\") }\n")
	f.commitAndPush("add a failing test")
	before := f.state()

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, out, "Rehearsal failed. Nothing changed.")
	assertContains(t, err.Error(), "the marker")
	if after := f.state(); after != before {
		t.Errorf("state changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestChangedTrackedFileStopsTheRelease(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("reel/data.txt", "original\n")
	f.writeRepo("reel/reel_test.go", "package reel\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestWrite(t *testing.T) { _ = os.WriteFile(\"data.txt\", []byte(\"changed\"), 0o644) }\n")
	f.commitAndPush("add a test that changes a tracked file")

	out, err := f.run(Flags{Bump: "v0.1.0"})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, err.Error(), "the rehearsal changed a tracked file")
}

// A test that passes and then stages a change must not get that change released.
func TestStagedSourceChangeStopsTheRelease(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("reel/reel_test.go", `package reel

import (
	"os"
	"os/exec"
	"testing"
)

func TestStage(t *testing.T) {
	if err := os.WriteFile("reel.go", []byte("package reel\n\n// F is F.\nfunc F() string { return \"replaced\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "add", "reel.go").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
`)
	f.commitAndPush("add a test that stages a source change")

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, err.Error(), "the rehearsal changed a tracked file: reel/reel.go")
	if got := f.remoteTagNames(); len(got) != 0 {
		t.Errorf("remote tags landed: %v", got)
	}
}

// A file that sorts first in git's output must be seen too.
func TestChangedFileSortingFirstStopsTheRelease(t *testing.T) {
	f := newFixture(t)
	f.writeRepo(".release-data", "original\n")
	f.writeRepo("root_test.go", "package root\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestWrite(t *testing.T) { _ = os.WriteFile(\".release-data\", []byte(\"changed\"), 0o644) }\n")
	f.commitAndPush("add a test that changes a dotfile")

	out, err := f.run(Flags{Bump: "v0.1.0"})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, err.Error(), "the rehearsal changed a tracked file: .release-data")
}

// The commit has to hold what was tested, whatever the repository's hooks do.
func TestHooksCantChangeTheRelease(t *testing.T) {
	f := newFixture(t)
	f.hook("pre-commit", "echo changed >> LICENSE && git add LICENSE")

	f.mustRun(Flags{Bump: "v0.1.0", Apply: true})

	if got := f.git("show", "HEAD:LICENSE"); got != "license" {
		t.Errorf("the hook changed the release: LICENSE is %q", got)
	}
}

// A release with nothing to update still gets its commit and tags.
func TestReleaseWithNoManifestChanges(t *testing.T) {
	f := newFixture(t)
	f.git("rm", "-r", "--quiet", "reel", "cmd", "_examples", "root.go")
	f.writeRepo("go.mod", "module "+fixtureModule+"\n\ngo 1.25\n")
	f.writeRepo("root.go", "package root\n")
	f.commitAndPush("reduce to one module")
	head := f.git("rev-parse", "HEAD")

	assertContains(t, f.mustRun(Flags{Bump: "v0.1.0", Apply: true}), "Released v0.1.0.")
	if got := f.git("rev-parse", "v0.1.0^{commit}"); got == head {
		t.Error("the tag is on the old HEAD, not a release commit")
	}
	if got := f.git("diff", "--name-only", head, "v0.1.0"); got != "" {
		t.Errorf("the release changed files: %s", got)
	}
}

// The local proxy's path goes into a URL, so its characters have to survive that.
func TestTempDirWithURLCharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("# isn't the concern here")
	}
	f := newFixture(t)
	tmp := filepath.Join(t.TempDir(), "a#b?c%d")
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)

	assertContains(t, f.mustRun(Flags{Bump: "v0.1.0"}), "Run again with --apply")
}

// A go.mod in a hidden directory may be invalid on purpose.
func TestInvalidGoModInHiddenDirectory(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("reel/testdata/broken/go.mod", "this isn't a go.mod\n")
	f.commitAndPush("add an invalid go.mod to testdata")

	assertContains(t, f.mustRun(Flags{Bump: "v0.1.0"}), "skip reel/testdata/broken (hidden directory)")
}

// push.followTags must not publish a tag that isn't part of the release.
func TestFollowTagsDoesntPublishOtherTags(t *testing.T) {
	f := newFixture(t)
	f.git("config", "push.followTags", "true")
	f.git("tag", "-s", "-m", "left behind", "v0.0.9")

	f.mustRun(Flags{Bump: "v0.1.0", Apply: true})

	if got := f.remoteTagNames(); slices.Contains(got, "v0.0.9") {
		t.Errorf("a tag outside the plan was published: %v", got)
	}
}

// A failure partway through tagging must not leave tags behind to block the next run.
func TestTagsAreRemovedWhenTaggingFails(t *testing.T) {
	f := newFixture(t)
	f.hook("reference-transaction", `[ "$1" = prepared ] || exit 0
while read -r _ _ ref; do
	[ "$ref" = refs/tags/cmd/app/v0.1.0 ] && exit 1
done
exit 0`)

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, err.Error(), "nothing was published")
	if got := f.git("tag", "--list"); got != "" {
		t.Errorf("local tags remain: %s", got)
	}
	if got := f.remoteTagNames(); len(got) != 0 {
		t.Errorf("remote tags landed: %v", got)
	}
}

func TestCommandOutlivedByAChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	defer func(wait time.Duration) { pipeWait = wait }(pipeWait)
	pipeWait = 50 * time.Millisecond

	r := &release{out: &bytes.Buffer{}}
	start := time.Now()
	// The shell succeeds at once, and leaves a child holding its output pipes.
	out, err := r.cmdRun(context.Background(), "", nil, "sh", "-c", "echo done; sleep 10 &")
	if err != nil {
		t.Fatal(err)
	}
	if out != "done" {
		t.Errorf("got %q", out)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %s, so it waited for the child", took)
	}
}

func TestUnfetchedTagsCountFromTheRemote(t *testing.T) {
	f := newFixture(t)
	f.mustRun(Flags{Bump: "v1.0.0", Apply: true})
	for tag := range strings.FieldsSeq(f.git("tag", "--list")) {
		f.git("tag", "--delete", tag)
	}
	assertContains(t, f.mustRun(Flags{Bump: "patch"}), "v1.0.0 -> v1.0.1")
}

// The public proxy caches a not-found answer, which would delay the real version
// for consumers, so the rehearsal must never ask it about these modules.
func TestRehearsalDoesntAskTheProxyAboutUnpublishedVersions(t *testing.T) {
	f := newFixture(t)
	f.writeRepo("extra/go.mod", "module "+fixtureModule+"/extra\n\ngo 1.25\n\nrequire "+fixtureModule+" v0.0.0\n")
	f.writeRepo("extra/extra.go", "package extra\n\nimport root \""+fixtureModule+"\"\n\n// F is F.\nfunc F() string { return root.F() }\n")
	f.commitAndPush("add a second level of requirements")

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, req.URL.Path)
		http.NotFound(w, req)
	}))
	defer server.Close()
	t.Setenv("GOPROXY", server.URL)

	f.mustRun(Flags{Bump: "v0.1.0"})

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 0 {
		t.Errorf("the proxy was asked for:\n%s", strings.Join(requests, "\n"))
	}
}

// runWithProxy runs a release with its public proxy check pointed at handler.
func (f *fixture) runWithProxy(handler http.HandlerFunc) (string, error) {
	f.t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()

	var out bytes.Buffer
	r := &release{
		flags:             Flags{Bump: "v0.1.0", Remote: "origin", Apply: true},
		out:               &out,
		dir:               f.repo,
		hermeticGitConfig: f.hermeticConfig,
		publicProxy:       server.URL,
	}
	err := r.run(context.Background())
	return out.String(), err
}

// A proxy that can't serve the version yet doesn't undo a release.
func TestProxyNotFoundIsAWarning(t *testing.T) {
	f := newFixture(t)
	out, err := f.runWithProxy(http.NotFound)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	assertContains(t, out, "warning: couldn't fetch")
	assertContains(t, out, "the direct check is what counts")
	assertContains(t, out, "Released v0.1.0, but not every hash could be checked.")
}

// A proxy that serves different content means the release's go.sum files are wrong.
func TestProxyMismatchIsAnError(t *testing.T) {
	f := newFixture(t)
	t.Setenv("GOSUMDB", "off")
	out, err := f.runWithProxy(func(w http.ResponseWriter, req *http.Request) {
		modPath, file, _ := strings.Cut(strings.TrimPrefix(req.URL.Path, "/"), "/@v/")
		switch file {
		case "v0.1.0.info":
			_, _ = w.Write([]byte(`{"Version":"v0.1.0"}`))
		case "v0.1.0.mod":
			_, _ = w.Write([]byte("module " + modPath + "\n"))
		case "v0.1.0.zip":
			zw := stdzip.NewWriter(w)
			defer func() { _ = zw.Close() }()
			entry, _ := zw.Create(modPath + "@v0.1.0/go.mod")
			_, _ = entry.Write([]byte("module " + modPath + "\n"))
		default:
			http.NotFound(w, req)
		}
	})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, out, "MISMATCH")
	assertContains(t, err.Error(), "not the predicted")
	if strings.Contains(out, "Released") {
		t.Errorf("reported as released:\n%s", out)
	}
}

// A remote that moves after the checks pass is a definite answer: nothing landed.
func TestPushRejected(t *testing.T) {
	f := newFixture(t)
	other := f.must(f.repo, "git", "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "elsewhere")
	f.hook("pre-push", fmt.Sprintf("git push --quiet --no-verify origin %s:refs/heads/master", other))
	head := f.git("rev-parse", "HEAD")

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, err.Error(), "rejected the push")
	if got := f.git("tag", "--list"); got != "" {
		t.Errorf("local tags remain: %s", got)
	}
	if got := f.remoteTagNames(); len(got) != 0 {
		t.Errorf("remote tags landed: %v", got)
	}
	if got := f.git("rev-parse", "HEAD"); got != head {
		t.Errorf("local HEAD moved to %s", got)
	}
	f.must(f.repo, "git", "merge-base", "--is-ancestor", head, other) // The commit is still there to pull.
}

// A push that fails with no answer from the remote proves nothing, so the tool
// keeps everything rather than risk discarding a release that landed.
func TestPushOutcomeUnknown(t *testing.T) {
	f := newFixture(t)
	f.hook("pre-push", "exit 1")

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true})
	if err == nil {
		t.Fatalf("release succeeded:\n%s", out)
	}
	assertContains(t, err.Error(), "the outcome of the push is unknown")
	want := []string{"cmd/app/v0.1.0", "reel/v0.1.0", "v0.1.0"}
	if got := strings.Fields(f.git("tag", "--list")); !slices.Equal(got, want) {
		t.Errorf("local tags: got %v, want %v", got, want)
	}
	if got := f.remoteTagNames(); len(got) != 0 {
		t.Errorf("remote tags: %v", got)
	}

	// The next run must not pretend this didn't happen.
	f.hook("pre-push", "exit 0")
	out, err = f.run(Flags{Bump: "v0.1.0", Apply: true})
	if !errors.Is(err, errReported) {
		t.Fatalf("second run: got %v, want the checks to fail\n%s", err, out)
	}
	assertContains(t, out, "An earlier release may have been interrupted")
}

// A push that reports failure but did land is a published release.
func TestPushFailsButLanded(t *testing.T) {
	f := newFixture(t)
	f.hook("pre-push", `set --
while read -r _ sha ref _; do set -- "$@" "$sha:$ref"; done
git push --quiet --no-verify --atomic origin "$@"
exit 1`)

	out := f.mustRun(Flags{Bump: "v0.1.0", Apply: true})
	assertContains(t, out, "the push didn't finish, but every tag is on origin")
	assertContains(t, out, "Released v0.1.0.")
	if got := f.git("rev-parse", "HEAD"); got != f.git("rev-parse", "v0.1.0^{commit}") {
		t.Errorf("local master wasn't fast-forwarded")
	}
}

// LFS: tests see real files, while hashes match a fetch with no LFS filter, as on
// the public proxy.
func TestLFS(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs isn't installed")
	}
	f := newFixture(t)
	f.git("config", "filter.lfs.clean", "git-lfs clean -- %f")
	f.git("config", "filter.lfs.smudge", "git-lfs smudge -- %f")
	f.git("config", "filter.lfs.process", "git-lfs filter-process")
	f.git("config", "filter.lfs.required", "true")

	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	f.writeRepo(".gitattributes", "**/testdata/**/*.png filter=lfs diff=lfs merge=lfs -text\n")
	f.writeRepo("reel/testdata/a.png", string(png))
	f.writeRepo("reel/reel_test.go", `package reel

import (
	"bytes"
	"os"
	"testing"
)

func TestRealFile(t *testing.T) {
	b, err := os.ReadFile("testdata/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatalf("got an LFS pointer: %q", b)
	}
}
`)
	f.commitAndPush("add LFS test data")
	if got := f.git("show", "HEAD:reel/testdata/a.png"); !strings.HasPrefix(got, "version https://git-lfs") {
		t.Fatalf("the fixture isn't using LFS: %q", got)
	}

	out := f.mustRun(Flags{Bump: "v0.1.0", Apply: true})
	assertContains(t, out, "git-lfs is available")
	assertContains(t, out, "match    reel")
	if strings.Contains(out, "warning") || strings.Contains(out, "MISMATCH") {
		t.Errorf("verification wasn't clean:\n%s", out)
	}
}

func TestWithoutPatterns(t *testing.T) {
	t.Parallel()

	paths := []string{"example.com/repo", "example.com/repo/sub"}
	tests := []struct {
		patterns, want string
	}{
		{"", ""},
		{"corp.example", "corp.example"},
		{"example.com/repo", ""},
		{"example.com/*", ""},
		{"example.com/repo/sub", ""},
		{"corp.example,example.com/repo,other.example/*", "corp.example,other.example/*"},
	}
	for _, tt := range tests {
		t.Run(tt.patterns, func(t *testing.T) {
			t.Parallel()
			if got := withoutPatterns(tt.patterns, paths); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPushWasRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, output string
		want         bool
	}{
		{"nothing", "", false},
		{"accepted", "To file:///r.git\n \tabc:refs/heads/master\tabc..def\nDone\n", false},
		{"rejected", "To file:///r.git\n!\tabc:refs/heads/master\t[rejected] (fetch first)\nDone\n", true},
		{"atomic", "To file:///r.git\n!\trefs/tags/v1:refs/tags/v1\t[rejected] (atomic push failed)\n", true},
		{"remote rejected", "To file:///r.git\n!\tabc:refs/heads/master\t[remote rejected] (hook declined)\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pushWasRejected(tt.output); got != tt.want {
				t.Errorf("got %t, want %t", got, tt.want)
			}
		})
	}
}
