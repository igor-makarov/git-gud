package remote

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

// DefaultDownloadJobs bounds concurrent blob extraction and file writes.
const DefaultDownloadJobs = 8

// Download materializes the contents of sourceDirectory into targetDirectory.
// Blob contents are fetched in batches and extracted by a bounded worker pool.
func (r *Repository) Download(ctx context.Context, sourceDirectory, targetDirectory string, jobs int) (result error) {
	rootHash, rootPath, err := r.resolveDirectory(ctx, sourceDirectory)
	if err != nil {
		return err
	}
	if targetDirectory == "" {
		return fmt.Errorf("target directory is empty")
	}
	targetRoot, err := filepath.Abs(targetDirectory)
	if err != nil {
		return fmt.Errorf("resolve target directory: %w", err)
	}
	if err := ensureSafeDirectory(targetRoot); err != nil {
		return err
	}
	if jobs <= 0 {
		jobs = DefaultDownloadJobs
	}

	type pendingDirectory struct {
		hash        plumbing.Hash
		path        string
		destination string
	}
	type pendingFile struct {
		entry       Entry
		destination string
	}

	workCtx, cancel := context.WithCancel(ctx)
	tasks := make(chan pendingFile, jobs*4)
	var workers sync.WaitGroup
	var errorMu sync.Mutex
	var workerError error
	recordError := func(err error) {
		errorMu.Lock()
		if workerError == nil {
			workerError = err
			cancel()
		}
		errorMu.Unlock()
	}
	getWorkerError := func() error {
		errorMu.Lock()
		defer errorMu.Unlock()
		return workerError
	}
	for range jobs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for file := range tasks {
				if workCtx.Err() != nil {
					continue
				}
				if err := r.writeEntry(file.destination, file.entry); err != nil {
					recordError(err)
				}
			}
		}()
	}
	defer func() {
		if result != nil {
			cancel()
		}
		close(tasks)
		workers.Wait()
		cancel()
		if err := getWorkerError(); result == nil && err != nil {
			result = err
		}
	}()

	queue := []pendingDirectory{{
		hash:        rootHash,
		path:        rootPath,
		destination: targetRoot,
	}}
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
			if err := r.ensureObjects(workCtx, hashes, ""); err != nil {
				if workerErr := getWorkerError(); workerErr != nil {
					return workerErr
				}
				return err
			}
			for _, file := range batch {
				select {
				case tasks <- file:
				case <-workCtx.Done():
					if workerErr := getWorkerError(); workerErr != nil {
						return workerErr
					}
					return workCtx.Err()
				}
			}
		}
		files = nil
		return nil
	}

	for len(queue) > 0 {
		if err := workCtx.Err(); err != nil {
			if workerErr := getWorkerError(); workerErr != nil {
				return workerErr
			}
			return err
		}
		count := min(r.batchSize, len(queue))
		batch := queue[:count]
		queue = queue[count:]
		hashes := make([]plumbing.Hash, len(batch))
		for index := range batch {
			hashes[index] = batch[index].hash
		}
		trees, err := r.loadTrees(workCtx, hashes)
		if err != nil {
			return err
		}
		for _, directory := range batch {
			for _, item := range sortedTreeEntries(trees[directory.hash]) {
				if item.Name == "." || item.Name == ".." || strings.ContainsAny(item.Name, "/\x00") {
					return fmt.Errorf("unsafe Git tree name %q", item.Name)
				}
				destination := filepath.Join(directory.destination, item.Name)
				entry := Entry{Path: joinRepoPath(directory.path, item.Name), Mode: item.Mode, Hash: item.Hash}
				if entry.IsDir() {
					if err := ensureChildDirectory(destination); err != nil {
						return err
					}
					queue = append(queue, pendingDirectory{
						hash:        entry.Hash,
						path:        entry.Path,
						destination: destination,
					})
				} else {
					files = append(files, pendingFile{entry: entry, destination: destination})
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

func (r *Repository) writeEntry(destination string, entry Entry) error {
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

	permissions := os.FileMode(0o644)
	if entry.Mode == filemode.Executable {
		permissions = 0o755
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
	if err == nil {
		if _, err := io.Copy(file, reader); err != nil {
			_ = file.Close()
			_ = os.Remove(destination)
			return fmt.Errorf("write %q: %w", destination, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(destination)
			return fmt.Errorf("close %q: %w", destination, err)
		}
		return nil
	}
	if !os.IsExist(err) {
		return fmt.Errorf("create %q: %w", destination, err)
	}
	return writeAtomicEntry(destination, permissions, reader)
}

func writeAtomicEntry(destination string, permissions os.FileMode, reader io.Reader) error {
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

func ensureChildDirectory(directory string) error {
	if err := os.Mkdir(directory, 0o755); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return fmt.Errorf("create directory %q: %w", directory, err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect directory %q: %w", directory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("download path %q is not a real directory", directory)
	}
	return nil
}
