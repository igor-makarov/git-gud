package remote

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

// Download materializes the contents of sourceDirectory into targetDirectory.
// Blob contents are fetched in batches only after the required trees are known.
func (r *Repository) Download(ctx context.Context, sourceDirectory, targetDirectory string) error {
	rootHash, rootPath, err := r.resolveDirectory(ctx, sourceDirectory)
	if err != nil {
		return err
	}
	if targetDirectory == "" {
		return fmt.Errorf("target directory is empty")
	}
	if err := ensureSafeDirectory(targetDirectory); err != nil {
		return err
	}

	type pendingDirectory struct {
		hash plumbing.Hash
		path string
		rel  string
	}
	type pendingFile struct {
		entry Entry
		rel   string
	}
	queue := []pendingDirectory{{hash: rootHash, path: rootPath}}
	files := make([]pendingFile, 0, r.batchSize)

	flushFiles := func() error {
		for len(files) > 0 {
			count := min(r.batchSize, len(files))
			batch := files[:count]
			files = files[count:]
			hashes := make([]plumbing.Hash, 0, len(batch))
			for _, file := range batch {
				if file.entry.Mode == filemode.Submodule {
					return fmt.Errorf("cannot download Git submodule %q", file.entry.Path)
				}
				hashes = append(hashes, file.entry.Hash)
			}
			if err := r.ensureObjects(ctx, hashes, ""); err != nil {
				return err
			}
			for _, file := range batch {
				if err := r.writeEntry(targetDirectory, file.rel, file.entry); err != nil {
					return err
				}
			}
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
		for _, directory := range batch {
			for _, item := range sortedTreeEntries(trees[directory.hash]) {
				if item.Name == "." || item.Name == ".." || strings.ContainsAny(item.Name, "/\x00") {
					return fmt.Errorf("unsafe Git tree name %q", item.Name)
				}
				relative := item.Name
				if directory.rel != "" {
					relative = filepath.Join(directory.rel, item.Name)
				}
				entry := Entry{Path: joinRepoPath(directory.path, item.Name), Mode: item.Mode, Hash: item.Hash}
				if entry.IsDir() {
					if err := ensureDirectoryUnder(targetDirectory, relative); err != nil {
						return err
					}
					queue = append(queue, pendingDirectory{hash: entry.Hash, path: entry.Path, rel: relative})
				} else {
					files = append(files, pendingFile{entry: entry, rel: relative})
				}
			}
		}
		if len(files) >= r.batchSize {
			if err := flushFiles(); err != nil {
				return err
			}
		}
	}
	return flushFiles()
}

func (r *Repository) writeEntry(root, relative string, entry Entry) error {
	destination, err := safeDestination(root, relative)
	if err != nil {
		return err
	}
	if err := ensureDirectoryUnder(root, filepath.Dir(relative)); err != nil {
		return err
	}
	object, err := r.repo.Storer.EncodedObject(plumbing.BlobObject, entry.Hash)
	if err != nil {
		return fmt.Errorf("read blob for %q: %w", entry.Path, err)
	}
	reader, err := object.Reader()
	if err != nil {
		return fmt.Errorf("open blob for %q: %w", entry.Path, err)
	}
	defer reader.Close()

	if entry.Mode == filemode.Symlink {
		contents, err := io.ReadAll(io.LimitReader(reader, 1<<20+1))
		if err != nil {
			return fmt.Errorf("read symlink %q: %w", entry.Path, err)
		}
		if len(contents) > 1<<20 || strings.IndexByte(string(contents), 0) >= 0 {
			return fmt.Errorf("invalid symlink target for %q", entry.Path)
		}
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("replace %q: %w", destination, err)
		}
		if err := os.Symlink(string(contents), destination); err != nil {
			return fmt.Errorf("create symlink %q: %w", destination, err)
		}
		return nil
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".git-gud-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", destination, err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := io.Copy(temporary, reader); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %q: %w", destination, err)
	}
	permissions := os.FileMode(0o644)
	if entry.Mode == filemode.Executable {
		permissions = 0o755
	}
	if err := temporary.Chmod(permissions); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("chmod %q: %w", destination, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %q: %w", destination, err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("install %q: %w", destination, err)
	}
	return nil
}

func ensureSafeDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", directory, err)
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return fmt.Errorf("inspect directory %q: %w", directory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("download path %q is not a real directory", directory)
	}
	return nil
}

func safeDestination(root, relative string) (string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(absoluteRoot, relative)
	rel, err := filepath.Rel(absoluteRoot, destination)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes download directory", relative)
	}
	return destination, nil
}

func ensureDirectoryUnder(root, relative string) error {
	if err := ensureSafeDirectory(root); err != nil {
		return err
	}
	if relative == "" || relative == "." {
		return nil
	}
	destination, err := safeDestination(root, relative)
	if err != nil {
		return err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	relative, err = filepath.Rel(absoluteRoot, destination)
	if err != nil {
		return err
	}
	current := absoluteRoot
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return fmt.Errorf("create directory %q: %w", current, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect directory %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("download path %q is not a real directory", current)
		}
	}
	return nil
}
