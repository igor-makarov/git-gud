package remote

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		input string
		url   string
		ref   string
	}{
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo.git", ""},
		{"https://github.com/owner/repo.git@feature/fast", "https://github.com/owner/repo.git", "feature/fast"},
		{"https://user@example.com/owner/repo.git", "https://user@example.com/owner/repo.git", ""},
	}
	for _, test := range tests {
		target, err := ParseTarget(test.input)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", test.input, err)
		}
		if target.URL != test.url || target.Ref != test.ref {
			t.Errorf("ParseTarget(%q) = %#v", test.input, target)
		}
	}
}

func TestListAndFind(t *testing.T) {
	repository := fixtureRepository(t)
	ctx := context.Background()

	var listed []string
	if err := repository.List(ctx, "Specs", true, func(entry Entry) error {
		listed = append(listed, entry.Path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantListed := []string{
		"Specs/0", "Specs/f", "Specs/0/1", "Specs/f/e", "Specs/0/1/2",
		"Specs/f/e/d", "Specs/0/1/2/Alpha.json", "Specs/f/e/d/Zulu.txt",
	}
	if !reflect.DeepEqual(listed, wantListed) {
		t.Fatalf("List returned\n%q\nwant\n%q", listed, wantListed)
	}

	var found []string
	if err := repository.Find(ctx, "Specs", "*/*/*/*", func(entry Entry) error {
		found = append(found, entry.Path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantFound := []string{"Specs/0/1/2/Alpha.json", "Specs/f/e/d/Zulu.txt"}
	if !reflect.DeepEqual(found, wantFound) {
		t.Fatalf("Find returned %q, want %q", found, wantFound)
	}

	found = nil
	if err := repository.Find(ctx, ".", "**/*.json", func(entry Entry) error {
		found = append(found, entry.Path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(found, []string{"Specs/0/1/2/Alpha.json"}) {
		t.Fatalf("doublestar Find returned %q", found)
	}
}

func TestDownload(t *testing.T) {
	repository := fixtureRepository(t)
	target := t.TempDir()
	if err := repository.Download(context.Background(), "Specs", target, 4); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{
		"0/1/2/Alpha.json": "alpha\n",
		"f/e/d/Zulu.txt":   "zulu\n",
	} {
		contents, err := os.ReadFile(filepath.Join(target, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != expected {
			t.Fatalf("downloaded %q, want %q", contents, expected)
		}
	}
	if err := repository.Download(context.Background(), "Specs", target, 4); err != nil {
		t.Fatalf("overwrite download: %v", err)
	}
}

func TestDownloadFile(t *testing.T) {
	repository := fixtureRepository(t)
	target := t.TempDir()
	if err := repository.Download(context.Background(), "Specs/0/1/2/Alpha.json", target, 4); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(target, "Alpha.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "alpha\n" {
		t.Fatalf("downloaded %q, want alpha", contents)
	}
	if _, err := os.Stat(filepath.Join(target, "Specs")); !os.IsNotExist(err) {
		t.Fatalf("single-file download created repository path: %v", err)
	}
}

func TestDownloadFileGlob(t *testing.T) {
	repository := fixtureRepository(t)
	target := t.TempDir()
	if err := repository.Download(context.Background(), "Specs/*/*/*/*.json", target, 4); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(target, "0", "1", "2", "Alpha.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "alpha\n" {
		t.Fatalf("downloaded %q, want alpha", contents)
	}
	if _, err := os.Stat(filepath.Join(target, "f")); !os.IsNotExist(err) {
		t.Fatalf("glob downloaded unmatched path: %v", err)
	}
}

func TestDownloadFileGlobRequiresMatch(t *testing.T) {
	repository := fixtureRepository(t)
	if err := repository.Download(context.Background(), "Specs/**/*.md", t.TempDir(), 4); err == nil {
		t.Fatal("expected unmatched glob to fail")
	}
}

func TestDownloadFetchesRecursiveClosureOnce(t *testing.T) {
	repository := fixtureRepository(t)
	commander := &recordingCommander{}
	repository.commander = commander
	repository.cachePath = t.TempDir()

	root, err := object.GetTree(repository.repo.Storer, repository.rootHash)
	if err != nil {
		t.Fatal(err)
	}
	want := root.Entries[0].Hash
	for range 2 {
		if err := repository.Download(context.Background(), "Specs", t.TempDir(), 4); err != nil {
			t.Fatal(err)
		}
	}
	if len(commander.requests) != 1 {
		t.Fatalf("recursive closure fetches = %d, want 1", len(commander.requests))
	}
	request := commander.requests[0]
	if len(request.Wants) != 1 || request.Wants[0] != want {
		t.Fatalf("recursive closure wants = %v, want [%s]", request.Wants, want)
	}
	if request.Filter != "" {
		t.Fatalf("recursive closure filter = %q, want no filter", request.Filter)
	}
}

type recordingCommander struct {
	requests []*packp.FetchArgs
}

func (commander *recordingCommander) Command(_ context.Context, command string, arguments packp.CommandArgs, _ packp.Decoder) error {
	if command != "fetch" {
		return nil
	}
	request := arguments.(*packp.FetchArgs)
	copy := *request
	copy.Wants = append([]plumbing.Hash(nil), request.Wants...)
	commander.requests = append(commander.requests, &copy)
	return nil
}

func TestValidateTreeName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b"} {
		if err := validateTreeName(name); err == nil {
			t.Errorf("validateTreeName(%q) succeeded", name)
		}
	}
	if err := validateTreeName("valid name.json"); err != nil {
		t.Fatalf("valid tree name rejected: %v", err)
	}
}

func TestDownloadRejectsExistingSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	destination := filepath.Join(root, "escape")
	if err := os.Symlink(outside, destination); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := ensureChildDirectory(destination); err == nil {
		t.Fatal("expected symlink directory to be rejected")
	}
}

func TestOpenBareCache(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "repository.git")
	for range 2 {
		repository, err := openBareCache(cache, "https://example.com/repository.git")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.Remote("origin"); err != nil {
			t.Fatal(err)
		}
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkPromisorPacks(t *testing.T) {
	cache := t.TempDir()
	packDir := filepath.Join(cache, "objects", "pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(packDir, "pack-deadbeef.pack")
	if err := os.WriteFile(pack, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := markPromisorPacks(cache); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(packDir, "pack-deadbeef.promisor")
	if err := os.Chmod(marker, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := markPromisorPacks(cache); err != nil {
		t.Fatalf("mark existing read-only promisor pack: %v", err)
	}
}

func fixtureRepository(t *testing.T) *Repository {
	t.Helper()
	storage := memory.NewStorage()
	repository, err := git.Init(storage)
	if err != nil {
		t.Fatal(err)
	}

	alpha := storeBlob(t, storage, "alpha\n")
	zulu := storeBlob(t, storage, "zulu\n")
	leftLeaf := storeTree(t, storage, object.TreeEntry{Name: "Alpha.json", Mode: filemode.Regular, Hash: alpha})
	rightLeaf := storeTree(t, storage, object.TreeEntry{Name: "Zulu.txt", Mode: filemode.Regular, Hash: zulu})
	leftTwo := storeTree(t, storage, object.TreeEntry{Name: "2", Mode: filemode.Dir, Hash: leftLeaf})
	rightD := storeTree(t, storage, object.TreeEntry{Name: "d", Mode: filemode.Dir, Hash: rightLeaf})
	leftOne := storeTree(t, storage, object.TreeEntry{Name: "1", Mode: filemode.Dir, Hash: leftTwo})
	rightE := storeTree(t, storage, object.TreeEntry{Name: "e", Mode: filemode.Dir, Hash: rightD})
	specs := storeTree(t, storage,
		object.TreeEntry{Name: "0", Mode: filemode.Dir, Hash: leftOne},
		object.TreeEntry{Name: "f", Mode: filemode.Dir, Hash: rightE},
	)
	root := storeTree(t, storage, object.TreeEntry{Name: "Specs", Mode: filemode.Dir, Hash: specs})
	return &Repository{repo: repository, rootHash: root, batchSize: 2}
}

func storeBlob(t *testing.T, storage *memory.Storage, contents string) plumbing.Hash {
	t.Helper()
	encoded := storage.NewEncodedObject()
	encoded.SetType(plumbing.BlobObject)
	encoded.SetSize(int64(len(contents)))
	writer, err := encoded.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bytes.NewBufferString(contents).WriteTo(writer); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	hash, err := storage.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func storeTree(t *testing.T, storage *memory.Storage, entries ...object.TreeEntry) plumbing.Hash {
	t.Helper()
	encoded := storage.NewEncodedObject()
	tree := &object.Tree{Entries: entries}
	if err := tree.Encode(encoded); err != nil {
		t.Fatal(err)
	}
	hash, err := storage.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
