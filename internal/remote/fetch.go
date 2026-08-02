package remote

import (
	"context"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
)

type fetchResponse struct {
	storage  storage.Storer
	progress io.Writer
}

func (response *fetchResponse) Decode(reader io.Reader) error {
	output := &packp.FetchOutput{}
	if err := output.Decode(reader); err != nil {
		return err
	}
	if !output.Packfile {
		return fmt.Errorf("remote returned no packfile")
	}

	demuxer := sideband.NewDemuxer(sideband.Sideband64k, reader)
	if response.progress != nil {
		demuxer.Progress = response.progress
	}
	if err := packfile.UpdateObjectStorage(response.storage, demuxer); err != nil {
		return fmt.Errorf("store fetched pack: %w", err)
	}
	return nil
}

func (r *Repository) ensureObjects(ctx context.Context, hashes []plumbing.Hash, filter packp.Filter) error {
	missing := make([]plumbing.Hash, 0, len(hashes))
	seen := make(map[plumbing.Hash]struct{}, len(hashes))
	for _, hash := range hashes {
		if hash.IsZero() {
			continue
		}
		if _, duplicate := seen[hash]; duplicate {
			continue
		}
		seen[hash] = struct{}{}
		if err := r.repo.Storer.HasEncodedObject(hash); err != nil {
			missing = append(missing, hash)
		}
	}

	for start := 0; start < len(missing); start += r.batchSize {
		end := min(start+r.batchSize, len(missing))
		arguments := &packp.FetchArgs{
			Wants:      missing[start:end],
			Done:       true,
			OFSDelta:   true,
			NoProgress: r.progress == nil,
			Shallows:   []plumbing.Hash{r.commitHash},
			Filter:     filter,
		}
		response := &fetchResponse{storage: r.repo.Storer, progress: r.progress}
		if err := r.commander.Command(ctx, "fetch", arguments, response); err != nil {
			return fmt.Errorf("fetch %d Git objects: %w", end-start, err)
		}
		if err := markPromisorPacks(r.cachePath); err != nil {
			return err
		}
	}
	return nil
}
