// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newReleaseFixture is a fixture on GitHub, with a fake API, and a few commits
// worth releasing.
func newReleaseFixture(t *testing.T) (*fixture, *fakeGitHub) {
	t.Helper()
	f := newFixture(t)
	f.github = "lrstanley/fixture"
	api := &fakeGitHub{}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	f.githubAPI = server.URL + "/"
	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("GITHUB_TOKEN", "")

	f.writeRepo(".github/SECURITY.md", "security\n")
	f.commitAs("docs: add a security policy", "Doc Writer <docs@example.invalid>")
	f.writeRepo("reel/more.go", "package reel\n\n// G is G.\nfunc G() string { return \"g\" }\n")
	f.commitAs("feat(reel): add G", "Some One <12345+someone@users.noreply.github.com>")
	f.writeRepo("reel/more.go", "package reel\n\n// G is G.\nfunc G() string { return \"G\" }\n")
	f.commitAs("fix(reel)!: capitalize G\n\nBREAKING CHANGE: G returns \"G\".", "Other Person <other@example.invalid>")
	f.git("push", "--quiet", "origin", "master")

	// Only the API knows which account other@example.invalid is.
	api.commits = fmt.Sprintf(`[{"sha":%q,"author":{"login":"other"},"commit":{"author":{"email":"other@example.invalid"}}}]`,
		f.git("rev-parse", "HEAD"))
	return f, api
}

// commitAs commits everything, with an author other than the fixture's user.
func (f *fixture) commitAs(message, author string) {
	f.t.Helper()
	f.git("add", "--all")
	f.git("commit", "--quiet", "--author", author, "-m", message)
}

func TestGitHubReleasePlan(t *testing.T) {
	f, api := newReleaseFixture(t)
	before := f.state()

	out := f.mustRun(Flags{Bump: "v0.1.0", GitHubRelease: true})
	if after := f.state(); after != before {
		t.Errorf("state changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, want := range []string{
		"ok   can create GitHub release v0.1.0, with 4 changes since the first commit",
		"8. Create GitHub release v0.1.0",
		"│ ## Changelog",
		"│ ### :warning: Breaking Changes",
		": **reel:** capitalize G (@other)",
		`│   * G returns "G".`,
		": **reel:** add G (@someone)",
		": add a security policy (Doc Writer)",
		": initial (test)",
		"│ go get " + fixtureModule + "/reel@v0.1.0",
		"│ go install " + fixtureModule + "/cmd/app@v0.1.0",
		"│ * :old_key: Find a vulnerability? Check out our [Security and Disclosure](https://github.com/lrstanley/fixture/security/policy) policy.",
		"│   * [License](https://github.com/lrstanley/fixture/blob/master/LICENSE)",
		"9. Check the published hashes",
		"steps 5 to 9",
	} {
		assertContains(t, out, want)
	}
	if strings.Contains(out, "Full changelog") {
		t.Errorf("the first release links a comparison:\n%s", out)
	}
	if len(api.releases) > 0 {
		t.Errorf("a plan created releases: %v", api.releases)
	}
}

func TestGitHubReleaseAfterPush(t *testing.T) {
	f, api := newReleaseFixture(t)
	highlights := filepath.Join(t.TempDir(), "highlights.md")
	f.write(highlights, "## :star2: Highlights\n\nG is here.\n")

	out := f.mustRun(Flags{Bump: "v0.1.0", Apply: true, GitHubRelease: true, Highlights: highlights})
	assertContains(t, out, "created https://github.com/lrstanley/fixture/releases/tag/v0.1.0")
	assertContains(t, out, "Released v0.1.0.")
	if len(api.releases) != 1 {
		t.Fatalf("created %d releases, want 1", len(api.releases))
	}
	got := api.releases[0]
	if got.TagName != "v0.1.0" || got.Name != "v0.1.0" || got.Prerelease || got.MakeLatest != "true" {
		t.Errorf("got release %+v", got)
	}
	if !strings.HasPrefix(got.Body, "## :star2: Highlights\n\nG is here.\n\n## Changelog\n\n") {
		t.Errorf("body doesn't start with the highlights:\n%s", got.Body)
	}

	f.git("pull", "--quiet", "--ff-only")
	f.writeRepo("reel/more.go", "package reel\n\n// G is G.\nfunc G() string { return \"g\" }\n")
	f.commitAndPush("fix(reel): lowercase G again")
	empty := filepath.Join(t.TempDir(), "empty.md")
	f.write(empty, "\n")
	footer := filepath.Join(t.TempDir(), "footer.md")
	f.write(footer, "Thanks for using {{.Repo}} {{.Version}}{{if .Prerelease}}, a prerelease{{end}}.\n")

	f.mustRun(Flags{Bump: bumpMinor, Beta: true, Apply: true, GitHubRelease: true, Highlights: empty, Footer: footer})
	if len(api.releases) != 2 {
		t.Fatalf("created %d releases, want 2", len(api.releases))
	}
	got = api.releases[1]
	if got.TagName != "v0.2.0-beta.0" || !got.Prerelease || got.MakeLatest != "false" {
		t.Errorf("got release %+v", got)
	}
	for _, want := range []string{
		": **reel:** lowercase G again (test)",
		"(https://github.com/lrstanley/fixture/compare/v0.1.0...v0.2.0-beta.0)",
		"\nThanks for using lrstanley/fixture v0.2.0-beta.0, a prerelease.\n",
	} {
		assertContains(t, got.Body, want)
	}
	for _, unwanted := range []string{"## Changelog\n\n### :warning:", "@latest", "What to do next", "chore(release)"} {
		if strings.Contains(got.Body, unwanted) {
			t.Errorf("body has %q:\n%s", unwanted, got.Body)
		}
	}
	if !strings.HasPrefix(got.Body, "## Changelog") {
		t.Errorf("an empty highlights file added something:\n%s", got.Body)
	}
}

func TestGitHubReleaseFailureAfterPush(t *testing.T) {
	f, api := newReleaseFixture(t)
	api.failReleases = true

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true, GitHubRelease: true})
	if !errors.Is(err, errReported) {
		t.Fatalf("got %v, want the release to report the GitHub release it didn't create\n%s", err, out)
	}
	assertContains(t, out, "Released v0.1.0, but the GitHub release wasn't created.")
	m := regexp.MustCompile(`create it with gh release create v0\.1\.0 --repo lrstanley/fixture --verify-tag --title v0\.1\.0 --notes-file (\S+):`).
		FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("doesn't say how to create the release:\n%s", out)
	}
	t.Cleanup(func() { _ = os.Remove(m[1]) })
	saved, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != api.releases[0].Body {
		t.Errorf("saved notes differ from the ones sent:\n%s", saved)
	}
	if got := len(f.remoteTagNames()); got != 3 {
		t.Errorf("got %d tags on the remote, want all 3", got)
	}
}

func TestGitHubReleaseChecks(t *testing.T) {
	f, _ := newReleaseFixture(t)
	failsWith := func(flags Flags, want string) {
		t.Helper()
		out, err := f.run(flags)
		if !errors.Is(err, errReported) {
			t.Fatalf("got %v, want a failed check\n%s", err, out)
		}
		assertContains(t, out, "FAIL "+want)
	}

	missing := filepath.Join(t.TempDir(), "missing.md")
	failsWith(Flags{Bump: "v0.1.0", GitHubRelease: true, Highlights: missing}, "couldn't read --highlights")
	broken := filepath.Join(t.TempDir(), "broken.md")
	f.write(broken, "{{.Repo")
	failsWith(Flags{Bump: "v0.1.0", GitHubRelease: true, Footer: broken}, "parsing "+broken)
	f.write(broken, "{{.Nope}}")
	failsWith(Flags{Bump: "v0.1.0", GitHubRelease: true, Footer: broken}, "rendering "+broken)

	t.Setenv("GH_TOKEN", "")
	failsWith(Flags{Bump: "v0.1.0", GitHubRelease: true}, "can't create the GitHub release: set GH_TOKEN or GITHUB_TOKEN")
	t.Setenv("GITHUB_TOKEN", "wrong")
	failsWith(Flags{Bump: "v0.1.0", GitHubRelease: true}, "couldn't look up lrstanley/fixture on GitHub, to create its release")

	f.github = ""
	failsWith(Flags{Bump: "v0.1.0", GitHubRelease: true}, "--github-release needs origin to be a GitHub repository")
}

func TestGitHubReleaseFlagsNeedIt(t *testing.T) {
	for _, flags := range []Flags{
		{Bump: bumpMinor, Highlights: "h.md"},
		{Bump: bumpMinor, Footer: "f.md"},
	} {
		if err := flags.Validate(); err == nil || !strings.Contains(err.Error(), "needs --github-release") {
			t.Errorf("%+v: got %v, want an error", flags, err)
		}
	}
	if err := (Flags{Bump: bumpMinor, GitHubRelease: true, Highlights: "h.md", Footer: "f.md"}).Validate(); err != nil {
		t.Error(err)
	}
}
