package workspace

import (
	"errors"
	"os"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// CheckWorkspaceFileSaveBarrier reads the persisted marker without opening a Store.
func CheckWorkspaceFileSaveBarrier(rootPath, expectedID, bucket string) error {
	info, err := os.Lstat(rootPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return storage.ErrSaveRequestUnknown
	}
	root, err := rooted.Open(rootPath)
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	defer root.Close()
	return checkWorkspaceFileSaveBarrierRoot(root, expectedID, bucket)
}

func checkWorkspaceFileSaveBarrierRoot(root *rooted.Root, expectedID, bucket string) error {
	if err := checkWorkspaceFileSaveDirectory(root); errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		return nil
	} else if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	return checkWorkspaceFileSaveBarrierIdentity(root, expectedID, bucket)
}

func checkWorkspaceFileSaveBarrierIdentity(root *rooted.Root, expectedID, bucket string) error {
	if !ValidWorkspaceID(expectedID) || storage.ValidateBucketName(bucket) != nil {
		return storage.ErrSaveRequestUnknown
	}
	manifest := &Manifest{}
	if err := readRecoveryDocument(root, "manifest.json", manifest); err != nil || manifest.WorkspaceID != expectedID || manifest.Bucket != bucket {
		return storage.ErrSaveRequestUnknown
	}
	if err := validateWorkspaceFileSaveRoot(root, expectedID, bucket); err != nil {
		return storage.ErrSaveRequestUnknown
	}
	identity, identityBytes, err := readWorkspaceFileSaveIdentity(root, expectedID, bucket)
	if err != nil {
		return storage.ErrSaveRequestUnknown
	}
	journal, err := loadWorkspaceFileSaveContents(root, identity, identityBytes)
	if err != nil || journal.pending != nil || root.Check() != nil {
		return storage.ErrSaveRequestUnknown
	}
	return nil
}
