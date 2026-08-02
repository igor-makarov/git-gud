# git-gud

`git gud` is a CLI client for listing, finding, and downloading paths from remote
Git repositories without cloning their complete history or blobs. It talks to
HTTP(S) remotes with Git smart protocol v2 and uses go-git—never a `git`
subprocess.

## Install

Install the latest release with Go 1.26 or newer:

```sh
go install github.com/igor-makarov/git-gud/cmd/git-gud@latest
```

Ensure Go's binary directory is in `PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Git discovers the installed `git-gud` executable as the `git gud` subcommand.

For development from a checkout, mise resolves `go = "latest"`; `mise.lock`
locks the exact toolchain for macOS ARM64 and Linux x64:

```sh
mise install
mise exec -- go test ./...
mise exec -- go build ./cmd/git-gud
```

## Usage

```text
git gud [GLOBAL FLAGS] REPOSITORY[@REF] ls [-R|--recursive] [DIR]
git gud [GLOBAL FLAGS] REPOSITORY[@REF] find [--from DIR] GLOB
git gud [GLOBAL FLAGS] REPOSITORY[@REF] download [-o|--output DIR] [--jobs N] DIR
```

`REF` defaults to `HEAD` of the remote's default branch. It may be a branch,
tag, full `refs/...` name, or full object ID. Branch names containing `/` work:

```sh
git gud 'https://github.com/owner/repo.git@feature/fast' ls src
```

### List

List one directory without downloading its blobs:

```sh
git gud https://github.com/owner/repo.git ls src
```

Recursively list repository-relative paths:

```sh
git gud https://github.com/owner/repo.git ls -R Specs
```

### Find

Find supports doublestar syntax (`*`, `?`, character classes, and `**`):

```sh
git gud https://github.com/CocoaPods/Specs.git find 'Specs/*/*/*/*'
git gud https://github.com/owner/repo.git find '**/*.json'
git gud https://github.com/owner/repo.git find --from assets '**/icon-?.svg'
```

`--from` resolves the scope first, so unrelated trees are never requested.
Matches include both files and directories and are printed as paths from the
repository root.

### Download

Download a directory's contents recursively into the current directory:

```sh
git gud https://github.com/owner/repo.git download assets
```

Choose an exact destination directory with `-o` and control concurrent file
extraction with `--jobs` (default: 8):

```sh
git gud https://github.com/owner/repo.git download -o ./vendor/assets --jobs 8 assets
```

Fresh files are written directly; existing files are replaced atomically.
Regular files, executable modes, and symbolic links are preserved. Git
submodules are rejected rather than treated as files.

## Global flags

```text
--cache-dir DIR   Bare Git cache (default: the OS user cache directory)
--batch-size N    Maximum object wants per fetch (default: 4096)
--progress        Show Git sideband progress
--version         Show version
```

Set `GIT_GUD_CACHE_DIR` to configure the cache without a flag.

## Git-native cache and fetch strategy

Each credential-free remote URL maps to a standard bare repository under:

```text
$CACHE_DIR/repos/<sha256-of-url>.git
```

The cache uses normal Git objects, refs, packs, shallow metadata, and promisor
pack markers. `remote.origin.promisor=true` and
`remote.origin.partialclonefilter=tree:0` make the intentionally missing
objects valid to ordinary Git tooling. Credentials from URL userinfo are used
for transport but are not written to the cache config.

For every command, git-gud:

1. negotiates smart protocol v2 and resolves only the requested ref with
   `ls-refs` prefixes;
2. fetches a new commit at depth one with `filter tree:0`;
3. lazily fetches only needed tree object IDs, batching up to `--batch-size`
   wants and sending the shallow commit boundary;
4. fetches no blobs for `ls` or `find`;
5. batches and streams only selected blobs for `download`.

A scoped or fixed-prefix glob therefore avoids unrelated subtrees. Broad globs
such as `**` necessarily inspect every matching tree, but packs are streamed to
disk and retained for later commands. A per-repository process lock protects
cache updates.

## Requirements and limitations

- The remote must support HTTP(S) smart protocol v2, shallow fetches, and object
  filters.
- SHA-1 and SHA-256 object IDs are accepted; server and storage support still
  depend on go-git.
- The project currently uses `go-git/v6` alpha because protocol-v2 client
  support is not available in the stable v5 API.
