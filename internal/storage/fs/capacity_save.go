package fs

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *FilesystemStore) admitSaveLocked(entry saveEntry, record objectRecord, reserve int) (*storage.ObjectMeta, error) {
	if s.namespace == nil {
		return s.publishSaveLocked(entry, record)
	}
	admission, err := s.saveAdmissionLocked(entry, record, reserve)
	if err != nil {
		return nil, err
	}
	var meta *storage.ObjectMeta
	var saveErr error
	called := false
	err = s.namespace.WithAdmission(context.Background(), admission, func() error {
		called = true
		s.capacityActive = true
		defer func() { s.capacityActive = false }()
		meta, saveErr = s.publishSaveLocked(entry, record)
		if saveErr != nil && !errors.Is(saveErr, storage.ErrMutationCommitted) {
			return errors.Join(capacity.ErrUnknown, saveErr)
		}
		return nil
	})
	if !called {
		return nil, err
	}
	if err != nil && saveErr == nil {
		return meta, storage.CommittedError(err)
	}
	return meta, errors.Join(saveErr, err)
}

func (s *FilesystemStore) saveAdmissionLocked(entry saveEntry, record objectRecord, reserve int) (capacity.Admission, error) {
	usage, err := s.reconciledCapacityUsageLocked()
	if err != nil {
		return capacity.Admission{}, err
	}
	path := s.objectPath(entry.Bucket, entry.Key)
	record.SaveRequest, record.SaveMeaning = entry.RequestKey, entry.Meaning
	record.Key = ""
	if s.isBoundedObjectPath(entry.Bucket, path) {
		record.Key = entry.Key
	}
	data, err := encodeObjectRecord(record)
	if err != nil {
		return capacity.Admission{}, err
	}
	usage.Resources = replaceResource(usage.Resources, resourceFor(path, data, capacity.Payload))
	entry.Phase = "committed"
	receipt, err := encodeSaveEntry(entry)
	if err != nil {
		return capacity.Admission{}, err
	}
	path = filepath.Join(s.saveDir(), requestName(entry.RequestKey))
	usage.Resources = replaceResource(usage.Resources, resourceFor(path, receipt, capacity.Recovery))
	// The journal reservation covers pending and replacement receipt files.
	return capacity.Admission{Usage: usage, StagingBytes: int64(len(data)), RecoveryBytes: int64(reserve)}, nil
}
