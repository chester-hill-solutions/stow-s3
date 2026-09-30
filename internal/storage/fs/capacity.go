package fs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
)

type capacityBinding struct{ Host, Namespace, StoreID string }

func (s *FilesystemStore) BindNamespace(namespace *capacity.Namespace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if namespace == nil || !s.SupportsGuardedWrites() {
		return capacity.ErrInvalid
	}
	host, id := namespace.Binding()
	binding := capacityBinding{Host: host, Namespace: id, StoreID: s.saves.id}
	path := filepath.Join(s.dataDir, ".capacity-binding.json")
	if err := s.checkCapacityBinding(path, binding); err != nil {
		return err
	}
	if err := namespace.BindStore(s.saves.id, s.dataDir); err != nil {
		return err
	}
	s.namespace = namespace
	if err := s.reconcileCapacityLocked(); err != nil {
		return err
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	return s.writeCapacityFile(path, encoded)
}

// checkCapacityBinding accepts an unbound store and refuses one bound to
// another host or namespace. An absent record is the only way a store
// directory gets bound in the first place, so it is not a conflict.
func (s *FilesystemStore) checkCapacityBinding(path string, binding capacityBinding) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var existing capacityBinding
	if json.Unmarshal(data, &existing) != nil || existing != binding {
		return capacity.ErrConflict
	}
	return nil
}

func resourceFor(path string, data []byte, class string) capacity.Resource {
	name := sha256.Sum256([]byte(filepath.Clean(path)))
	version := sha256.Sum256(data)
	return capacity.Resource{ID: hex.EncodeToString(name[:]), Version: hex.EncodeToString(version[:]), Bytes: int64(len(data)), Class: class}
}

func (s *FilesystemStore) scanCapacityLocked() (capacity.Usage, error) {
	usage := capacity.Usage{StoreID: s.saves.id}
	err := filepath.WalkDir(s.dataDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return capacity.ErrUnknown
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == storeLockName {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		class, err := s.validateCapacityFile(path, data)
		if err != nil {
			return err
		}
		usage.Resources = append(usage.Resources, resourceFor(path, data, class))
		return nil
	})
	return usage, err
}

func (s *FilesystemStore) validateCapacityFile(path string, data []byte) (string, error) {
	rel, err := filepath.Rel(s.dataDir, path)
	if err != nil {
		return "", err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	switch parts[0] {
	case "buckets":
		return s.capacityClassForBucket(path, data, parts)
	case ".multipart":
		return capacityClassForMultipart(path, parts)
	}
	if parts[0] == ".save-requests" || rel == ".save-pending.json" {
		_, _, err := s.readSaveEntry(path)
		return capacity.Recovery, err
	}
	if rel == OwnerMarkerName || rel == ".save-identity.json" || rel == ".capacity-binding.json" ||
		rel == ".recovery-holds.json" || rel == ".recovery-holds.identity" {
		return capacity.Recovery, nil
	}
	return "", capacity.ErrUnknown
}

// capacityClassForBucket classifies one file under buckets/. A file this store
// did not write is refused rather than guessed at, because a scan that skips
// one under-reports and the budget it feeds then lets more data in.
func (s *FilesystemStore) capacityClassForBucket(path string, data []byte, parts []string) (string, error) {
	if len(parts) < 3 {
		return "", capacity.ErrUnknown
	}
	if parts[2] == "bucket.json" {
		return capacity.Recovery, validateJSONRecord(data)
	}
	if len(parts) < 4 || parts[2] != "objects" {
		return "", capacity.ErrUnknown
	}
	_, err := readObjectRecord(path)
	return capacity.Payload, err
}

func capacityClassForMultipart(path string, parts []string) (string, error) {
	if len(parts) != 3 {
		return "", capacity.ErrUnknown
	}
	if parts[2] == "manifest.json" {
		_, err := readMultipartManifest(filepath.Dir(path))
		return capacity.Recovery, err
	}
	if strings.HasPrefix(parts[2], "part-") {
		return capacity.Payload, nil
	}
	return "", capacity.ErrUnknown
}

func validateJSONRecord(data []byte) error {
	if json.Valid(data) {
		return nil
	}
	return fmt.Errorf("capacity: malformed json record: %w", capacity.ErrUnknown)
}

func (s *FilesystemStore) reconcileCapacityLocked() error {
	if s.namespace == nil || s.capacityActive {
		return nil
	}
	if s.saves.pendingError != nil || s.saves.pending != nil {
		return capacity.ErrUnknown
	}
	usage, err := s.scanCapacityLocked()
	if err != nil {
		return errors.Join(capacity.ErrUnknown, err)
	}
	return s.namespace.Reconcile(context.Background(), usage)
}

func (s *FilesystemStore) writeCapacityFile(path string, data []byte) error {
	if s.namespace == nil || s.capacityActive {
		return writeBytesAtomic(path, data)
	}
	if err := s.reconcileCapacityLocked(); err != nil {
		return err
	}
	usage, err := s.scanCapacityLocked()
	if err != nil {
		return err
	}
	class, err := s.capacityClass(path)
	if err != nil {
		return err
	}
	resource := resourceFor(path, data, class)
	usage.Resources = replaceResource(usage.Resources, resource)
	admission := capacity.Admission{Usage: usage}
	if class == capacity.Payload {
		admission.StagingBytes = int64(len(data))
	} else {
		admission.RecoveryBytes = int64(len(data))
	}
	return s.namespace.WithAdmission(context.Background(), admission, func() error {
		if err := writeBytesAtomic(path, data); err != nil {
			return err
		}
		return syncSaveAncestors(filepath.Dir(path))
	})
}

func (s *FilesystemStore) capacityClass(path string) (string, error) {
	rel, err := filepath.Rel(s.dataDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", capacity.ErrInvalid
	}
	if strings.Contains(rel, string(filepath.Separator)+"objects"+string(filepath.Separator)) || strings.HasPrefix(filepath.Base(path), "part-") {
		return capacity.Payload, nil
	}
	return capacity.Recovery, nil
}

func replaceResource(resources []capacity.Resource, resource capacity.Resource) []capacity.Resource {
	for index, current := range resources {
		if current.ID == resource.ID {
			resources[index] = resource
			return resources
		}
	}
	return append(resources, resource)
}

func (s *FilesystemStore) removeCapacityPath(path string) error {
	if s.namespace == nil || s.capacityActive {
		return os.RemoveAll(path)
	}
	if err := s.reconcileCapacityLocked(); err != nil {
		return err
	}
	usage, err := s.scanCapacityLocked()
	if err != nil {
		return err
	}
	filtered := usage.Resources[:0]
	err = filepath.WalkDir(path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		id := resourceFor(p, nil, capacity.Payload).ID
		for index, r := range usage.Resources {
			if r.ID == id {
				usage.Resources[index].ID = ""
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, r := range usage.Resources {
		if r.ID != "" {
			filtered = append(filtered, r)
		}
	}
	usage.Resources = filtered
	return s.namespace.WithAdmission(context.Background(), capacity.Admission{Usage: usage}, func() error {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		return syncSaveAncestors(filepath.Dir(path))
	})
}
