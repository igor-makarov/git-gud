package remote

import (
	"context"
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/go-git/go-git/v6/plumbing"
)

// Find emits files and directories matching pattern below scope. Patterns use
// doublestar syntax: *, ?, character classes, and ** for any number of path
// components. Returned paths are rooted at the repository, not at scope.
func (r *Repository) Find(ctx context.Context, scope, pattern string, emit func(Entry) error) error {
	rootHash, rootPath, err := r.resolveDirectory(ctx, scope)
	if err != nil {
		return err
	}
	pattern = strings.Trim(pattern, "/")
	if pattern == "" || pattern == "." {
		return fmt.Errorf("glob pattern is empty")
	}
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid glob pattern %q", pattern)
	}
	parts := strings.Split(pattern, "/")

	type state struct {
		hash plumbing.Hash
		path string
		part int
	}
	stateKey := func(value state) string {
		return fmt.Sprintf("%s\x00%d", value.path, value.part)
	}
	queue := []state{{hash: rootHash, path: rootPath}}
	seenStates := map[string]struct{}{stateKey(queue[0]): {}}
	emitted := make(map[string]struct{})
	enqueue := func(value state) {
		key := stateKey(value)
		if _, seen := seenStates[key]; seen {
			return
		}
		seenStates[key] = struct{}{}
		queue = append(queue, value)
	}
	emitOnce := func(entry Entry) error {
		if _, seen := emitted[entry.Path]; seen {
			return nil
		}
		emitted[entry.Path] = struct{}{}
		if err := emit(entry); err != nil {
			return fmt.Errorf("emit %q: %w", entry.Path, err)
		}
		return nil
	}

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

		for _, current := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			segment := parts[current.part]
			entries := sortedTreeEntries(trees[current.hash])
			if segment == "**" {
				if current.part+1 < len(parts) {
					enqueue(state{hash: current.hash, path: current.path, part: current.part + 1})
				}
				for _, item := range entries {
					if err := validateTreeName(item.Name); err != nil {
						return err
					}
					entry := Entry{Path: joinRepoPath(current.path, item.Name), Mode: item.Mode, Hash: item.Hash}
					if current.part+1 == len(parts) {
						if err := emitOnce(entry); err != nil {
							return err
						}
					}
					if entry.IsDir() {
						enqueue(state{hash: entry.Hash, path: entry.Path, part: current.part})
					}
				}
				continue
			}

			for _, item := range entries {
				if err := validateTreeName(item.Name); err != nil {
					return err
				}
				matches, err := doublestar.Match(segment, item.Name)
				if err != nil {
					return fmt.Errorf("match glob segment %q: %w", segment, err)
				}
				if !matches {
					continue
				}
				entry := Entry{Path: joinRepoPath(current.path, item.Name), Mode: item.Mode, Hash: item.Hash}
				if current.part+1 == len(parts) {
					if err := emitOnce(entry); err != nil {
						return err
					}
				} else if entry.IsDir() {
					enqueue(state{hash: entry.Hash, path: entry.Path, part: current.part + 1})
				}
			}
		}
	}
	return nil
}
