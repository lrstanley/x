# modrelease

`modrelease` releases every Go module in a repository at one version. It works out
the release order from each `go.mod`, rehearses the whole release against the
versions about to be published, then publishes one signed commit and all of its
signed tags in a single atomic push.

```console
go run github.com/lrstanley/x/cmd/modrelease@latest minor          # print the plan
go run github.com/lrstanley/x/cmd/modrelease@latest minor --apply  # publish it
```

Nothing changes without `--apply`. A clean plan means the release was already
tidied, built, and tested as a consumer would get it, with no workspace.

## Versions

Pass `major`, `minor`, `patch`, or an explicit version. Add `--alpha`, `--beta`, or
`--rc` to a bump to release it as a prerelease:

| Current          | Command        | Result           |
| ---------------- | -------------- | ---------------- |
| `v1.2.0`         | `minor --beta` | `v1.3.0-beta.0`  |
| `v1.3.0-beta.0`  | `minor --beta` | `v1.3.0-beta.1`  |
| `v1.3.0-beta.1`  | `minor --rc`   | `v1.3.0-rc.0`    |
| `v1.3.0-rc.0`    | `minor`        | `v1.3.0`         |
| `v1.3.0`         | `patch --rc`   | `v1.3.1-rc.0`    |

The current version is the highest `vX.Y.Z` tag on the remote. A repository with no
tags needs an explicit version. Crossing into `/v2` or higher needs its module paths
and imports changed first, and `modrelease` stops with an explanation until they are.

## Modules

Every module tracked by git is released, except those in a directory starting with
`_` or `.`, those in `testdata`, and those named by `--skip` or `$MODRELEASE_SKIP`.
All of them get a tag, even if only their requirements changed.

## LFS

Module hashes are computed the way the public proxy computes them, so they're right
even with a local LFS filter. If you fetch these modules directly with `GOPRIVATE` or
`GOPROXY=direct` and an LFS filter active, Go computes a different hash and reports a
checksum mismatch. Set `GIT_LFS_SKIP_SMUDGE=1`, or use `GONOSUMDB` instead.

Run `modrelease --help` for every flag.
