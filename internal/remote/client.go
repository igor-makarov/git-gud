package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"
	httptransport "github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/gofrs/flock"
)

const defaultBatchSize = 512

// Options configures a Client.
type Options struct {
	// CacheDir contains one standard bare Git repository per remote URL.
	// It defaults to os.UserCacheDir()/git-gud.
	CacheDir string
	// BatchSize bounds the number of object wants in one fetch.
	BatchSize int
	// Progress receives Git sideband progress. Nil suppresses it.
	Progress io.Writer
}

// Client opens remote repositories backed by persistent bare Git caches.
type Client struct {
	cacheDir  string
	batchSize int
	progress  io.Writer
}

func NewClient(options Options) (*Client, error) {
	cacheDir := options.CacheDir
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("locate user cache: %w", err)
		}
		cacheDir = filepath.Join(base, "git-gud")
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	return &Client{cacheDir: cacheDir, batchSize: batchSize, progress: options.Progress}, nil
}

// Repository is a remote snapshot and its persistent bare Git object cache.
type Repository struct {
	target     Target
	commitHash plumbing.Hash
	rootHash   plumbing.Hash
	repo       *git.Repository
	session    transport.Session
	commander  transport.Commander
	batchSize  int
	progress   io.Writer
	cachePath  string
	lock       *flock.Flock
}

// Open resolves the requested ref, incrementally fetches its commit, and opens
// a snapshot. Call Close when done so another process can update the cache.
func (c *Client) Open(ctx context.Context, target Target) (_ *Repository, err error) {
	parsedURL, err := url.Parse(target.URL)
	if err != nil {
		return nil, fmt.Errorf("parse repository URL: %w", err)
	}

	cacheURL := *parsedURL
	cacheURL.User = nil
	keyBytes := sha256.Sum256([]byte(cacheURL.String()))
	key := hex.EncodeToString(keyBytes[:])
	cachePath := filepath.Join(c.cacheDir, "repos", key+".git")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	cacheLock := flock.New(cachePath + ".lock")
	locked, err := cacheLock.TryLockContext(ctx, 100_000_000)
	if err != nil {
		return nil, fmt.Errorf("lock cache: %w", err)
	}
	if !locked {
		return nil, context.Canceled
	}
	defer func() {
		if err != nil {
			_ = cacheLock.Unlock()
		}
	}()

	repository, err := openBareCache(cachePath, cacheURL.String())
	if err != nil {
		return nil, err
	}

	transportURL, err := transport.ParseURL(target.URL)
	if err != nil {
		return nil, fmt.Errorf("parse Git endpoint: %w", err)
	}
	tr := httptransport.NewTransport(httptransport.Options{})
	session, err := tr.Handshake(ctx, &transport.Request{
		URL:      transportURL,
		Command:  transport.UploadPackService,
		Protocol: protocol.V2,
	})
	if err != nil {
		return nil, fmt.Errorf("smart HTTP handshake: %w", err)
	}
	defer func() {
		if err != nil {
			_ = session.Close()
		}
	}()

	if !session.Capabilities().Supports(capability.LsRefs) {
		return nil, fmt.Errorf("remote does not support Git protocol v2")
	}
	commander, ok := session.(transport.Commander)
	if !ok {
		return nil, fmt.Errorf("go-git transport does not expose protocol v2 commands")
	}
	if !fetchFeature(session.Capabilities(), "filter") || !fetchFeature(session.Capabilities(), "shallow") {
		return nil, fmt.Errorf("remote must support protocol v2 fetch filters and shallow fetches")
	}

	commitHash, refName, err := resolveRef(ctx, commander, target.Ref)
	if err != nil {
		return nil, err
	}
	if err := ensureCommit(ctx, session, repository, commitHash, c.progress); err != nil {
		return nil, err
	}
	if err := markPromisorPacks(cachePath); err != nil {
		return nil, err
	}
	commitHash, rootHash, err := peelCommit(repository, commitHash)
	if err != nil {
		return nil, err
	}
	if refName != "" {
		localName := plumbing.ReferenceName("refs/git-gud/" + keyForRef(refName.String()))
		if err := repository.Storer.SetReference(plumbing.NewHashReference(localName, commitHash)); err != nil {
			return nil, fmt.Errorf("update cached ref: %w", err)
		}
	}
	if err := repository.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, commitHash)); err != nil {
		return nil, fmt.Errorf("update cached HEAD: %w", err)
	}

	return &Repository{
		target:     target,
		commitHash: commitHash,
		rootHash:   rootHash,
		repo:       repository,
		session:    session,
		commander:  commander,
		batchSize:  c.batchSize,
		progress:   c.progress,
		cachePath:  cachePath,
		lock:       cacheLock,
	}, nil
}

func openBareCache(cachePath, remoteURL string) (*git.Repository, error) {
	repository, err := git.PlainOpen(cachePath)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repository, err = git.PlainInit(cachePath, true)
		if err != nil {
			return nil, fmt.Errorf("initialize bare cache: %w", err)
		}
		_, err = repository.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{remoteURL},
		})
		if err != nil && !errors.Is(err, git.ErrRemoteExists) {
			return nil, fmt.Errorf("configure cache remote: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("open bare cache: %w", err)
	}

	cfg, err := repository.Config()
	if err != nil {
		return nil, fmt.Errorf("read cache config: %w", err)
	}
	remote := cfg.Raw.Section("remote").Subsection("origin")
	remote.SetOption("promisor", "true")
	remote.SetOption("partialclonefilter", "tree:0")
	if err := repository.Storer.SetConfig(cfg); err != nil {
		return nil, fmt.Errorf("configure partial Git cache: %w", err)
	}
	if err := markPromisorPacks(cachePath); err != nil {
		return nil, err
	}
	return repository, nil
}

func markPromisorPacks(cachePath string) error {
	packs, err := filepath.Glob(filepath.Join(cachePath, "objects", "pack", "pack-*.pack"))
	if err != nil {
		return fmt.Errorf("scan cached packs: %w", err)
	}
	for _, pack := range packs {
		marker := strings.TrimSuffix(pack, ".pack") + ".promisor"
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("mark promisor pack: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("mark promisor pack: %w", err)
		}
	}
	return nil
}

func fetchFeature(capabilities *capability.List, feature string) bool {
	for _, value := range capabilities.Get("fetch") {
		for _, field := range strings.Fields(value) {
			if field == feature {
				return true
			}
		}
	}
	return false
}

func ensureCommit(ctx context.Context, session transport.Session, repository *git.Repository, hash plumbing.Hash, progress io.Writer) error {
	if err := repository.Storer.HasEncodedObject(hash); err == nil {
		return nil
	}
	request := &transport.FetchRequest{
		Wants:  []plumbing.Hash{hash},
		Depth:  1,
		Filter: packp.FilterTreeDepth(0),
	}
	if progress != nil {
		request.Progress = progress
	}
	if err := session.Fetch(ctx, repository.Storer, request); err != nil && !errors.Is(err, transport.ErrNoChange) {
		return fmt.Errorf("fetch commit %s: %w", hash, err)
	}
	return nil
}

func peelCommit(repository *git.Repository, hash plumbing.Hash) (plumbing.Hash, plumbing.Hash, error) {
	for depth := 0; depth < 16; depth++ {
		object, err := repository.Storer.EncodedObject(plumbing.AnyObject, hash)
		if err != nil {
			return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("read revision object %s: %w", hash, err)
		}
		switch object.Type() {
		case plumbing.CommitObject:
			commit, err := repository.CommitObject(hash)
			if err != nil {
				return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("decode commit %s: %w", hash, err)
			}
			return hash, commit.TreeHash, nil
		case plumbing.TagObject:
			tag, err := repository.TagObject(hash)
			if err != nil {
				return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("decode tag %s: %w", hash, err)
			}
			hash = tag.Target
		default:
			return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("revision resolves to %s, not a commit", object.Type())
		}
	}
	return plumbing.ZeroHash, plumbing.ZeroHash, fmt.Errorf("tag chain is too deep")
}

func keyForRef(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(sum[:16])
}

// CommitHash returns the immutable commit selected when Open was called.
func (r *Repository) CommitHash() plumbing.Hash { return r.commitHash }

func (r *Repository) Close() error {
	var result error
	if r.session != nil {
		result = r.session.Close()
		r.session = nil
	}
	if r.lock != nil {
		if err := r.lock.Unlock(); result == nil {
			result = err
		}
		r.lock = nil
	}
	return result
}
