package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Store) ObserveWorkspaceFile(ctx context.Context, bucket, key string, maxBytes int64) (storage.WorkspaceFileObservation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if bucket != s.manifest.Bucket || !s.layout.IsNatural(key) || maxBytes < 1 {
		return storage.WorkspaceFileObservation{}, storage.ErrInvalidKey
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.observeWorkspaceFileLocked(ctx, bucket, key, maxBytes)
}

func (s *Store) observeWorkspaceFileLocked(ctx context.Context, bucket, key string, maxBytes int64) (storage.WorkspaceFileObservation, error) {
	if s.closed {
		return storage.WorkspaceFileObservation{}, ErrClosed
	}
	if err := s.checkFileSaveMarkerLocked(); err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	entry, indexed := s.objectIndex.entry(bucket, key)
	if indexed && entry.Form != FormNatural {
		return storage.WorkspaceFileObservation{}, storage.ErrInvalidKey
	}
	root, err := s.readRoot()
	if err != nil {
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	defer root.Close()
	return readWorkspacePath(ctx, root, pathReadRequest{key: key, maxBytes: maxBytes, entry: entry, indexed: indexed})
}

func (s *Store) WorkspaceFileBucket() string { return s.manifest.Bucket }

type pathReadRequest struct {
	key      string
	maxBytes int64
	entry    ManifestEntry
	indexed  bool
}

func readWorkspacePath(ctx context.Context, root *rooted.Root, request pathReadRequest) (storage.WorkspaceFileObservation, error) {
	dir, parentRel, err := openExactWorkspaceParent(root, request.key)
	if err != nil {
		if errors.Is(err, storage.ErrWorkspacePathSpelling) {
			return storage.WorkspaceFileObservation{}, storage.ErrInvalidKey
		}
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	defer dir.Close()
	if err := ctx.Err(); err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	parts := strings.Split(request.key, "/")
	leaf := parts[len(parts)-1]
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	folded := false
	exact := false
	for _, entry := range entries {
		if entry.Name() == leaf {
			exact = true
		} else if FoldPath(entry.Name()) == FoldPath(leaf) {
			folded = true
		}
	}
	if folded {
		return storage.WorkspaceFileObservation{}, storage.ErrInvalidKey
	}
	if !exact {
		return observeWorkspaceAbsence(root, dir, parentRel, request.key)
	}
	return readWorkspaceFile(ctx, root, dir, parentRel, request)
}

func observeWorkspaceAbsence(root *rooted.Root, dir *os.File, parentRel, key string) (storage.WorkspaceFileObservation, error) {
	if err := root.Check(); err != nil {
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	parentInfo, err := dir.Stat()
	if err != nil {
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	if !sameWorkspaceReadDirectory(root, parentRel, parentInfo) {
		return storage.WorkspaceFileObservation{}, storage.ErrObjectUncertain
	}
	return storage.WorkspaceFileObservation{Path: key, Absent: true, Identity: nativeWorkspaceFileIdentity{root: root.Info(), parent: parentInfo}}, nil
}

func readWorkspaceFile(ctx context.Context, root *rooted.Root, dir *os.File, parentRel string, request pathReadRequest) (storage.WorkspaceFileObservation, error) {
	file, err := root.OpenRegularFile(filepath.FromSlash(request.key))
	if err != nil {
		if errors.Is(err, rooted.ErrNotRegular) || errors.Is(err, rooted.ErrSymlink) {
			return storage.WorkspaceFileObservation{}, fmt.Errorf("workspace: path is not a regular file")
		}
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	defer file.Close()
	data, after, err := readWorkspaceFileBytes(ctx, file, request.maxBytes)
	if err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	if err := root.Check(); err != nil {
		return storage.WorkspaceFileObservation{}, uncertain(err)
	}
	metadata, err := workspaceEntryFingerprint(request.entry)
	if err != nil {
		return storage.WorkspaceFileObservation{}, err
	}
	digest := sha256.Sum256(data)
	generation := request.entry.VersionID
	if !request.indexed {
		generation = ""
	}
	parentInfo, statErr := dir.Stat()
	if statErr != nil {
		return storage.WorkspaceFileObservation{}, uncertain(statErr)
	}
	if !sameWorkspaceReadDirectory(root, parentRel, parentInfo) {
		return storage.WorkspaceFileObservation{}, storage.ErrObjectUncertain
	}
	identity := nativeWorkspaceFileIdentity{root: root.Info(), parent: parentInfo, file: after}
	return storage.WorkspaceFileObservation{Path: request.key, Data: data, Digest: hex.EncodeToString(digest[:]), Mode: uint32(after.Mode().Perm()),
		Modified: after.ModTime().UTC(), Generation: generation, Identity: identity, Size: after.Size(), Metadata: metadata}, nil
}

func readWorkspaceFileBytes(ctx context.Context, file *os.File, maxBytes int64) ([]byte, os.FileInfo, error) {
	before, err := file.Stat()
	if err != nil {
		return nil, nil, uncertain(err)
	}
	if !before.Mode().IsRegular() {
		return nil, nil, storage.ErrInvalidKey
	}
	if before.Size() > maxBytes {
		return nil, nil, storage.ErrWorkspaceFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, nil, uncertain(err)
	}
	if int64(len(data)) > maxBytes {
		return nil, nil, storage.ErrWorkspaceFileTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil || !stableWorkspaceFile(before, after, int64(len(data))) {
		return nil, nil, fmt.Errorf("workspace: file changed during observation")
	}
	return data, after, nil
}

func stableWorkspaceFile(before, after os.FileInfo, size int64) bool {
	return os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && size == after.Size()
}

func workspaceEntryFingerprint(entry ManifestEntry) (string, error) {
	hash := sha256.New()
	if err := json.NewEncoder(hash).Encode(entry); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func openExactWorkspaceParent(root *rooted.Root, key string) (*os.File, string, error) {
	parts := strings.Split(key, "/")
	current, err := root.OpenDirectory(".")
	if err != nil {
		return nil, "", err
	}
	prefix := ""
	for _, part := range parts[:len(parts)-1] {
		entries, readErr := current.ReadDir(-1)
		if readErr != nil {
			current.Close()
			return nil, "", readErr
		}
		var exact os.DirEntry
		folded := false
		for _, entry := range entries {
			if entry.Name() == part {
				exact = entry
			} else if FoldPath(entry.Name()) == FoldPath(part) {
				folded = true
			}
		}
		if folded {
			current.Close()
			return nil, "", storage.ErrWorkspacePathSpelling
		}
		if exact == nil {
			current.Close()
			return nil, "", storage.ErrWorkspaceParentInvalid
		}
		if prefix == "" {
			prefix = part
		} else {
			prefix = filepath.Join(prefix, part)
		}
		expected, infoErr := exact.Info()
		if infoErr != nil {
			current.Close()
			return nil, "", infoErr
		}
		next, openErr := root.OpenDirectory(prefix)
		if openErr != nil {
			current.Close()
			return nil, "", openErr
		}
		actual, statErr := next.Stat()
		if statErr != nil || !os.SameFile(expected, actual) {
			next.Close()
			current.Close()
			return nil, "", storage.ErrWorkspaceParentInvalid
		}
		current.Close()
		current = next
	}
	if prefix == "" {
		prefix = "."
	}
	return current, prefix, nil
}

func sameWorkspaceReadDirectory(root *rooted.Root, path string, expected os.FileInfo) bool {
	current, err := root.OpenDirectory(path)
	if err != nil {
		return false
	}
	defer current.Close()
	actual, err := current.Stat()
	return err == nil && os.SameFile(expected, actual)
}

type nativeWorkspaceFileIdentity struct {
	root   os.FileInfo
	parent os.FileInfo
	file   os.FileInfo
}

func (identity nativeWorkspaceFileIdentity) Same(other storage.WorkspaceFileIdentity) bool {
	peer, ok := other.(nativeWorkspaceFileIdentity)
	if !ok || !os.SameFile(identity.root, peer.root) || !os.SameFile(identity.parent, peer.parent) || (identity.file == nil) != (peer.file == nil) {
		return false
	}
	return identity.file == nil || os.SameFile(identity.file, peer.file)
}

func (s *Store) CheckFileSaveMarker() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ErrClosed
	}
	return s.checkFileSaveMarkerLocked()
}

func (s *Store) CheckWorkspaceFileSaveEditorOpen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.fileSaveBlocked {
		return storage.ErrWorkspaceFileBlocked
	}
	root, err := s.readRoot()
	if err != nil {
		return storage.ErrWorkspaceFileBlocked
	}
	defer root.Close()
	if err := checkWorkspaceFileSaveDirectory(root); errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		if s.fileSaveJournal == nil {
			return nil
		}
		return storage.ErrWorkspaceFileBlocked
	} else if err != nil {
		return storage.ErrWorkspaceFileBlocked
	}
	if s.fileSaveJournal == nil {
		return storage.ErrWorkspaceFileUnsupported
	}
	journal, err := loadWorkspaceFileSaveJournal(root, s.manifest.WorkspaceID, s.manifest.Bucket)
	if err != nil || journal.identity.JournalID != s.fileSaveJournal.identity.JournalID {
		return storage.ErrWorkspaceFileBlocked
	}
	s.fileSaveJournal = journal
	return nil
}

func (s *Store) checkFileSaveMarkerLocked() error {
	if s.fileSaveBlocked || s.fileSaveJournalBlocked {
		return storage.ErrWorkspaceFileBlocked
	}
	root, err := s.readRoot()
	if err != nil {
		return uncertain(err)
	}
	defer root.Close()
	err = checkWorkspaceFileSaveDirectory(root)
	if errors.Is(err, errWorkspaceFileSaveJournalAbsent) {
		if s.fileSaveJournal == nil {
			return nil
		}
		return storage.ErrWorkspaceFileBlocked
	}
	if err != nil {
		return storage.ErrWorkspaceFileBlocked
	}
	if s.fileSaveJournal == nil || s.fileSaveJournal.identity.WorkspaceID != s.manifest.WorkspaceID || s.fileSaveJournal.identity.Bucket != s.manifest.Bucket {
		return storage.ErrWorkspaceFileUnsupported
	}
	identity, _, err := readWorkspaceFileSaveIdentity(root, s.manifest.WorkspaceID, s.manifest.Bucket)
	if err != nil || identity.JournalID != s.fileSaveJournal.identity.JournalID {
		return storage.ErrWorkspaceFileBlocked
	}
	if _, err := readWorkspaceFileSaveFile(root, workspaceFileSavePendingPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return storage.ErrWorkspaceFileBlocked
	}
	if s.fileSaveJournal.pending != nil {
		return storage.ErrWorkspaceFileBlocked
	}
	return CheckWorkspaceFileSaveBarrier(s.root, s.manifest.WorkspaceID, s.manifest.Bucket)
}

func refuseUnrecoveredWorkspaceFileSaveJournal(rootPath string) error {
	info, err := os.Lstat(rootPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return storage.ErrWorkspaceFileUnsupported
	}
	marker, err := workspaceFileSaveMarkerPresent(rootPath)
	if err != nil || marker {
		return storage.ErrWorkspaceFileUnsupported
	}
	return nil
}

func workspaceFileSaveMarkerPresent(rootPath string) (bool, error) {
	metadataPath := filepath.Join(rootPath, ".stow")
	metadataInfo, err := os.Lstat(metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return false, storage.ErrWorkspaceFileUnsupported
	}
	metadata, err := os.Open(metadataPath)
	if err != nil {
		return false, storage.ErrWorkspaceFileUnsupported
	}
	openedInfo, statErr := metadata.Stat()
	exact, folded, readErr := findWorkspaceFileSaveMarker(metadata)
	closeErr := metadata.Close()
	finalInfo, finalErr := os.Lstat(metadataPath)
	if statErr != nil || !os.SameFile(metadataInfo, openedInfo) || finalErr != nil || !os.SameFile(metadataInfo, finalInfo) || readErr != nil || closeErr != nil || folded || exact {
		return exact || folded, storage.ErrWorkspaceFileUnsupported
	}
	return false, nil
}
