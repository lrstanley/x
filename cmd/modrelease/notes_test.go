// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"text/template"
)

func TestChangeGroup(t *testing.T) {
	for _, tt := range []struct {
		subject, body string
		group         int
		scope, text   string
	}{
		{subject: "feat(sync/cache): add GetOrLoad", group: groupFeatures, scope: "sync/cache", text: "add GetOrLoad"},
		{subject: "feat!: drop Go 1.25", group: groupBreaking, text: "drop Go 1.25"},
		{subject: "fix(api): rename field", body: "BREAKING CHANGE: Field is now Name.", group: groupBreaking, scope: "api", text: "rename field"},
		{subject: "fix(db): a bug", group: groupFixes, scope: "db", text: "a bug"},
		{subject: "fix: patch CVE-2026-1234 in parser", group: groupSecurity},
		{subject: "fix(security): escape output", group: groupSecurity},
		{subject: "security: escape output", group: groupSecurity},
		{subject: "fix(deps): update module golang.org/x/mod to v0.43.0", group: groupDeps},
		{subject: "chore(deps,ci): bump everything", group: groupDeps},
		{subject: "build(deps-dev): bump vite from 6.0.0 to 6.1.0", group: groupDeps},
		{subject: "Bump golang.org/x/net from 0.1.0 to 0.2.0", group: groupDeps},
		{subject: "perf(tail): skip a copy", group: groupPerf},
		{subject: "docs: explain the plan", group: groupDocs},
		{subject: "chore: tidy", group: groupMisc},
		{subject: "ci: test on windows", group: groupMisc},
		{subject: "refactor(bloom): split the engine", group: groupMisc},
		{subject: "Add a thing", group: groupFeatures, text: "Add a thing"},
		{subject: "fixes the build on windows", group: groupFixes},
		{subject: "Update the readme", group: groupMisc},
		{subject: "FEAT(X): shouting", group: groupFeatures, scope: "X", text: "shouting"},
	} {
		c := parseChange("abc", "n", "e", tt.subject, tt.body)
		if got := c.group(); got != tt.group {
			t.Errorf("%q: group %s, want %s", tt.subject, groupTitles[got], groupTitles[tt.group])
		}
		if tt.text != "" && (c.scope != tt.scope || c.text != tt.text) {
			t.Errorf("%q: scope %q, text %q, want %q, %q", tt.subject, c.scope, c.text, tt.scope, tt.text)
		}
	}
}

func TestFilterChanges(t *testing.T) {
	changes := []*change{
		parseChange("1111111111", "", "", "feat: kept", ""),
		parseChange("2222222222", "", "", "feat: reverted", ""),
		parseChange("3333333333", "", "", `Revert "feat: reverted"`, "This reverts commit 2222222.\n"),
		parseChange("4444444444", "", "", `revert: older change`, "This reverts commit 9999999999.\n"),
		parseChange("5555555555", "", "", "chore(readme): auto-update [skip ci]", ""),
		parseChange("6666666666", "", "", `chore(terraform): auto-applied ".github/CONTRIBUTING.md"`, ""),
		parseChange("7777777777", "", "", "chore(release): v1.2.3", ""),
		parseChange("8888888888", "", "", "docs: fix a typo", ""),
		parseChange("9999999990", "", "", "feat: support typo tolerance", ""),
	}
	var got []string
	for _, c := range filterChanges(changes) {
		got = append(got, c.sha[:1])
	}
	if want := "1,4,9"; strings.Join(got, ",") != want {
		t.Errorf("kept %v, want %s", got, want)
	}
}

func TestPreviousVersion(t *testing.T) {
	tags := []string{"v1.0.0", "v1.1.0", "v1.2.0-beta.0", "v1.2.0-beta.1", "reel/v1.1.5", "v2.0.0-rc.0", "not-a-version"}
	for version, want := range map[string]string{
		"v1.2.0-beta.2": "v1.2.0-beta.1",
		"v1.2.0-beta.0": "v1.1.0",
		"v1.2.0":        "v1.1.0",
		"v1.1.1":        "v1.1.0",
		"v2.0.0":        "v1.1.0",
		"v2.0.0-rc.1":   "v2.0.0-rc.0",
		"v0.1.0":        "",
	} {
		if got := previousVersion(tags, version); got != want {
			t.Errorf("previousVersion(%s) = %q, want %q", version, got, want)
		}
	}
}

func TestBreakingNote(t *testing.T) {
	for body, want := range map[string]string{
		"BREAKING CHANGE: one line.\n\nSigned-off-by: A <a@b>":                      "one line.",
		"Intro.\n\nBREAKING CHANGE: wraps\n  onto two.\nRefs: #12\n":                "wraps onto two.",
		"BREAKING-CHANGE: dashed\nSigned-off-by: A <a@b>":                           "dashed",
		"BREAKING CHANGE:\nOn the next line.\n\nMore paragraphs aren't part of it.": "On the next line.",
	} {
		c := parseChange("abc", "", "", "fix: x", body)
		if !c.breaking || c.breakingNote != want {
			t.Errorf("%q: breaking %v, note %q, want %q", body, c.breaking, c.breakingNote, want)
		}
	}
}

func TestChangeMarkdown(t *testing.T) {
	c := parseChange("abc123", "Some One", "1+someone@users.noreply.github.com", "feat(api)!: rename Field", "BREAKING CHANGE: Field is now Name.")
	c.login = "someone"
	if got, want := c.markdown(), "* abc123: **api:** rename Field (@someone)\n  * Field is now Name."; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	c = parseChange("abc123", "Bot", "", "chore: x", "")
	c.login = "dependabot[bot]"
	if got, want := c.markdown(), "* abc123: x (@dependabot)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := parseChange("abc123", "Some One", "", "tidy", "").markdown(); got != "* abc123: tidy (Some One)" {
		t.Errorf("got %q", got)
	}
}

func TestNoreply(t *testing.T) {
	for email, want := range map[string]string{
		"12345+lrstanley@users.noreply.github.com": "lrstanley",
		"lrstanley@users.noreply.github.com":       "lrstanley",
		"liam@example.com":                         "",
		"-bad@users.noreply.github.com":            "",
	} {
		var got string
		if m := noreplyRE.FindStringSubmatch(email); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", email, got, want)
		}
	}
}

func TestRenderNotes(t *testing.T) {
	n := &notes{
		repo:       "lrstanley/x",
		version:    "v1.2.0",
		previous:   "v1.1.0",
		highlights: "\n## Highlights\n\nIt's faster.\n\n",
		changes: []*change{
			parseChange("s1", "A", "", "chore: tidy", ""),
			parseChange("s2", "A", "", "feat(sync): add Group", ""),
			parseChange("s3", "A", "", "feat: add a root thing", ""),
			parseChange("s4", "A", "", "fix(http): close bodies", ""),
			parseChange("s5", "A", "", "feat(http): add retries", ""),
		},
		modules: []releaseModule{{path: "github.com/lrstanley/x"}, {path: "github.com/lrstanley/x/cmd/tool", command: true}},
		footer:  "## Footer\n",
	}
	got := n.render()
	want := "## Highlights\n\nIt's faster.\n\n" +
		"## Changelog\n\n" +
		"### :sparkles: New Features\n\n" +
		"* s3: add a root thing (A)\n" +
		"* s5: **http:** add retries (A)\n" +
		"* s2: **sync:** add Group (A)\n\n" +
		"### :lady_beetle: Bug Fixes\n\n" +
		"* s4: **http:** close bodies (A)\n\n" +
		"### :broom: Misc\n\n" +
		"<details>\n<summary>1 change</summary>\n\n" +
		"* s1: tidy (A)\n" +
		"\n</details>\n\n" +
		"**Full changelog**: [`v1.1.0...v1.2.0`](https://github.com/lrstanley/x/compare/v1.1.0...v1.2.0)\n\n" +
		"## :package: Install\n\n" +
		"```sh\ngo get github.com/lrstanley/x@v1.2.0\ngo install github.com/lrstanley/x/cmd/tool@v1.2.0\n```\n\n" +
		"**Docs**:\n\n" +
		"* [github.com/lrstanley/x](https://pkg.go.dev/github.com/lrstanley/x@v1.2.0)\n" +
		"* [github.com/lrstanley/x/cmd/tool](https://pkg.go.dev/github.com/lrstanley/x/cmd/tool@v1.2.0)\n\n" +
		"## Footer\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderPrereleaseNotes(t *testing.T) {
	n := &notes{repo: "o/r", version: "v1.0.0-rc.0"}
	for range 6 {
		n.modules = append(n.modules, releaseModule{path: fmt.Sprintf("example.com/m%d", len(n.modules))})
	}
	got := n.render()
	for _, want := range []string{
		"No changes besides updated requirements.",
		"<details>\n<summary>All 6 modules</summary>\n\n",
		"\ngo get example.com/m5@v1.0.0-rc.0\n",
		"</details>",
	} {
		assertContains(t, got, want)
	}
	for _, unwanted := range []string{"@latest", "Full changelog"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a first prerelease's notes have %q:\n%s", unwanted, got)
		}
	}
}

func TestInstallOneModule(t *testing.T) {
	n := &notes{repo: "o/r", version: "v0.0.0-alpha.3", modules: []releaseModule{{path: "example.com/m"}}}
	got := n.install()
	want := "## :package: Install\n\n" +
		"```sh\ngo get example.com/m@v0.0.0-alpha.3\n```\n\n" +
		"**Docs**: [example.com/m](https://pkg.go.dev/example.com/m@v0.0.0-alpha.3)\n\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTruncates(t *testing.T) {
	n := &notes{repo: "o/r", version: "v1.0.0", previous: "v0.9.0"}
	for i := range 5000 {
		n.changes = append(n.changes, parseChange(fmt.Sprintf("%040d", i), "Someone", "", "feat: change number "+strconv.Itoa(i), ""))
	}
	got := n.render()
	if len(got) > maxNotesLength {
		t.Errorf("notes are %d characters, more than %d", len(got), maxNotesLength)
	}
	assertContains(t, got, "more changes don't fit here, so see the full changelog.")
	assertContains(t, got, "**Full changelog**")
}

func TestRenderTruncatesCollapsedGroup(t *testing.T) {
	n := &notes{repo: "o/r", version: "v1.0.0", previous: "v0.9.0"}
	for i := range 5000 {
		n.changes = append(n.changes, parseChange(fmt.Sprintf("%040d", i), "Someone", "", "chore: tidy number "+strconv.Itoa(i), ""))
	}
	got := n.render()
	if len(got) > maxNotesLength {
		t.Errorf("notes are %d characters, more than %d", len(got), maxNotesLength)
	}
	assertContains(t, got, "### :broom: Misc\n\n<details>\n<summary>5000 changes</summary>\n\n")
	assertContains(t, got, "\n</details>\n\n_")
	assertContains(t, got, "more changes don't fit here, so see the full changelog.")
}

func TestDefaultFooter(t *testing.T) {
	render := func(files ...string) string {
		t.Helper()
		d := &footerData{Repo: "o/r", URL: "https://github.com/o/r", Branch: "main"}
		d.setCommunityFiles(files)
		var b strings.Builder
		if err := template.Must(template.New("").Option("missingkey=error").Parse(defaultFooter)).Execute(&b, d); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}

	got := render("LICENSE", ".github/CONTRIBUTING.md", "CONTRIBUTING.md", "docs/SUPPORT.md", ".github/SECURITY.md", "sub/CODE_OF_CONDUCT.md")
	for _, want := range []string{
		"[Take a look here](https://github.com/o/r/issues/new/choose)",
		"[Security and Disclosure](https://github.com/o/r/security/policy)",
		"  * [License](https://github.com/o/r/blob/main/LICENSE)\n",
		"  * [Contributing](https://github.com/o/r/blob/main/.github/CONTRIBUTING.md)\n",
		"  * [Support](https://github.com/o/r/blob/main/docs/SUPPORT.md)",
	} {
		assertContains(t, got, want)
	}
	if strings.Contains(got, "Code of Conduct") {
		t.Errorf("linked a code of conduct GitHub wouldn't find:\n%s", got)
	}

	got = render()
	for _, unwanted := range []string{"Security", "Other useful links"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("footer without community files has %q:\n%s", unwanted, got)
		}
	}
}
