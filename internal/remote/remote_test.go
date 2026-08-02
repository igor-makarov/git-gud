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
	if _, err := os.Stat(filepath.Join(packDir, "pack-deadbeef.promisor")); err != nil {
		t.Fatal(err)
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
