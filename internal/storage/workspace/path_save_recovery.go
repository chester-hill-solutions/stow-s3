package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func NewForFileSaveRecovery(options Options, expectedID string) (*Store, error) {
	if options.Root == "" || !ValidWorkspaceID(expectedID) || storage.ValidateBucketName(options.Bucket) != nil {
		return nil, storage.ErrInvalidSaveRequest
	}
	root, err := rooted.Open(options.Root)
	if err != nil {
		return nil, storage.ErrWorkspaceFileBlocked
	}
	defer root.Close()
	manifest, index, err := loadRecoveryManifest(root)
	if err != nil || manifest.WorkspaceID != expectedID || manifest.Bucket != options.Bucket {
		return nil, storage.ErrWorkspaceFileBlocked
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	manifest.rootIdentity = root.Info()
	index.rootIdentity = root.Info()
	manifest.path = manifestPath(options.Root)
	index.path = indexPath(options.Root)
	store := &Store{rootIdentity: root.Info(), root: options.Root, layout: NewLayout(), manifest: manifest, objectIndex: index,
		workspace: options.Bucket, folded: make(map[string]string), Now: now, fileSaveRecoveryOwner: true}
	if err := store.index(); err != nil {
		return nil, err
	}
	journal, err := loadWorkspaceFileSaveJournal(root, expectedID, options.Bucket)
	if err != nil {
		return nil, storage.ErrWorkspaceFileBlocked
	}
	store.fileSaveJournal = journal
	store.fileSaveJournalBlocked = journal.pending != nil
	if err := root.Check(); err != nil || !os.SameFile(store.rootIdentity, root.Info()) {
		return nil, storage.ErrWorkspaceFileBlocked
	}
	return store, nil
}

func ValidateRegisteredWorkspace(rootPath, expectedID, bucket string, owned bool) error {
	root, err := rooted.Open(rootPath)
	if err != nil {
		return storage.ErrWorkspaceFileBlocked
	}
	defer root.Close()
	manifest := &Manifest{}
	if err := readRecoveryDocument(root, "manifest.json", manifest); err != nil || manifest.WorkspaceID != expectedID || manifest.Bucket != bucket || manifest.Owned != owned {
		return storage.ErrWorkspaceFileBlocked
	}
	return root.Check()
}

func loadRecoveryManifest(root *rooted.Root) (*Manifest, *Index, error) {
	manifest := &Manifest{}
	if err := readRecoveryDocument(root, "manifest.json", manifest); err != nil {
		return nil, nil, err
	}
	index := &Index{}
	if err := readRecoveryDocument(root, "index.json", index); err != nil {
		return nil, nil, err
	}
	if index.Buckets == nil {
		index.Buckets = make(map[string]map[string]ManifestEntry)
	}
	return manifest, index, nil
}

func readRecoveryDocument(root *rooted.Root, path string, target versioned) error {
	file, err := root.OpenRegularFile(InternalPath("", path))
	if err != nil {
		return fmt.Errorf("workspace: read recovery %s: %w", path, err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 {
		return errors.Join(storage.ErrWorkspaceFileBlocked, err)
	}
	decoder := json.NewDecoder(io.LimitReader(file, before.Size()+1))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("workspace: decode recovery %s: %w", path, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return storage.ErrWorkspaceFileBlocked
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return storage.ErrWorkspaceFileBlocked
	}
	if target.layoutVersion() != manifestVersion {
		return ErrManifestCorrupt
	}
	return root.Check()
}
