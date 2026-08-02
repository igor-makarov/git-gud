# AGENTS.md

## Project scope

- `git-gud` is a CLI-only project. Keep implementation packages under
  `internal/`. The root package exists only to embed `README.md`; do not expand
  it into a general public library API.
- The application uses go-git and must not run a `git` subprocess.
- Preserve safe path handling: reject output directory symlinks and unsafe Git
  tree names, and do not follow symlink directories while materializing files.
- Keep remote snapshots pinned to the commit resolved when a command starts.

## Development environment

- Go 1.26 or newer is required by `go.mod`.
- Tooling is managed by mise. `mise.toml` selects Go and `mise.lock` pins the
  toolchain for macOS ARM64 and Linux x64.
- Install the development toolchain with `mise install`. Shell startup activates
  mise, so invoke project tools directly.

## Validation

Before committing Go changes, run:

```sh
gofmt -w readme.go cmd internal
go test -race ./...
go vet ./...
go build ./cmd/git-gud
git diff --check
```

Add regression tests under `internal/remote` for cache, protocol, traversal, and
filesystem behavior. Keep command parsing tests close to `internal/command` or
`cmd/git-gud` as appropriate.

## Architecture notes

- The root package embeds `README.md`; `cmd/git-gud` owns argument parsing,
  signals, version reporting, and printing the embedded documentation.
- `internal/command` parses subcommand-specific options.
- `internal/remote` owns protocol-v2 communication, the persistent bare cache,
  traversal, matching, and extraction.
- The project uses go-git/v6 alpha because the required protocol-v2 client APIs
  are not available in stable go-git/v5.
- Cache access is protected by a per-repository process lock. The filesystem
  storer therefore uses exclusive access, memory-mapped packs, and in-memory
  pack indexes for read performance.
- Promisor marker files may be read-only after native Git repacking; never
  require write access merely to verify an existing marker.

## Releases

- Release tags are annotated and use `vX.Y.Z`.
- Validate the tree before tagging and push both `main` and the tag. A tag push
  runs `.github/workflows/release.yml` and publishes GoReleaser archives,
  checksums, and attestations to GitHub Releases.
- `.goreleaser.yaml` builds stripped Linux, macOS, and Windows binaries. Keep its
  linker-injected version synchronized with `buildVersion` in `cmd/git-gud`.
- `go install ...@latest` derives the displayed version from Go build metadata;
  no generated version file is needed for source installs.
