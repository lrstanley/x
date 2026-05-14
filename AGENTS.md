# AGENTS.md

## Cursor Cloud specific instructions

### Overview

This is a Go library monorepo (`github.com/lrstanley/x`) containing 13 independent
Go modules. There are no runnable services, databases, or containers — only library
packages with tests.

### Prerequisites (installed by the update script)

- **Go >= 1.26.0** at `/usr/local/go/bin/go`
- **task** (Taskfile runner) at `~/go/bin/task`
- **golangci-lint** at `~/go/bin/golangci-lint`
- **gofumpt** at `~/go/bin/gofumpt`

PATH must include `/usr/local/go/bin` and `~/go/bin`.

### Key commands

All workflows are defined in the root `Taskfile.yaml`:

| Action | Command |
|--------|---------|
| Sync workspace | `go work init && task sync:all` |
| Run all tests | `task test:all` |
| Run linting | `task lint:all` |
| Run formatting | `task fmt:all` |
| Tidy modules | `task tidy:all` |
| Run single-pkg test | `cd <pkg> && go test -v ./...` |
| Run single-pkg lint | `cd <pkg> && golangci-lint run` |

### Important caveats

- `go.work` is `.gitignore`'d — you must run `go work init && task sync:all` before
  any cross-module work. The update script handles this.
- `task test:all` runs `prepare:all` as a dependency, which calls `license:all` and
  `fmt:all`. These modify source files (add license headers, reformat). Run
  `git checkout -- .` afterwards if you don't want those changes committed.
- `task lint:all` also depends on `fmt:all` and `license:all` — same caveat applies.
- To run tests or lint without the formatting/license side effects, use direct
  `go test` or `golangci-lint run` in individual package directories.
- The repo's CONTRIBUTING.md states: **AI Agents MUST NOT add `Signed-off-by` tags
  to commits.** Only humans can certify the DCO.
