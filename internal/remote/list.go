package remote

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
)

// List emits entries in directory. Recursive traversal fetches missing trees in
// batches and never fetches blob contents.
func (r *Repository) List(ctx context.Context, directory string, recursive bool, emit func(Entry) error) error {
	rootHash, rootPath, err := r.resolveDirectory(ctx, directory)
	if err != nil {
		return err
	}

	type pendingDirectory struct {
		hash plumbing.Hash
		path string
	}
	queue := []pendingDirectory{{hash: rootHash, path: rootPath}}
	for len(queue) > 0 {
		count := min(r.batchSize, len(queue))
		batch := queue[:count]
		queue = queue[count:]
		hashes := make([]plumbing.Hash, len(batch))
		for index := range batch {
			hashes[index] = batch[index].hash
		}
		trees, err := r.loadTrees(ctx, hashes)
		if err != nil {
			return err
		}
		for _, directory := range batch {
			for _, item := range sortedTreeEntries(trees[directory.hash]) {
				entry := Entry{Path: joinRepoPath(directory.path, item.Name), Mode: item.Mode, Hash: item.Hash}
				if err := emit(entry); err != nil {
					return fmt.Errorf("emit %q: %w", entry.Path, err)
				}
				if recursive && entry.IsDir() {
					queue = append(queue, pendingDirectory{hash: entry.Hash, path: entry.Path})
				}
			}
		}
		if !recursive {
			break
		}
	}
	return nil
}
