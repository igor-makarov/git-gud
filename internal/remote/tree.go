package remote

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

// Entry describes one path at the selected commit.
type Entry struct {
	Path string
	Mode filemode.FileMode
	Hash plumbing.Hash
}

func (entry Entry) IsDir() bool { return entry.Mode == filemode.Dir }

func normalizeRepoPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." || value == "/" {
		return "", nil
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." {
			return "", fmt.Errorf("path must not contain ..")
		}
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+value), "/")
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

func (r *Repository) loadTrees(ctx context.Context, hashes []plumbing.Hash) (map[plumbing.Hash]*object.Tree, error) {
	if err := r.ensureObjects(ctx, hashes, packp.FilterTreeDepth(0)); err != nil {
		return nil, err
	}
	trees := make(map[plumbing.Hash]*object.Tree, len(hashes))
	for _, hash := range hashes {
		if _, exists := trees[hash]; exists {
			continue
		}
		tree, err := object.GetTree(r.repo.Storer, hash)
		if err != nil {
			return nil, fmt.Errorf("decode tree %s: %w", hash, err)
		}
		trees[hash] = tree
	}
	return trees, nil
}

func (r *Repository) resolveDirectory(ctx context.Context, value string) (plumbing.Hash, string, error) {
	cleaned, err := normalizeRepoPath(value)
	if err != nil {
		return plumbing.ZeroHash, "", err
	}
	if cleaned == "" {
		return r.rootHash, "", nil
	}

	hash := r.rootHash
	for _, component := range strings.Split(cleaned, "/") {
		trees, err := r.loadTrees(ctx, []plumbing.Hash{hash})
		if err != nil {
			return plumbing.ZeroHash, "", err
		}
		var found *object.TreeEntry
		for index := range trees[hash].Entries {
			entry := &trees[hash].Entries[index]
			if entry.Name == component {
				found = entry
				break
			}
		}
		if found == nil || found.Mode != filemode.Dir {
			return plumbing.ZeroHash, "", fmt.Errorf("directory %q not found", cleaned)
		}
		hash = found.Hash
	}
	return hash, cleaned, nil
}

func sortedTreeEntries(tree *object.Tree) []object.TreeEntry {
	entries := append([]object.TreeEntry(nil), tree.Entries...)
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name < entries[right].Name
	})
	return entries
}

func joinRepoPath(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}
