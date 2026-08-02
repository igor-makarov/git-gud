# git-gud

`git gud` lists, finds, and downloads paths from remote Git repositories without
requiring a full clone. It uses Git smart protocol v2 through go-git and never
runs a `git` subprocess.

## Install

Prebuilt archives for Linux, macOS, and Windows are available from
[GitHub Releases](https://github.com/igor-makarov/git-gud/releases).

Install from source with Go 1.26 or newer:

```sh
go install github.com/igor-makarov/git-gud/cmd/git-gud@latest
```

Ensure Go's binary directory is in `PATH`:

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Git discovers the installed `git-gud` executable as the `git gud` subcommand.
The complete documentation is also embedded in the executable:

```sh
git gud --readme
```

## Usage

```text
git gud [GLOBAL FLAGS] REPOSITORY[@REF] ls [-R|--recursive] [DIR]
git gud [GLOBAL FLAGS] REPOSITORY[@REF] find [--from DIR] GLOB
git gud [GLOBAL FLAGS] REPOSITORY[@REF] download [-o|--output DIR] [--jobs N] DIR
```

Global flags must precede `REPOSITORY`. Use `--` after command options when a
positional argument begins with `-`.

`REPOSITORY` must be an HTTP(S) smart Git URL. URL userinfo may provide
credentials; userinfo is removed before deriving the persistent cache key and
is not written to the cached remote configuration.

`REF` defaults to the remote's default branch. It may be a branch, tag, full
`refs/...` name, or full object ID. Quote the argument when needed by the shell:

```sh
git gud 'https://github.com/owner/repo.git@feature/fast' ls src
```

### List

List the direct children of a directory without downloading their blobs. `DIR`
defaults to the repository root (`.`), and non-recursive output contains
basenames:

```sh
git gud https://github.com/owner/repo.git ls src
```

Recursively list repository-relative paths:

```sh
git gud https://github.com/CocoaPods/Specs.git ls -R Specs
```

Both files and directories are listed.

### Find

Find files and directories with doublestar patterns. Supported syntax includes
`*`, `?`, character classes, and `**` for any number of path components. Quote
patterns to prevent the shell from expanding them:

```sh
git gud https://github.com/CocoaPods/Specs.git find 'Specs/*/*/*/*'
git gud https://github.com/owner/repo.git find '**/*.json'
git gud https://github.com/owner/repo.git find --from assets '**/icon-?.svg'
```

`--from` defaults to the repository root and resolves the scope before matching,
so unrelated trees are not requested. Patterns are interpreted relative to the
scope, while matches are printed as paths from the repository root.

### Download

Recursively download a directory's contents into the current directory:

```sh
git gud https://github.com/owner/repo.git download assets
```

Choose a destination and set the bounded extraction concurrency with `--jobs`
(default: 8):

```sh
git gud https://github.com/owner/repo.git download \
  --output ./vendor/assets \
  --jobs 8 \
  assets
```

The source directory itself is not created in the destination; its contents are
placed directly in the output directory. Fresh files are written directly and
existing files are replaced atomically. Regular files, Git executable modes,
and symbolic links are preserved. Existing output directories must be real
directories rather than symbolic links. Git submodules are rejected.

Download overlays the destination. It does not remove destination entries that
are absent from the selected Git snapshot, so reusing an output directory is
not an exact mirroring operation.

## Global flags

```text
--cache-dir DIR   Bare Git cache (default: OS user cache directory/git-gud)
--batch-size N    Maximum object wants per fetch (default: 4096)
--progress        Show remote Git sideband progress
--version         Show the installed module version
--readme          Print this README
```

Set `GIT_GUD_CACHE_DIR` to configure the cache without a flag. A command-line
`--cache-dir` value takes precedence.

## Cache and fetch behavior

Each remote URL, with userinfo removed, maps to a standard bare repository:

```text
$CACHE_DIR/repos/<sha256-of-url>.git
```

The cache uses normal Git objects, refs, packs, shallow metadata, and promisor
pack markers. It can be inspected and maintained with ordinary Git tooling.
A per-repository process lock prevents concurrent git-gud commands from
modifying the same cache.

For every command, git-gud:

1. negotiates smart protocol v2 and resolves the requested ref;
2. fetches a missing commit at depth one with `filter tree:0`;
3. lazily fetches only required tree object IDs in bounded batches;
4. fetches no blobs for `ls` or `find`;
5. fetches and retains required blobs for `download`.

A scoped path or fixed-prefix glob therefore avoids unrelated subtrees. Broad
patterns such as `**` necessarily inspect all trees under their scope. Cached
objects are retained for later commands.

## Requirements and limitations

- The remote must support HTTP(S) Git smart protocol v2, shallow fetches, and
  object filters.
- SSH and local filesystem remotes are not supported.
- A full object ID must be accepted by the server as a reachable request.
- SHA-1 and SHA-256 object IDs are parsed; practical SHA-256 support also
  depends on the remote and go-git storage support.
