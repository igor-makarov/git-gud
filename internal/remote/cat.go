package remote

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

// Cat writes a file from the selected commit to writer.
func (r *Repository) Cat(ctx context.Context, source string, writer io.Writer) error {
	entry, err := r.resolvePath(ctx, source)
	if err != nil {
		return err
	}
	if entry.IsDir() {
		return fmt.Errorf("cannot cat directory %q", entry.Path)
	}
	if entry.Mode == filemode.Submodule {
		return fmt.Errorf("cannot cat Git submodule %q", entry.Path)
	}
	if err := r.ensureObjects(ctx, []plumbing.Hash{entry.Hash}, ""); err != nil {
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
	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("write %q to stdout: %w", entry.Path, err)
	}
	return nil
}
